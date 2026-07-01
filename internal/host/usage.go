package host

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/models"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// recentSampleCap là kích thước cửa sổ trượt: chỉ giữ mẫu (cacheRead, input) của N lần gọi gần nhất cho mỗi role,
// dùng để so ở cột trái tỉ lệ hit "tích lũy vs N lần gần nhất", nhận ra "kéo lùi giai đoạn đầu" vs "hit thấp ở trạng thái ổn định".
const recentSampleCap = 10

// UsageTracker tích lũy token input/output LLM và chi phí đô của tất cả agent trong cả phiên.
//
// Cơ chế hoạt động:
//   - Mỗi khi callback OnMessage của agent kích hoạt thì gọi Record(agentName, msg)
//   - agentName ánh xạ sang role (architect_* gộp thành architect), tra model mà role đó đang gắn trong ModelSet
//   - Dùng models.DefaultRegistry tra giá model, nhân cộng theo bốn khoản input không cache/output/cache read/cache write
//   - Khi registry không có model này thì lui về msg.Usage.Cost.Total (provider tự mang, có thể là 0)
//   - Sau khi đổi nóng model (/model) các tin nhắn về sau tự tính giá theo model mới, tin nhắn cũ giữ chi phí cũ
//
// Đồng thời duy trì chiều per-role (writer/editor/architect/coordinator):
//   - Dữ liệu hit tích lũy → hiệu quả tối ưu tổng thể
//   - Cửa sổ trượt N lần gần nhất → phân biệt kéo lùi giai đoạn đầu vs hit thấp ở trạng thái ổn định
//   - Cờ CacheCapable → phân biệt "chưa bật" và "thật sự 0% hit"
//
// An toàn luồng.
type UsageTracker struct {
	mu       sync.Mutex
	overall  agentTotals
	perAgent map[string]*agentTotals // key là tên role sau khi agentRoleName gộp
	perModel map[string]*agentTotals // key là provider/model; khi provider không rõ thì suy biến thành model
	modelSet *bootstrap.ModelSet
	store    *storepkg.Store // có thể nil (kịch bản test), khi nil thì mọi method persist im lặng noop

	// missingAssistantUsage tích lũy số lần "nhận tin nhắn assistant nhưng Usage là nil".
	// Thực nghiệm cho thấy chủ yếu xảy ra khi backend tương thích OpenAI tự dựng không gửi final usage chunk
	// theo protocol stream_options.include_usage của OpenAI ở cuối streaming — partial.Usage
	// luôn nil, mọi field tích lũy dừng ở 0. Bộ đếm cho UI nói thẳng với người dùng "là thượng nguồn không trả
	// usage chứ không phải bên này hỏng", thay vì cố đào bới code panel cache.
	missingAssistantUsage int
	loggedMissingUsage    bool // cả phiên chỉ warn một lần, tránh làm ngập tui.log

	// saveCh được Record kích hoạt không chặn sau khi cộng dồn; autoSaveLoop lắng nghe và ghi xuống đĩa theo debounce.
	// buffered=1: nhiều Record liên tiếp gộp thành một tín hiệu ghi đĩa; đầy thì bỏ thẳng, tick sau ghi luôn.
	saveCh chan struct{}

	// onCost được gọi ngoài lock sau mỗi lần ghi sổ, mang theo chi phí tích lũy mới nhất (BudgetSentinel kiểm tra vượt ngưỡng).
	// Phải đặt qua SetOnCost trước khi Record đồng thời bắt đầu, sau đó chỉ đọc.
	onCost func(total float64)

	// onMissingUsage được gọi một lần khi lần đầu phát hiện "tin nhắn assistant không có Usage" (cùng lúc với slog warn).
	// Khi budget được bật thì điều này nghĩa là vùng mù tính phí — chi phí luôn 0, budget không bao giờ kích hoạt, phải gọi người.
	onMissingUsage func()
}

// usageSample là mẫu hit của một lần OnMessage, chỉ ghi tử số mẫu số của tỉ lệ hit.
type usageSample struct {
	CacheRead int
	Input     int
}

