package flow

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// Dispatcher đăng ký nghe sự kiện Coordinator, khi subagent trả về thì tính routing và hạ chỉ thị Host.
//
// Vòng đời: Attach trả về một hàm detach; khi đóng Host thì gọi để giải phóng đăng ký.
type Dispatcher struct {
	coordinator *agentcore.Agent
	store       *storepkg.Store

	enabled atomic.Bool // do Host kiểm soát có phái hay không (trước khi khởi động xong nên tắt)

	// Theo dõi lặp: nhớ Agent+Task của lần phái gần nhất và số lần hạ liên tiếp.
	// Cùng một chỉ thị tính lặp lại (sau khi subagent trả về trạng thái chưa tiến, Route tính lại kết quả không đổi) không âm thầm nuốt,
	// mà gửi lại kèm sự kiện số lần — "kết quả routing giống nhau N lần liên tiếp" là sự kiện chỉ Host quan sát được;
	// nếu im lặng, Coordinator sẽ rơi vào mâu thuẫn kép giữa "cấm tự quyết bước kế" (coordinator.md) và
	// "cấm dừng máy" (StopGuard), tự ý phát huy chính là vòng lặp chết kiểu freelance #24.
	// Quyền phân xử vẫn ở LLM: tin nhắn gửi lại chỉ kèm sự kiện và giấy phép rà soát, không đặt ngưỡng, không ngắt mạch (kiến trúc §10.13).
	// Tin nhắn vì kèm số lần nên khác nhau, không đẩy lặp chỉ thị giống y hệt vào followUpQ.
	lastMu   sync.Mutex
	lastSent *Instruction
	repeats  int

	// onRepeat là callback telemetry thuần (dùng để cảnh báo khi không có người trực), kích hoạt một lần khi cùng một chỉ thị
	// được hạ lần thứ repeatNotifyAt; không ảnh hưởng ngược tới việc phái, logic phái không hề biết nó tồn tại.
	onRepeat func(agent, task string, n int)
}

// repeatNotifyAt viết cứng không vào config: nó không phải ngưỡng điều khiển luồng (không kích hoạt hành động nào, chỉ "gọi người"),
// chỉnh nó không có lợi ích; đưa vào config ngược lại ám chỉ có thể chỉnh ra khác biệt hành vi.
const repeatNotifyAt = 3

// NewDispatcher tạo Dispatcher. Trước khi dùng cần gọi Attach để đăng ký sự kiện.
func NewDispatcher(coordinator *agentcore.Agent, store *storepkg.Store) *Dispatcher {
	d := &Dispatcher{coordinator: coordinator, store: store}
	return d
}

// Enable bật phái routing; khi tắt thì EventToolExecEnd đến cũng không gửi FollowUp.
// Host bật sau khi Start/Resume hoàn tất prompt đầu tiên, tránh xung đột với luồng khởi động.
func (d *Dispatcher) Enable() { d.enabled.Store(true) }

// Attach đăng ký nghe sự kiện Coordinator; hàm trả về được gọi khi đóng để gỡ liên kết.
func (d *Dispatcher) Attach() func() {
	return d.coordinator.Subscribe(d.handle)
}

func (d *Dispatcher) handle(ev agentcore.Event) {
	if !d.enabled.Load() {
		return
	}
	// Điểm kích hoạt chính xác: subagent trả về thành công, hoặc reopen_book mở lại một cuốn đã hoàn kết vào trạng thái làm lại.
	// Cả hai đều đã tiến tầng sự kiện, cần theo ngay một lần Route để tính bước kế — reopen_book không phải subagent
	// (giai đoạn complete cần né completePhaseGate), nếu không kích hoạt ở đây thì hàng đợi làm lại sau khi mở lại sẽ không có người phái.
	// Không dùng EventModelResponse vì agentcore emit nó mỗi khi một LLM call hoàn tất,
	// sẽ đẩy lặp cùng một chỉ thị vào followUpQ; Steer kiểu truy vấn bị coordinator.md ràng buộc tiếp tục gọi subagent
	// trong cùng một turn, từ đó trúng điểm kích hoạt này.
	if ev.Type != agentcore.EventToolExecEnd || ev.IsError {
		return
	}
	if ev.Tool != "subagent" && ev.Tool != "reopen_book" {
		return
	}
	d.Dispatch()
}

