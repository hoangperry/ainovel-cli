package host

import (
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/models"
)

// makeUsageMsg dựng một tin nhắn mà callback OnMessage chấp nhận được (kèm Usage).
// Role phải đặt tường minh thành assistant: UsageTracker.Record hiện lọc theo role,
// chỉ tin nhắn assistant mới được tích lũy (các role khác tự nhiên không mang usage).
func makeUsageMsg(input, cacheRead, cacheWrite, output int) agentcore.AgentMessage {
	return agentcore.Message{
		Role: agentcore.RoleAssistant,
		Usage: &agentcore.Usage{
			Input: input, Output: output, CacheRead: cacheRead, CacheWrite: cacheWrite,
		},
	}
}

// Test_pushSample_RingBuffer kiểm chứng ngữ nghĩa luân chuyển của cửa sổ trượt:
// N lần đầu append thẳng; sau đó theo sampleIdx ghi đè mục cũ nhất. recentSums luôn phản ánh "N lần gần nhất".
func Test_pushSample_RingBuffer(t *testing.T) {
	var tot agentTotals

	for i := 1; i <= recentSampleCap; i++ {
		pushSample(&tot, i, i*100)
	}
	if got := len(tot.samples); got != recentSampleCap {
		t.Fatalf("after %d pushes, samples len=%d want %d", recentSampleCap, got, recentSampleCap)
	}

	pushSample(&tot, 999, 99900)
	if got := len(tot.samples); got != recentSampleCap {
		t.Fatalf("after overflow, samples len=%d want %d (no growth)", got, recentSampleCap)
	}
	cacheRead, input := recentSums(&tot)
	expectedCacheRead := 999
	expectedInput := 99900
	for i := 2; i <= recentSampleCap; i++ {
		expectedCacheRead += i
		expectedInput += i * 100
	}
	if cacheRead != expectedCacheRead || input != expectedInput {
		t.Fatalf("recentSums after overflow = (%d, %d), want (%d, %d)",
			cacheRead, input, expectedCacheRead, expectedInput)
	}
}

// Test_UsageTracker_RecordAccumulates kiểm chứng Record tích lũy nhiều role đúng,
// gộp tổng thể = tổng mọi role; per-role độc lập với nhau.
func Test_UsageTracker_RecordAccumulates(t *testing.T) {
	tk := NewUsageTracker(nil, nil) // modelSet=nil → đi lưới đỡ provider Cost, không ảnh hưởng logic tích lũy

	tk.Record("writer", makeUsageMsg(1000, 800, 0, 200))
	tk.Record("writer", makeUsageMsg(1500, 1200, 100, 300))
	tk.Record("editor", makeUsageMsg(500, 0, 0, 100))

	cost, in, out, cr, cw := tk.Totals()
	if in != 3000 || out != 600 || cr != 2000 || cw != 100 {
		t.Fatalf("totals = (in=%d out=%d cr=%d cw=%d), want (3000 600 2000 100)", in, out, cr, cw)
	}
	if cost != 0 {
		t.Errorf("cost should be 0 when modelSet=nil and no provider Cost, got %v", cost)
	}

	per := tk.PerAgent()
	if len(per) != 2 {
		t.Fatalf("per-agent len=%d want 2", len(per))
	}
	// PerAgent sắp giảm dần theo CacheRead: writer (2000) phải xếp trước editor (0)
	if per[0].Role != "writer" || per[1].Role != "editor" {
		t.Fatalf("per-agent order = %s,%s want writer,editor", per[0].Role, per[1].Role)
	}
	if per[0].Input != 2500 || per[0].CacheRead != 2000 {
		t.Errorf("writer totals = (in=%d cr=%d), want (2500 2000)", per[0].Input, per[0].CacheRead)
	}
}