// agentTotals là bộ đếm tích lũy của một agent.
//   - Saved là chênh lệch "nếu tính theo giá không cache" suy ngược từ dữ liệu hit hiện tại
//   - CacheCapable chỉ đặt true sau khi role đó trải qua ít nhất một lần gọi "model đã biết hỗ trợ cache"
//   - samples là ring buffer độ dài cố định, recentSampleCap lần đầu append thẳng, sau đó luân chuyển theo sampleIdx
type agentTotals struct {
	Input        int
	Output       int
	CacheRead    int
	CacheWrite   int
	Cost         float64
	Saved        float64
	CacheCapable bool
	samples      []usageSample
	sampleIdx    int
}

func NewUsageTracker(set *bootstrap.ModelSet, store *storepkg.Store) *UsageTracker {
	return &UsageTracker{
		modelSet: set,
		store:    store,
		perAgent: make(map[string]*agentTotals, 4),
		perModel: make(map[string]*agentTotals, 4),
		saveCh:   make(chan struct{}, 1),
	}
}

// Record phân phối một tin nhắn agent vào hai đường cộng dồn / chẩn đoán.
//
// Cộng dồn chỉ xét Usage có tồn tại hay không — "tin nhắn nào mang Usage" là chi tiết lắp ráp của
// adapter agentcore/litellm (protocol thượng nguồn đặt usage ở đỉnh response), luật lắp ráp đổi về sau cũng không phải động vào đây.
// Chẩn đoán yêu cầu Role=Assistant và Content khác rỗng, tránh AbortMsg / khôi phục ngoại lệ / tool /
// tin nhắn user làm ô nhiễm bộ đếm missingAssistantUsage.
func (t *UsageTracker) Record(agentName string, msg agentcore.AgentMessage) {
	if t == nil {
		return
	}
	m, ok := msg.(agentcore.Message)
	if !ok {
		return
	}
	if m.Usage == nil {
		if m.Role == agentcore.RoleAssistant && len(m.Content) > 0 {
			t.flagMissingUsage(agentName)
		}
		return
	}
	role := agentRoleName(agentName)
	provider, modelName := usageActualModel(m.Usage)
	t.accumulate(role, provider, modelName, *m.Usage)
}

func usageActualModel(u *agentcore.Usage) (provider, modelName string) {
	if u == nil {
		return "", ""
	}
	return strings.TrimSpace(u.Provider), strings.TrimSpace(u.Model)
}

// flagMissingUsage tích lũy một sự kiện "trông như response LLM thật nhưng không lấy được usage", cả phiên chỉ in một lần
// log warn để tránh làm ngập tui.log.
func (t *UsageTracker) flagMissingUsage(agentName string) {
	t.mu.Lock()
	t.missingAssistantUsage++
	shouldLog := !t.loggedMissingUsage
	t.loggedMissingUsage = true
	t.mu.Unlock()
	if shouldLog {
		slog.Warn(i18n.T("log.usage.missing_usage_data"),
			"module", "usage", "agent", agentName)
		if t.onMissingUsage != nil {
			t.onMissingUsage()
		}
	}
	t.notifyDirty()
}

// SetOnMissingUsage đăng ký callback một-lần cho "lần đầu phát hiện thiếu usage".
// Phải gọi một lần trong giai đoạn dựng Host, trước khi Record đồng thời bắt đầu.
func (t *UsageTracker) SetOnMissingUsage(cb func()) {
	if t == nil {
		return
	}
	t.onMissingUsage = cb
}

// notifyDirty kích hoạt không chặn một tín hiệu ghi đĩa, do autoSaveLoop thực sự ghi theo debounce.
// Kênh tín hiệu buffered=1: nhiều Record liên tiếp gộp thành một yêu cầu lưu là đủ.
func (t *UsageTracker) notifyDirty() {
	if t == nil || t.saveCh == nil {
		return
	}
	select {
	case t.saveCh <- struct{}{}:
	default:
	}
}

