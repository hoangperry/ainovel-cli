package host

import (
	"fmt"
	"math"
	"sync/atomic"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/contentlang"
)

// Máy trạng thái budget: tiến đơn điệu, mỗi lần chuyển đúng một lần kích hoạt side effect, không lùi.
// Tăng budget = người dùng tái cấp quyền = sửa config rồi khởi động lại / Host instance mới, không lùi trạng thái trong chính instance này.
const (
	budgetNormal      int32 = iota // chưa tới mốc cảnh báo
	budgetWarned                   // đã phát cảnh báo, chưa vượt ngưỡng
	budgetStopPending              // đã vượt ngưỡng, chờ biên sub-agent để dừng
	budgetStopped                  // đã thực thi dừng
)

// BudgetSentinel giám sát chi phí tích lũy, thực thi chính sách budget của người dùng (khối config budget).
//
// Định vị hợp hiến (architecture.md §8.3/§10): không đánh giá hành vi model — vượt ngưỡng dừng tương đương người dùng
// tại thời điểm đó tự tay Abort, Host chỉ thay mặt thực thi một mệnh lệnh đã ký trước. Nó tác động đến luồng điều khiển, nên
// không phải observer, mà được định vị là thành phần chính sách Host ngang hàng với flow.Dispatcher; tầng Route/tool không hay biết.
//
// Thời điểm dừng: mặc định tại biên sub-agent (HandleEvent lắng nghe EventToolExecEnd(tool=subagent),
// cùng điểm kích hoạt với Dispatcher), không lãng phí chương đang in-flight; khi hardStop=true thì vượt ngưỡng dừng ngay.
// Ràng buộc thứ tự đăng ký: Sentinel phải đăng ký trước Dispatcher — sau khi Abort được bật thì FollowUp của
// Dispatcher tự nhiên hụt, không cần thêm nhận thức budget ở tầng route.
type BudgetSentinel struct {
	limit     float64
	warnRatio float64
	hardStop  bool

	costNow func() float64              // chi phí tích lũy hiện tại (bọc usage.Totals; có thể inject stub test)
	abort   func(reason string)         // bọc dừng máy của Host (kèm sự kiện lý do)
	report  func(level, summary string) // cổng xuất cảnh báo (emitEvent + notify, do Host inject)

	state atomic.Int32

	// Phát hiện vùng mù tính phí: model mà registry không có giá và provider không tự báo cost thì mỗi lần ghi sổ tăng $0,
	// budget thất hiệu âm thầm. Xét theo "nhiều lần liên tiếp tăng zero" thay vì total==0 — cái sau không bắt được kịch bản
	// giữa chừng long-run /model chuyển sang model không giá (total dừng ở giá trị lịch sử khác zero nhưng không tăng nữa).
	// Model miễn phí cũng trúng, nhắc "budget sẽ không kích hoạt" với chúng cũng đúng.
	lastTotal   atomic.Uint64 // math.Float64bits(chi phí tích lũy của callback lần trước)
	zeroStreak  atomic.Int32
	blindWarned atomic.Bool
}

// blindZeroStreak là số lần ghi sổ tăng zero liên tiếp trước khi cảnh báo. Model tính giá bình thường mỗi lần tăng tất > 0
// (cost là float tích lũy không làm tròn), lấy 5 chỉ để tránh nhiễu cực đoan, không phải ngưỡng chính sách điều chỉnh được.
const blindZeroStreak = 5

// NewBudgetSentinel tạo sentinel budget; khi chính sách chưa bật thì trả về nil (mọi method đều nil-safe).
func NewBudgetSentinel(cfg bootstrap.BudgetConfig, costNow func() float64, abort func(reason string), report func(level, summary string)) *BudgetSentinel {
	if !cfg.Enabled() {
		return nil
	}
	return &BudgetSentinel{
		limit:     cfg.BookUSD,
		warnRatio: cfg.WarnRatio,
		hardStop:  cfg.HardStop,
		costNow:   costNow,
		abort:     abort,
		report:    report,
	}
}

