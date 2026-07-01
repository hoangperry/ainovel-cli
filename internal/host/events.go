package host

import (
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
)

// Event là sự kiện có cấu trúc mà TUI tiêu thụ.
//
// Với hai loại sự kiện gọi TOOL / DISPATCH, lần bắt đầu và kết thúc của cùng một lần gọi dùng chung một ID:
// khi bắt đầu phát sự kiện có FinishedAt là giá trị zero (TUI render kiểu "đang chạy");
// khi kết thúc phát thêm một sự kiện cùng ID, điền FinishedAt + Duration (+ Failed),
// TUI dựa theo ID định vị dòng gốc và cập nhật tại chỗ, tránh dư thừa "một dòng bắt đầu, một dòng hoàn thành".
//
// Các sự kiện không phải loại gọi như SYSTEM / ERROR / CONTEXT có ID rỗng, mỗi sự kiện được nối thêm độc lập.
type Event struct {
	ID         string    // dùng chung cho bắt đầu/kết thúc của cùng một lần gọi; sự kiện không phải loại gọi thì rỗng
	Time       time.Time // thời điểm phát lần đầu (thời điểm bắt đầu)
	FinishedAt time.Time // giá trị zero = đang chạy; khác zero = đã hoàn thành
	Failed     bool      // đã hoàn thành nhưng thất bại (chỉ có ý nghĩa ở trạng thái hoàn thành)
	Category   string    // DISPATCH / TOOL / SYSTEM / REVIEW / CHECK / ERROR / CONTEXT
	Agent      string    // agent sinh ra sự kiện
	Summary    string
	Detail     string        // nội dung đầy đủ, ghi vào log không cắt bớt để rà soát; nếu rỗng thì quay về Summary. UI chỉ đọc Summary
	Kind       string        // phân loại lỗi (như stream_idle), xuất kèm log để lọc/cảnh báo; rỗng thì không xuất
	Level      string        // info / warn / error / success
	Depth      int           // 0 = tầng coordinator, 1 = tầng sub-agent
	Duration   time.Duration // thời gian thực thi khi hoàn thành
}

// Running trả về sự kiện có đang chạy hay không.
// Chỉ sự kiện loại gọi (TOOL / DISPATCH có ID) mới có thể đang chạy; loại khác luôn trả về false.
func (e Event) Running() bool {
	return e.ID != "" && e.FinishedAt.IsZero()
}

// UISnapshot là snapshot trạng thái tổng hợp cần cho việc render của TUI.
type UISnapshot struct {
	Provider           string
	NovelName          string
	ModelName          string
	ModelContextWindow int // context window của model mặc định hiện tại (phân giải realtime khi /model chuyển đổi)
	Style              string
	RuntimeState       string // idle / running / pausing / paused / completed
	StatusLabel        string
	Phase              string
	Flow               string
	CurrentChapter     int
	TotalChapters      int
	CompletedCount     int
	TotalWordCount     int
	InProgressChapter  int
	PendingRewrites    []int
	RewriteReason      string
	PendingSteer       string
	RecoveryLabel      string
	IsRunning          bool
	Agents             []AgentSnapshot

	// context
	ContextTokens         int
	ContextWindow         int
	ContextPercent        float64
	ContextScope          string
	ContextStrategy       string
	ContextActiveMessages int
	ContextSummaryCount   int
	ContextCompactedCount int
	ContextKeptCount      int

	// usage tích lũy (toàn bộ phiên, xuyên suốt mọi agent và lần chuyển model)
	TotalInputTokens      int
	TotalOutputTokens     int
	TotalCacheReadTokens  int
	TotalCacheWriteTokens int
	TotalCostUSD          float64
	TotalSavedUSD         float64 // số đô tiết kiệm nhờ CacheRead hit (so với tính giá toàn bộ theo giá input không cache)
	BudgetLimitUSD        float64 // trần budget (config budget.book_usd); 0 = chưa bật

	// chẩn đoán cache
	OverallCacheCapable    bool // ít nhất một role đã chạy model hỗ trợ prompt cache (phân biệt "chưa bật" và "0% hit")
	OverallRecentCacheRead int  // tổng cacheRead của N lần gần nhất trong cửa sổ trượt
	OverallRecentInput     int  // tổng input của N lần gần nhất trong cửa sổ trượt
	OverallRecentSamples   int  // số mẫu trong cửa sổ trượt (≤ recentSampleCap)

	// MissingAssistantUsage > 0 thường nghĩa là streaming phía thượng nguồn không gửi
	// final usage chunk theo protocol stream_options.include_usage của OpenAI (hay gặp ở proxy tự dựng),
	// khiến UsageTracker không nhận được dữ liệu tích lũy nào. UI dựa vào đó nhắc rõ người dùng kiểm tra backend,
	// tránh để người dùng hiểu nhầm là bản thân module cache bị hỏng.
	MissingAssistantUsage int

	// cache theo chiều per-role, sắp giảm dần theo CacheRead, đã lọc role chưa tiêu thụ token
	CachePerAgent []AgentCacheStat
	CachePerModel []AgentCacheStat

	// thiết lập cơ bản
	Premise          string
	Outline          []OutlineSnapshot
	Characters       []string
	SupportingCount  int      // tổng số nhân vật thứ yếu trong danh bạ vai phụ
	RecentSupporting []string // nhân vật thứ yếu hoạt động gần đây (tối đa 5, sắp giảm dần theo LastSeenChapter)
	Layered          bool
	CurrentVolumeArc string
	NextVolumeTitle  string
	CompassDirection string
	CompassScale     string

	// chi tiết
	LastCommitSummary  string
	LastReviewSummary  string
	LastCheckpointName string
	RecentSummaries    []string
}