// accumulate cộng dồn một tin nhắn có Usage vào ba bộ đếm overall / per-role / per-model.
// provider/model rỗng nghĩa là "dùng ModelSet hiện tại lấy model tương ứng role" (đường realtime); khác rỗng nghĩa là
// "ép tính giá theo model chỉ định" (đường replay dùng _meta trong session jsonl).
// resolveCost chạy ngoài lock (nó chỉ đọc modelSet/Registry), trong lock chỉ làm phép cộng.
func (t *UsageTracker) accumulate(role, provider, modelName string, u agentcore.Usage) {
	provider, modelName = t.effectiveModel(role, provider, modelName)
	cost, saved, capable := t.resolveCost(modelName, u)

	t.mu.Lock()
	addUsage(&t.overall, u, cost, saved, capable)

	per := t.perAgent[role]
	if per == nil {
		per = &agentTotals{}
		t.perAgent[role] = per
	}
	addUsage(per, u, cost, saved, capable)

	if key := modelUsageKey(provider, modelName); key != "" {
		perModel := t.perModel[key]
		if perModel == nil {
			perModel = &agentTotals{}
			t.perModel[key] = perModel
		}
		addUsage(perModel, u, cost, saved, capable)
	}
	total := t.overall.Cost
	t.mu.Unlock()

	t.notifyDirty()
	if t.onCost != nil {
		t.onCost(total)
	}
}

// SetOnCost đăng ký callback ghi sổ (mang theo chi phí tích lũy mới nhất, gọi ngoài lock).
// Phải gọi một lần trong giai đoạn dựng Host, trước khi Record đồng thời bắt đầu.
func (t *UsageTracker) SetOnCost(cb func(total float64)) {
	if t == nil {
		return
	}
	t.onCost = cb
}

func (t *UsageTracker) effectiveModel(role, provider, modelName string) (string, string) {
	provider = strings.TrimSpace(provider)
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		if t != nil && t.modelSet != nil {
			p, m, _ := t.modelSet.CurrentSelection(role)
			return p, m
		}
		return "", ""
	}
	if provider == "" && t != nil && t.modelSet != nil {
		p, m, _ := t.modelSet.CurrentSelection(role)
		if m == modelName {
			provider = p
		}
	}
	return provider, modelName
}

func modelUsageKey(provider, modelName string) string {
	provider = strings.TrimSpace(provider)
	modelName = strings.TrimSpace(modelName)
	switch {
	case modelName == "":
		return ""
	case provider == "":
		return modelName
	default:
		return provider + "/" + modelName
	}
}

// addUsage cộng token và chi phí của một lần gọi vào một bộ totals.
// Phải gọi khi đang giữ UsageTracker.mu.
//
// CacheCapable ưu tiên phán định bằng "sự thật": chỉ cần thấy CacheRead hoặc CacheWrite > 0 thì chứng minh
// thượng nguồn thực sự đã làm prompt caching. CacheReadCostPer1M của registry chỉ làm fallback,
// vì các model backend tự dựng (mimo-v2.5-pro / proxy nội địa ...) thường không nằm trong chỉ mục pricing
// BerriAI/litellm, nhưng Usage thực tế hoàn toàn có dữ liệu cache, UI không nên phán nhầm là "chưa bật".
func addUsage(t *agentTotals, u agentcore.Usage, cost, saved float64, capable bool) {
	t.Input += u.Input
	t.Output += u.Output
	t.CacheRead += u.CacheRead
	t.CacheWrite += u.CacheWrite
	t.Cost += cost
	t.Saved += saved
	if capable || u.CacheRead > 0 || u.CacheWrite > 0 {
		t.CacheCapable = true
	}
	pushSample(t, u.CacheRead, u.Input)
}

// pushSample đẩy một mẫu vào ring buffer. recentSampleCap lần đầu append thuần, sau đó luân chuyển ghi đè.
func pushSample(t *agentTotals, cacheRead, input int) {
	s := usageSample{CacheRead: cacheRead, Input: input}
	if len(t.samples) < recentSampleCap {
		t.samples = append(t.samples, s)
		return
	}
	t.samples[t.sampleIdx] = s
	t.sampleIdx = (t.sampleIdx + 1) % recentSampleCap
}

// recentSums trả về tổng cacheRead và input trong cửa sổ trượt, làm tử số mẫu số của "tỉ lệ hit N lần gần nhất".
// Dùng sum/sum thay vì "trung bình của tỉ lệ từng lần" để tránh mẫu nhỏ (input=vài trăm token) khuếch đại nhiễu.
func recentSums(t *agentTotals) (cacheRead, input int) {
	for _, s := range t.samples {
		cacheRead += s.CacheRead
		input += s.Input
	}
	return cacheRead, input
}