// OnCost được UsageTracker gọi sau mỗi lần ghi sổ, mang theo chi phí tích lũy mới nhất (ngoài lock).
// Một lần callback có thể nhảy liền hai bậc (normal→warned→stopPending), hai side effect mỗi cái kích hoạt một lần.
func (s *BudgetSentinel) OnCost(total float64) {
	if s == nil {
		return
	}
	if prev := s.lastTotal.Swap(math.Float64bits(total)); total == math.Float64frombits(prev) {
		if s.zeroStreak.Add(1) >= blindZeroStreak && s.blindWarned.CompareAndSwap(false, true) {
			s.report("warn", fmt.Sprintf(contentlang.Pick("预算盲区: 连续记账但累计成本停在 $%.2f 不再增长（当前模型注册表无价且 provider 未自报 cost，或为免费模型）——预算上限不会触发", "Vùng mù ngân sách: ghi sổ liên tục nhưng chi phí tích lũy dừng ở $%.2f không tăng nữa (model registry hiện không có giá và provider không tự báo cost, hoặc là model miễn phí) —— trần ngân sách sẽ không kích hoạt"), total))
		}
	} else {
		s.zeroStreak.Store(0)
	}
	if total >= s.limit*s.warnRatio && s.state.CompareAndSwap(budgetNormal, budgetWarned) {
		s.report("warn", fmt.Sprintf(contentlang.Pick("预算告警: 已花费 $%.2f，达到预算 $%.2f 的 %.0f%%", "Cảnh báo ngân sách: đã tiêu $%.2f, đạt $%.2f ở mức %.0f%% ngân sách"), total, s.limit, s.warnRatio*100))
	}
	if total >= s.limit && s.state.CompareAndSwap(budgetWarned, budgetStopPending) {
		if s.hardStop {
			s.report("error", fmt.Sprintf(contentlang.Pick("预算用尽: 已花费 $%.2f，超出预算 $%.2f，立即停机", "Hết ngân sách: đã tiêu $%.2f, vượt ngân sách $%.2f, dừng máy ngay"), total, s.limit))
			s.stop(total)
			return
		}
		s.report("error", fmt.Sprintf(contentlang.Pick("预算用尽: 已花费 $%.2f，超出预算 $%.2f，将在当前子代理任务结束后停机", "Hết ngân sách: đã tiêu $%.2f, vượt ngân sách $%.2f, sẽ dừng máy sau khi nhiệm vụ subagent hiện tại kết thúc"), total, s.limit))
	}
}

// HandleEvent thực thi việc dừng đang chờ tại biên sub-agent. Đăng ký phải trước Dispatcher.
// Không bỏ qua IsError — trả về lỗi cũng là một biên, việc dừng không nên hoãn vì sub-agent thất bại.
func (s *BudgetSentinel) HandleEvent(ev agentcore.Event) {
	if s == nil {
		return
	}
	if ev.Type != agentcore.EventToolExecEnd || ev.Tool != "subagent" {
		return
	}
	if s.state.Load() != budgetStopPending {
		return
	}
	s.stop(s.costNow())
}

func (s *BudgetSentinel) stop(total float64) {
	if s.state.CompareAndSwap(budgetStopPending, budgetStopped) {
		s.abort(fmt.Sprintf(contentlang.Pick("预算停机: 已花费 $%.2f，超出预算 $%.2f；上调 budget.book_usd 后可恢复续跑", "Dừng máy do ngân sách: đã tiêu $%.2f, vượt ngân sách $%.2f; tăng budget.book_usd để khôi phục chạy tiếp"), total, s.limit))
	}
}

// Refuse là kiểm tra tiền điều kiện khi khởi động: budget đã vượt thì trả về lỗi từ chối (các đường khôi phục Start/Resume/Continue gọi).
// Người dùng tăng budget = tái cấp quyền, dưới config mới Refuse tự nhiên cho qua.
func (s *BudgetSentinel) Refuse() error {
	if s == nil {
		return nil
	}
	if cost := s.costNow(); cost >= s.limit {
		return fmt.Errorf(contentlang.Pick("本书已花费 $%.2f，达到预算上限 $%.2f；请上调配置 budget.book_usd 后重试", "Sách này đã tiêu $%.2f, chạm trần ngân sách $%.2f; hãy tăng cấu hình budget.book_usd rồi thử lại"), cost, s.limit)
	}
	return nil
}

// Limit trả về trần budget (dùng để hiển thị UI); chưa bật thì trả về 0.
func (s *BudgetSentinel) Limit() float64 {
	if s == nil {
		return 0
	}
	return s.limit
}