// OutlineSnapshot là tóm tắt hiển thị của mục dàn ý.
type OutlineSnapshot struct {
	Chapter   int
	Title     string
	CoreEvent string
}

// AgentSnapshot là phép chiếu hiển thị trạng thái của Agent.
type AgentSnapshot struct {
	Name      string
	State     string
	TaskID    string
	TaskKind  string
	Summary   string
	Tool      string
	Turn      int
	Context   AgentContextSnapshot
	UpdatedAt time.Time
}

// AgentCacheStat là cache hit tích lũy của một agent (chiếu sang cột trái).
// HitRate = CacheRead / Input; Input ở tầng litellm đã thống nhất ngữ nghĩa "đã gồm CacheRead".
//
// CacheCapable dùng để phân biệt hai loại 0% hit:
//   - true  → model hỗ trợ prompt cache, 0% là do prompt thiết kế tệ hoặc prefix không ổn định, cần tối ưu
//   - false → model/provider không hỗ trợ prompt cache, 0% là điều dự kiến, không cần rà soát
//
// Recent* là dữ liệu hit của cửa sổ trượt (N lần gọi gần nhất), so với tích lũy có thể nhận ra "kéo lùi giai đoạn đầu" vs "hit thấp ở trạng thái ổn định".
type AgentCacheStat struct {
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

// AgentContextSnapshot là tình trạng dùng context của Agent.
type AgentContextSnapshot struct {
	Tokens          int
	ContextWindow   int
	Percent         float64
	Scope           string
	Strategy        string
	ActiveMessages  int
	SummaryMessages int
	CompactedCount  int
	KeptCount       int
}

// CoCreateMessage là tin nhắn của cuộc đối thoại đồng sáng tạo.
type CoCreateMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CoCreateReply là phản hồi LLM của cuộc đối thoại đồng sáng tạo. Raw giữ nguyên văn đầy đủ bốn đoạn của model,
// dùng để ghi ngược vào history cho model lượt sau thấy [DRAFT] của chính mình ở lượt trước, nhờ đó thật sự
// cập nhật tích lũy trên bản nháp đã có (chỉ riêng Message không chứa [DRAFT], sẽ khiến model mỗi lượt phải quy nạp lại từ đối thoại).
// Suggestions là phần AI chủ động đưa ra "điều bạn có thể muốn nói tiếp", khi người dùng bí thì bấm phím số để điền nhanh vào ô nhập.
type CoCreateReply struct {
	Message     string
	Prompt      string
	Ready       bool
	Suggestions []string
	Raw         string
}

// ReplayDeltaText trích xuất văn bản stream có thể replay từ mục trong hàng đợi runtime.
func ReplayDeltaText(item domain.RuntimeQueueItem) string {
	if payload, ok := item.Payload.(map[string]any); ok {
		if text, ok := payload["delta"].(string); ok {
			return text
		}
	}
	return ""
}

// BuildStartPrompt gói nhu cầu của người dùng thành prompt khởi động cho Coordinator.
func BuildStartPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	return contentlang.Pick("请根据以下创作要求开始创作一部小说。进入规划后，Premise 第一行必须输出 `# 书名`。章节数量由你根据故事需要自行决定；若题材与冲突天然适合长篇连载，请优先规划为分层长篇结构，而不是压缩成短篇式梗概。\n\n[创作要求]\n", "Hãy bắt đầu sáng tác một cuốn tiểu thuyết theo yêu cầu sáng tác dưới đây. Sau khi vào lập kế hoạch, dòng đầu tiên của Premise bắt buộc phải xuất ra `# 书名`. Số lượng chương do bạn tự quyết theo nhu cầu câu chuyện; nếu đề tài và xung đột tự nhiên phù hợp với truyện dài nhiều kỳ, hãy ưu tiên lập kế hoạch theo kết cấu trường thiên phân tầng, thay vì nén thành dàn ý kiểu truyện ngắn.\n\n[创作要求]\n") +
		prompt +
		contentlang.Pick("\n\n若某些细节未明确，请在不违背用户方向的前提下自行补全。", "\n\nNếu một số chi tiết chưa rõ, hãy tự bổ sung trong khuôn khổ không đi ngược định hướng của người dùng.")
}