// Totals trả về snapshot của tổng tích lũy.
func (t *UsageTracker) Totals() (cost float64, input, output, cacheRead, cacheWrite int) {
	if t == nil {
		return 0, 0, 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall.Cost, t.overall.Input, t.overall.Output, t.overall.CacheRead, t.overall.CacheWrite
}

// SavedUSD trả về tổng số đô tiết kiệm nhờ cache hit.
func (t *UsageTracker) SavedUSD() float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall.Saved
}

// OverallRecent trả về tổng cacheRead, tổng input, số mẫu trong cửa sổ trượt (≤ recentSampleCap lần).
func (t *UsageTracker) OverallRecent() (cacheRead, input, samples int) {
	if t == nil {
		return 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	r, in := recentSums(&t.overall)
	return r, in, len(t.overall.samples)
}

// OverallCacheCapable tổng thể có trải qua ít nhất một lần model đã biết hỗ trợ cache hay không.
func (t *UsageTracker) OverallCacheCapable() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.overall.CacheCapable
}

// MissingAssistantUsage trả về số lần tích lũy "nhận tin nhắn assistant nhưng Usage là nil".
// Lớn hơn 0 thường nghĩa là streaming thượng nguồn không gửi final usage chunk của OpenAI,
// UI dựa vào đó hiển thị nhắc nhở thay vì hiểu nhầm bản thân module cache bị hỏng.
func (t *UsageTracker) MissingAssistantUsage() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.missingAssistantUsage
}

// ── persist ──