// Test_UsageTracker_ArchitectAliasNormalized kiểm chứng architect_short/mid/long
// đều gộp về cùng một key "architect" (tránh bị các sub-role do /model chuyển đổi tách thành nhiều dòng).
func Test_UsageTracker_ArchitectAliasNormalized(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.Record("architect_short", makeUsageMsg(100, 50, 0, 20))
	tk.Record("architect_mid", makeUsageMsg(200, 100, 0, 40))
	tk.Record("architect_long", makeUsageMsg(300, 150, 0, 60))

	per := tk.PerAgent()
	if len(per) != 1 {
		t.Fatalf("aliases must merge to single role, got %d entries: %+v", len(per), per)
	}
	if per[0].Role != "architect" {
		t.Fatalf("merged role name = %q, want architect", per[0].Role)
	}
	if per[0].Input != 600 || per[0].CacheRead != 300 {
		t.Errorf("merged totals = (in=%d cr=%d), want (600 300)", per[0].Input, per[0].CacheRead)
	}
}

func Test_UsageTracker_PerModelAccumulates(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.accumulate("writer", "openrouter", "model-a", agentcore.Usage{Input: 1000, Output: 200, CacheRead: 700})
	tk.accumulate("editor", "openrouter", "model-b", agentcore.Usage{Input: 500, Output: 100})
	tk.accumulate("writer", "openrouter", "model-a", agentcore.Usage{Input: 300, Output: 80, CacheRead: 200})

	perModel := tk.PerModel()
	if len(perModel) != 2 {
		t.Fatalf("per-model len=%d want 2", len(perModel))
	}
	seen := map[string]AgentUsage{}
	for _, m := range perModel {
		seen[m.Model] = m
	}
	if seen["openrouter/model-a"].Input != 1300 || seen["openrouter/model-a"].CacheRead != 900 {
		t.Errorf("model-a totals = %+v", seen["openrouter/model-a"])
	}
	if seen["openrouter/model-b"].Output != 100 {
		t.Errorf("model-b totals = %+v", seen["openrouter/model-b"])
	}

	snap := tk.Snapshot()
	restored := NewUsageTracker(nil, nil)
	restored.applyState(snap)
	if got := restored.PerModel(); len(got) != 2 {
		t.Fatalf("restored per-model len=%d want 2: %+v", len(got), got)
	}
}

func Test_UsageTracker_RecordUsesActualUsageModel(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.Record("writer", agentcore.Message{
		Role: agentcore.RoleAssistant,
		Usage: &agentcore.Usage{
			Provider: "openrouter",
			Model:    "google/gemini-2.5-pro",
			Input:    1000,
			Output:   200,
		},
	})

	perModel := tk.PerModel()
	if len(perModel) != 1 {
		t.Fatalf("per-model len=%d want 1: %+v", len(perModel), perModel)
	}
	if perModel[0].Model != "openrouter/google/gemini-2.5-pro" {
		t.Fatalf("model key = %q, want openrouter/google/gemini-2.5-pro", perModel[0].Model)
	}
	if perModel[0].Input != 1000 || perModel[0].Output != 200 {
		t.Fatalf("model totals = %+v", perModel[0])
	}
}

func Test_UsageTracker_ProviderOnlyDoesNotInventModelKey(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	tk.Record("writer", agentcore.Message{
		Role: agentcore.RoleAssistant,
		Usage: &agentcore.Usage{
			Provider: "openrouter",
			Input:    1000,
			Output:   200,
		},
	})

	if got := tk.PerModel(); len(got) != 0 {
		t.Fatalf("provider-only usage must not create model stats without a model, got %+v", got)
	}
}