// Dispatch tính routing ngay và hạ chỉ thị; có thể được Host chủ động gọi vào thời điểm đặc biệt (như sau Resume).
func (d *Dispatcher) Dispatch() {
	state := LoadState(d.store)
	inst := Route(state)
	if inst == nil {
		return
	}
	n := d.trackRepeat(inst)
	// Nhiệm vụ Writer: ngay tại thời điểm phái thì đánh dấu chương đang tiến hành, outline bên phải UI phản ánh ngay "▸ đang tiến hành",
	// không phải đợi plan_chapter thực sự chạy (plan_chapter sẽ gọi StartChapter thêm một lần nữa, idempotent).
	if inst.Agent == "writer" && inst.Chapter > 0 && d.store != nil {
		if err := d.store.Progress.ValidateChapterWork(inst.Chapter); err != nil {
			slog.Error("flow router refuses invalid writer dispatch", "module", "host.flow", "chapter", inst.Chapter, "err", err)
			return
		}
		if err := d.store.Progress.StartChapter(inst.Chapter); err != nil {
			slog.Warn("flow router pre-mark in-progress failed", "module", "host.flow", "chapter", inst.Chapter, "err", err)
		}
	}
	msg := formatDispatchMessage(inst, n)
	slog.Debug("flow router dispatch", "module", "host.flow", "agent", inst.Agent, "reason", inst.Reason, "repeat", n)
	d.coordinator.FollowUp(agentcore.UserMsg(msg))
}

// formatDispatchMessage lắp ráp tin nhắn chỉ thị hạ cho Coordinator.
// Khi n>1 thì đính kèm sự kiện lặp — báo "sau lần phái trước sự kiện routing không đổi" và mở giấy phép rà soát,
// để LLM tự phân xử thực thi như thường hay đổi phái; không làm bất kỳ rẽ nhánh cưỡng bức nào ở tầng Host.
func formatDispatchMessage(inst *Instruction, n int) string {
	msg := FormatMessage(inst)
	if n > 1 {
		msg += fmt.Sprintf(contentlang.Pick("\n（注意：本指令为第 %d 次下达——上次派发后路由事实未变化。本次允许先调 novel_context 核对事实，再裁定照常执行或改派其它子代理。）", "\n(Lưu ý: chỉ thị này là lần ra lệnh thứ %d —— sau lần phái trước sự thật định tuyến không đổi. Lần này cho phép tra novel_context để đối chiếu sự thật trước, rồi mới quyết định thực thi như thường hay đổi phái subagent khác.)"), n)
	}
	return msg
}

// SetOnRepeat đăng ký callback telemetry cho chỉ thị lặp. Phải gọi một lần trước khi Attach/bắt đầu phái.
func (d *Dispatcher) SetOnRepeat(cb func(agent, task string, n int)) {
	d.onRepeat = cb
}

// trackRepeat ghi số lần hạ liên tiếp của chỉ thị giống nhau và trả về số lần hiện tại (1 = chỉ thị mới).
// Dùng phép bằng Agent+Task (không so Reason vì Reason là văn bản phụ trợ cho người xem).
// Khi số lần đúng bằng repeatNotifyAt thì kích hoạt onRepeat một lần ngoài khóa (khi key đổi thì đếm lại rồi tái vũ trang).
func (d *Dispatcher) trackRepeat(next *Instruction) int {
	d.lastMu.Lock()
	if d.lastSent != nil && d.lastSent.Agent == next.Agent && d.lastSent.Task == next.Task {
		d.repeats++
	} else {
		cp := *next
		d.lastSent = &cp
		d.repeats = 1
	}
	n := d.repeats
	d.lastMu.Unlock()

	if n == repeatNotifyAt && d.onRepeat != nil {
		d.onRepeat(next.Agent, next.Task, n)
	}
	return n
}

// ResetRepeat xóa sạch theo dõi lặp. Host gọi khi Resume / Start mới,
// đảm bảo chỉ thị đầu tiên sau khi khôi phục hoặc tạo mới được hạ với ngữ nghĩa "lần thứ 1".
func (d *Dispatcher) ResetRepeat() {
	d.lastMu.Lock()
	defer d.lastMu.Unlock()
	d.lastSent = nil
	d.repeats = 0
}