// Snapshot sao chép trạng thái tích lũy hiện tại thành domain.UsageState có thể serialize.
// samples cửa sổ trượt không vào snapshot — nó là cửa sổ chẩn đoán ngắn hạn, ý nghĩa xuyên tiến trình không lớn.
func (t *UsageTracker) Snapshot() domain.UsageState {
	if t == nil {
		return domain.UsageState{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state := domain.UsageState{
		Schema:       domain.UsageSchemaVersion,
		UpdatedAt:    time.Now(),
		Overall:      totalsSnapshot(&t.overall),
		PerAgent:     make(map[string]domain.AgentUsageTotals, len(t.perAgent)),
		PerModel:     make(map[string]domain.AgentUsageTotals, len(t.perModel)),
		MissingUsage: t.missingAssistantUsage,
	}
	for role, v := range t.perAgent {
		state.PerAgent[role] = totalsSnapshot(v)
	}
	for model, v := range t.perModel {
		state.PerModel[model] = totalsSnapshot(v)
	}
	return state
}

// LoadFromStore đọc snapshot đã persist từ store.Usage và backfill vào bộ nhớ. Trả về true nghĩa là
// đã tải thành công một trạng thái khác rỗng (schema khớp); false nghĩa là không có file hoặc không dùng được, bên gọi
// nên tiếp tục đi qua session replay để backfill một lần.
func (t *UsageTracker) LoadFromStore() (bool, error) {
	if t == nil || t.store == nil {
		return false, nil
	}
	state, err := t.store.Usage.Load()
	if err != nil {
		return false, err
	}
	if state == nil {
		return false, nil
	}
	t.applyState(*state)
	return true, nil
}

// SaveNow ghi snapshot hiện tại xuống đĩa ngay. Các đường autoSaveLoop / Close đều ghi qua nó.
func (t *UsageTracker) SaveNow() error {
	if t == nil || t.store == nil {
		return nil
	}
	return t.store.Usage.Save(t.Snapshot())
}

// StartAutoSave khởi một goroutine, lắng nghe saveCh + debounce ghi đĩa. Trước khi ctx done sẽ
// flush trạng thái chưa lưu lần cuối ra. Close kích hoạt flush + thoát qua việc cancel ctx.
func (t *UsageTracker) StartAutoSave(ctx context.Context) {
	if t == nil || t.store == nil {
		return
	}
	go t.autoSaveLoop(ctx)
}

// autoSaveLoop điều tiết tín hiệu dirty tần suất cao thành ghi đĩa mỗi 500ms một lần.
//
// Ghi chú thiết kế: 500ms là giá trị kinh nghiệm — mỗi chương 1-2 turn LLM, ghi đĩa 1-2 lần hoàn toàn chấp nhận được;
// dù người dùng thủ công ctrl+C thoát không kịp kích hoạt timer, đường hủy ctx cũng sẽ flush lần cuối.
// Crash thật sự (OS kill -9) sẽ mất phần tích lũy trong 0.5s gần nhất — session jsonl thượng nguồn vẫn là
// sự thật đầy đủ, lần khởi động sau sẽ replay từ sessions/ để vá phần chênh.
func (t *UsageTracker) autoSaveLoop(ctx context.Context) {
	const debounce = 500 * time.Millisecond
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	var pending bool
	flush := func() {
		if err := t.SaveNow(); err != nil {
			slog.Warn(i18n.T("log.usage.save_failed"), "module", "usage", "err", err)
		}
		pending = false
	}
	for {
		select {
		case <-ctx.Done():
			if pending {
				flush()
			}
			return
		case <-t.saveCh:
			if pending {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
			timer.Reset(debounce)
			pending = true
		case <-timer.C:
			flush()
		}
	}
}

// applyState ghi snapshot đã persist trở lại bộ nhớ. Chỉ gọi lúc khởi động (sau LoadFromStore / replay),
// lúc này chưa khởi động autoSaveLoop / Record cũng không kích hoạt đồng thời, có thể không giữ lock; nhưng giữ mu để phòng
// test hoặc thay đổi thứ tự gọi về sau gây ra đồng thời.
func (t *UsageTracker) applyState(state domain.UsageState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.overall = totalsFromState(state.Overall)
	if state.PerAgent == nil {
		t.perAgent = make(map[string]*agentTotals, 4)
	} else {
		t.perAgent = make(map[string]*agentTotals, len(state.PerAgent))
		for role, v := range state.PerAgent {
			tot := totalsFromState(v)
			t.perAgent[role] = &tot
		}
	}
	if state.PerModel == nil {
		t.perModel = make(map[string]*agentTotals, 4)
	} else {
		t.perModel = make(map[string]*agentTotals, len(state.PerModel))
		for model, v := range state.PerModel {
			tot := totalsFromState(v)
			t.perModel[model] = &tot
		}
	}
	t.missingAssistantUsage = state.MissingUsage
}

// totalsSnapshot sao chép agentTotals trong bộ nhớ thành domain.AgentUsageTotals có thể persist.
// ring buffer samples cố ý không mang ra — xem ghi chú UsageState.
func totalsSnapshot(t *agentTotals) domain.AgentUsageTotals {
	if t == nil {
		return domain.AgentUsageTotals{}
	}
	return domain.AgentUsageTotals{
		Input:        t.Input,
		Output:       t.Output,
		CacheRead:    t.CacheRead,
		CacheWrite:   t.CacheWrite,
		Cost:         t.Cost,
		Saved:        t.Saved,
		CacheCapable: t.CacheCapable,
	}
}

// totalsFromState khôi phục dạng đã persist thành agentTotals trong bộ nhớ. samples để trống, sau khi khởi động lại
// tích lũy lại từ 0, sau vài lượt Record là khôi phục được ngữ nghĩa "tỉ lệ hit N lần gần nhất".
func totalsFromState(s domain.AgentUsageTotals) agentTotals {
	return agentTotals{
		Input:        s.Input,
		Output:       s.Output,
		CacheRead:    s.CacheRead,
		CacheWrite:   s.CacheWrite,
		Cost:         s.Cost,
		Saved:        s.Saved,
		CacheCapable: s.CacheCapable,
	}
}

// AgentUsage là snapshot usage tích lũy của một agent (phơi ra cho UI).
type AgentUsage struct {
	Role            string
	Model           string
	Input           int
	Output          int
	CacheRead       int
	CacheWrite      int
	Cost            float64
	Saved           float64
	CacheCapable    bool
	RecentCacheRead int
	RecentInput     int
	RecentSamples   int
}

// PerAgent trả về usage tích lũy của từng role. Kết quả sắp giảm dần theo số CacheRead, role chưa tiêu thụ token thì bỏ qua.
func (t *UsageTracker) PerAgent() []AgentUsage {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]AgentUsage, 0, len(t.perAgent))
	for role, v := range t.perAgent {
		if v.Input == 0 && v.Output == 0 {
			continue
		}
		recentRead, recentInput := recentSums(v)
		out = append(out, AgentUsage{
			Role:            role,
			Input:           v.Input,
			Output:          v.Output,
			CacheRead:       v.CacheRead,
			CacheWrite:      v.CacheWrite,
			Cost:            v.Cost,
			Saved:           v.Saved,
			CacheCapable:    v.CacheCapable,
			RecentCacheRead: recentRead,
			RecentInput:     recentInput,
			RecentSamples:   len(v.samples),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CacheRead != out[j].CacheRead {
			return out[i].CacheRead > out[j].CacheRead
		}
		return out[i].Input > out[j].Input
	})
	return out
}

// PerModel trả về usage tích lũy của từng model. Kết quả sắp giảm dần theo chi phí, kế đến theo lượng input.
func (t *UsageTracker) PerModel() []AgentUsage {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]AgentUsage, 0, len(t.perModel))
	for model, v := range t.perModel {
		if v.Input == 0 && v.Output == 0 {
			continue
		}
		out = append(out, AgentUsage{
			Model:        model,
			Input:        v.Input,
			Output:       v.Output,
			CacheRead:    v.CacheRead,
			CacheWrite:   v.CacheWrite,
			Cost:         v.Cost,
			Saved:        v.Saved,
			CacheCapable: v.CacheCapable,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Input > out[j].Input
	})
	return out
}

// resolveCost đồng thời trả về cost / saved / capable của tin nhắn lần này.
//   - cost: registry trúng thì nhân cộng theo 4 khoản; không trúng thì lui về cost provider tự mang
//   - saved: chỉ > 0 khi registry trúng, CacheRead > 0, và InputCost > CacheReadCost
//   - capable: registry trúng và model đó CacheReadCostPer1M > 0 → đã biết hỗ trợ prompt caching
//
// modelName ưu tiên dùng cái bên gọi truyền vào (khi replay đến từ _meta.model trong session jsonl).
func (t *UsageTracker) resolveCost(modelName string, u agentcore.Usage) (cost, saved float64, capable bool) {
	if entry, ok := models.DefaultRegistry().Resolve(modelName); ok {
		c := computeCost(u, *entry)
		s := computeSaved(u, *entry)
		canCache := entry.CacheReadCostPer1M > 0
		if c > 0 {
			return c, s, canCache
		}
	}
	if u.Cost != nil {
		return u.Cost.Total, 0, false
	}
	return 0, 0, false
}

// agentRoleName gộp tên subagent về tên role.
// architect_short/mid/long đều gộp về architect; các tên khác trả về nguyên trạng.
func agentRoleName(agentName string) string {
	if strings.HasPrefix(agentName, "architect_") {
		return "architect"
	}
	return agentName
}

// computeCost tính chi phí đô của lần gọi này theo đơn giá $/1M tokens.
//
// Tiền đề ngữ nghĩa (do các provider litellm bảo đảm thống nhất, xem điểm lắp ráp Usage của anthropic.go / bedrock.go /
// openai.go / gemini.go / compat.go):
//
//	u.Input  = toàn bộ token input, **bao gồm** CacheRead; không gồm CacheWrite
//	u.Output = token output
//
// Do đó nonCachedInput = u.Input - u.CacheRead đúng với mọi provider.
// Nhánh lưới đỡ được giữ lại để khi về sau một provider nào đó trả nhầm dữ liệu bẩn thì không sập.
func computeCost(u agentcore.Usage, e models.ModelEntry) float64 {
	nonCachedInput := u.Input - u.CacheRead
	if nonCachedInput < 0 {
		nonCachedInput = u.Input
	}
	c := 0.0
	c += float64(nonCachedInput) * e.InputCostPer1M / 1_000_000
	c += float64(u.Output) * e.OutputCostPer1M / 1_000_000
	c += float64(u.CacheRead) * e.CacheReadCostPer1M / 1_000_000
	c += float64(u.CacheWrite) * e.CacheWriteCostPer1M / 1_000_000
	return c
}

// computeSaved ước tính số đô tiết kiệm khi CacheRead hit so với "tính theo giá input thường".
// Lưu ý phần phụ phí của CacheWrite không được trừ — nó thuộc khoản đầu tư cần thiết "lót đường cho hit về sau",
// lợi ích thật dựa vào CacheRead về sau tích lũy thu hồi.
func computeSaved(u agentcore.Usage, e models.ModelEntry) float64 {
	if u.CacheRead <= 0 || e.InputCostPer1M <= 0 {
		return 0
	}
	delta := e.InputCostPer1M - e.CacheReadCostPer1M
	if delta <= 0 {
		return 0
	}
	return float64(u.CacheRead) * delta / 1_000_000
}