// Test_UsageTracker_RecentWindowReflectsLatest kiểm chứng cửa sổ trượt phản ánh "N lần gần nhất",
// không bị hit thấp giai đoạn đầu kéo lùi — đây chính là vấn đề "kéo lùi giai đoạn đầu vs hit thấp ổn định" mà P1 cần giải.
func Test_UsageTracker_RecentWindowReflectsLatest(t *testing.T) {
	tk := NewUsageTracker(nil, nil)

	// 5 lần đầu hit cực thấp (kịch bản chương đầu)
	for i := 0; i < 5; i++ {
		tk.Record("writer", makeUsageMsg(1000, 0, 0, 200))
	}
	// 8 lần sau (>5) hit cao (kịch bản ổn định)
	for i := 0; i < 8; i++ {
		tk.Record("writer", makeUsageMsg(1000, 900, 0, 200))
	}

	per := tk.PerAgent()
	if len(per) != 1 {
		t.Fatalf("len=%d want 1", len(per))
	}
	w := per[0]

	// Tích lũy: 8/13 lần có hit → 7200/13000 ≈ 55.4%
	cumulativeRate := float64(w.CacheRead) / float64(w.Input) * 100
	if cumulativeRate < 50 || cumulativeRate > 60 {
		t.Errorf("cumulative hit rate = %.1f%%, want ~55%%", cumulativeRate)
	}

	// Cửa sổ trượt: 10 lần gần nhất gồm 8 lần hit cao + 2 lần hit zero → 7200/10000 = 72%
	if w.RecentSamples != recentSampleCap {
		t.Errorf("recent samples = %d, want %d (window full)", w.RecentSamples, recentSampleCap)
	}
	recentRate := float64(w.RecentCacheRead) / float64(w.RecentInput) * 100
	if recentRate < 70 || recentRate > 75 {
		t.Errorf("recent hit rate = %.1f%%, want ~72%% (proves window dropped early misses)", recentRate)
	}
	// Then chốt: N lần gần nhất cao hơn rõ rệt so với tích lũy, chứng minh các 0 giai đoạn đầu đã bị đẩy khỏi cửa sổ
	if recentRate <= cumulativeRate {
		t.Errorf("recent (%.1f%%) must exceed cumulative (%.1f%%) once window slides past early misses",
			recentRate, cumulativeRate)
	}
}

// Test_computeSaved kiểm chứng thuật toán saved: CacheRead × (giá Input - giá CacheRead);
// khi chênh giá ≤ 0 hoặc InputCost ≤ 0 thì trả về 0 (phụ phí CacheWrite không trừ).
func Test_computeSaved(t *testing.T) {
	cases := []struct {
		name  string
		usage agentcore.Usage
		entry models.ModelEntry
		want  float64
	}{
		{
			name:  "anthropic 5m 命中节省 90%",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 80_000},
			entry: models.ModelEntry{InputCostPer1M: 3.0, CacheReadCostPer1M: 0.3},
			want:  80_000 * (3.0 - 0.3) / 1_000_000, // 0.216
		},
		{
			name:  "无命中 saved=0",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 0},
			entry: models.ModelEntry{InputCostPer1M: 3.0, CacheReadCostPer1M: 0.3},
			want:  0,
		},
		{
			name:  "模型未标价 saved=0",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 50_000},
			entry: models.ModelEntry{InputCostPer1M: 0, CacheReadCostPer1M: 0},
			want:  0,
		},
		{
			name:  "异常价差 saved=0",
			usage: agentcore.Usage{Input: 100_000, CacheRead: 50_000},
			entry: models.ModelEntry{InputCostPer1M: 1.0, CacheReadCostPer1M: 2.0}, // cache lại còn đắt hơn
			want:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeSaved(tc.usage, tc.entry)
			if got != tc.want {
				t.Errorf("computeSaved=%v want %v", got, tc.want)
			}
		})
	}
}

// Test_UsageTracker_CacheCapableSticky kiểm chứng CacheCapable một khi đặt true thì không lùi.
// Lịch sử đã chạy model hỗ trợ cache → dữ liệu hit tích lũy hợp lệ; giữa chừng chuyển sang model không hỗ trợ không nên khiến cờ lùi.
//
// Mô phỏng bằng cách dựng perAgent gán trực tiếp (đường resolveCost cần ModelSet+Registry, tầng integration đã phủ).
func Test_UsageTracker_CacheCapableSticky(t *testing.T) {
	tk := NewUsageTracker(nil, nil)

	// Mô phỏng "từng chạy model hỗ trợ cache + đã hit"
	tk.perAgent["writer"] = &agentTotals{
		Input: 1000, CacheRead: 500, Output: 200, CacheCapable: true,
	}
	// Sau đó nối thêm một lần "gọi model không hỗ trợ cache"
	tk.Record("writer", makeUsageMsg(500, 0, 0, 100))

	per := tk.PerAgent()
	if len(per) != 1 || per[0].Role != "writer" {
		t.Fatalf("expected single writer entry, got %+v", per)
	}
	if !per[0].CacheCapable {
		t.Errorf("CacheCapable must remain true after switching to non-capable model")
	}
	if per[0].CacheRead != 500 || per[0].Input != 1500 {
		t.Errorf("totals after merge = (in=%d cr=%d), want (1500 500)",
			per[0].Input, per[0].CacheRead)
	}
}

// Test_UsageTracker_PerAgentSkipsZero kiểm chứng role chưa tiêu thụ token không xuất hiện trong PerAgent.
func Test_UsageTracker_PerAgentSkipsZero(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	// Dựng một role nhưng không tiêu thụ token (trường hợp cực đoan)
	tk.perAgent["ghost"] = &agentTotals{}
	tk.Record("writer", makeUsageMsg(100, 50, 0, 20))

	per := tk.PerAgent()
	if len(per) != 1 || per[0].Role != "writer" {
		t.Fatalf("ghost role with zero tokens must be skipped, got %+v", per)
	}
}

// Test_UsageTracker_MissingAssistantUsageCounted kiểm chứng biên phán định đếm
// missingAssistantUsage:
//   - đường cộng dồn chỉ xét Usage != nil (không trói chặt Role)
//   - đường chẩn đoán yêu cầu Role=Assistant và Content khác rỗng — như vậy mới giống "một response LLM thật mà
//     không lấy được usage", tương ứng biểu hiện điển hình khi streaming thượng nguồn không gửi final chunk
//     include_usage của OpenAI. Các trường hợp khác (tin nhắn user/tool, assistant content rỗng)
//     đều không tính là missing.
func Test_UsageTracker_MissingAssistantUsageCounted(t *testing.T) {
	tk := NewUsageTracker(nil, nil)

	withContent := func(text string) agentcore.Message {
		return agentcore.Message{
			Role:    agentcore.RoleAssistant,
			Content: []agentcore.ContentBlock{agentcore.TextBlock(text)},
		}
	}

	// assistant + có Content + Usage nil → trông như response thật nhưng thiếu usage, tính vào chẩn đoán
	tk.Record("writer", withContent("hi"))
	tk.Record("writer", withContent("again"))
	// assistant nhưng Content rỗng → đường khôi phục ngoại lệ hoặc tin nhắn placeholder, không tính missing
	tk.Record("writer", agentcore.Message{Role: agentcore.RoleAssistant})
	// tin nhắn user/tool tự nhiên không mang usage, dù Content rỗng hay không cũng không tính missing
	tk.Record("writer", agentcore.Message{Role: agentcore.RoleUser, Content: []agentcore.ContentBlock{agentcore.TextBlock("u")}})
	tk.Record("writer", agentcore.Message{Role: agentcore.RoleTool, Content: []agentcore.ContentBlock{agentcore.TextBlock("t")}})
	// Bình thường có usage → đi đường cộng dồn, không tính vào chẩn đoán
	tk.Record("writer", makeUsageMsg(100, 50, 0, 20))

	if got := tk.MissingAssistantUsage(); got != 2 {
		t.Errorf("MissingAssistantUsage=%d, want 2", got)
	}
	_, in, _, _, _ := tk.Totals()
	if in != 100 {
		t.Errorf("正常路径累计被破坏，input=%d want 100", in)
	}
}

// Test_UsageTracker_CacheCapableFromFacts kiểm chứng CacheCapable khi registry không tra được model đó
// vẫn đánh dấu true được theo "sự thật": model của backend tự dựng / proxy nội địa thường không nằm trong
// chỉ mục pricing của BerriAI/litellm, resolveCost trả về capable=false; nhưng chỉ cần backend thật sự trả về
// CacheRead hoặc CacheWrite > 0 thì chứng minh model đó khách quan hỗ trợ prompt cache, dòng per-role
// không nên hiển thị "chưa bật".
func Test_UsageTracker_CacheCapableFromFacts(t *testing.T) {
	tk := NewUsageTracker(nil, nil) // modelSet=nil → resolveCost luôn capable=false

	// Một lần có CacheWrite (mô phỏng lần đầu ghi cache, registry không đánh capable, nhưng sự thật chứng minh có hỗ trợ)
	tk.Record("writer", makeUsageMsg(1000, 0, 200, 100))
	per := tk.PerAgent()
	if len(per) != 1 || !per[0].CacheCapable {
		t.Fatalf("CacheWrite > 0 应立即标记 CacheCapable=true，got %+v", per)
	}
	if !tk.OverallCacheCapable() {
		t.Errorf("overall CacheCapable 也应同步置 true")
	}

	// Ngược lại: role hoàn toàn không có hoạt động cache, CacheCapable phải giữ false
	tk.Record("editor", makeUsageMsg(500, 0, 0, 100))
	per = tk.PerAgent()
	for _, a := range per {
		if a.Role == "editor" && a.CacheCapable {
			t.Errorf("editor 全程无 CacheRead/Write，CacheCapable 不应被错误标记为 true")
		}
	}
}

// Test_UsageTracker_AccumulatesAnyRoleWithUsage kiểm chứng đường cộng dồn tách rời khỏi Role:
// dù về sau một adapter nào đó lắp usage vào message của role không phải assistant,
// vẫn tích lũy đúng. Giữ vững hợp đồng "luật lắp ráp tách rời luật cộng dồn".
func Test_UsageTracker_AccumulatesAnyRoleWithUsage(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	// Dựng một tin nhắn không phải assistant nhưng có Usage, về lý thuyết khá hiếm gặp
	hypothetical := agentcore.Message{
		Role:  agentcore.RoleSystem,
		Usage: &agentcore.Usage{Input: 200, Output: 50, CacheRead: 100},
	}
	tk.Record("writer", hypothetical)

	_, in, out, cr, _ := tk.Totals()
	if in != 200 || out != 50 || cr != 100 {
		t.Errorf("未按 Usage 字段累加，got (in=%d out=%d cr=%d) want (200 50 100)", in, out, cr)
	}
	if tk.MissingAssistantUsage() != 0 {
		t.Errorf("有 Usage 不应计入 missing")
	}
}

// Test_UsageTracker_OnCostCallback kiểm chứng điểm đấu nối của sentinel budget: sau mỗi lần ghi sổ
// callback ngoài lock mang theo chi phí tích lũy mới nhất (gồm cả đường provider tự báo cost).
func Test_UsageTracker_OnCostCallback(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	var got []float64
	tk.SetOnCost(func(total float64) { got = append(got, total) })

	msg := func(cost float64) agentcore.AgentMessage {
		return agentcore.Message{
			Role:  agentcore.RoleAssistant,
			Usage: &agentcore.Usage{Input: 100, Output: 10, Cost: &agentcore.Cost{Total: cost}},
		}
	}
	tk.Record("writer", msg(0.5))
	tk.Record("writer", msg(0.25))

	if len(got) != 2 || got[0] != 0.5 || got[1] != 0.75 {
		t.Fatalf("onCost should carry growing totals, got %v", got)
	}
}

// Test_UsageTracker_OnMissingUsageOnce kiểm chứng callback vùng mù chỉ kích hoạt ở lần đầu.
func Test_UsageTracker_OnMissingUsageOnce(t *testing.T) {
	tk := NewUsageTracker(nil, nil)
	fired := 0
	tk.SetOnMissingUsage(func() { fired++ })

	noUsage := agentcore.Message{Role: agentcore.RoleAssistant, Content: []agentcore.ContentBlock{agentcore.TextBlock("正文")}}
	tk.Record("writer", noUsage)
	tk.Record("writer", noUsage)
	tk.Record("editor", noUsage)

	if fired != 1 {
		t.Fatalf("onMissingUsage should fire exactly once, got %d", fired)
	}
}
