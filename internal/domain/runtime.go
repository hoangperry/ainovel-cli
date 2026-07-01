package domain

import (
	"strings"

	"github.com/voocel/ainovel-cli/internal/contentlang"
)

// Phase biểu thị giai đoạn sáng tác tiểu thuyết.
type Phase string

const (
	PhaseInit     Phase = "init"
	PhasePremise  Phase = "premise"
	PhaseOutline  Phase = "outline"
	PhaseWriting  Phase = "writing"
	PhaseComplete Phase = "complete"
)

// FlowState là loại quy trình đang hoạt động hiện tại, dùng để khôi phục checkpoint.
type FlowState string

const (
	FlowWriting   FlowState = "writing"
	FlowReviewing FlowState = "reviewing"
	FlowRewriting FlowState = "rewriting"
	FlowPolishing FlowState = "polishing"
	FlowSteering  FlowState = "steering"
)

// PlanningTier biểu thị cấp độ độ dài của kế hoạch tác phẩm.
type PlanningTier string

const (
	PlanningTierShort PlanningTier = "short"
	PlanningTierMid   PlanningTier = "mid"
	PlanningTierLong  PlanningTier = "long"
)

// Progress theo dõi tiến độ, persist vào meta/progress.json.
type Progress struct {
	NovelName         string      `json:"novel_name"`
	Phase             Phase       `json:"phase"`
	CurrentChapter    int         `json:"current_chapter"`
	TotalChapters     int         `json:"total_chapters"`
	CompletedChapters []int       `json:"completed_chapters"`
	TotalWordCount    int         `json:"total_word_count"`
	ChapterWordCounts map[int]int `json:"chapter_word_counts,omitempty"` // số chữ mỗi chương, hỗ trợ chỉnh tổng số chữ khi viết lại
	InProgressChapter int         `json:"in_progress_chapter,omitempty"` // chương đang viết (khôi phục cấp cảnh)
	CompletedScenes   []int       `json:"completed_scenes,omitempty"`    // số thứ tự cảnh đã hoàn thành của chương hiện tại
	Flow              FlowState   `json:"flow,omitempty"`                // quy trình hiện tại
	PendingRewrites   []int       `json:"pending_rewrites,omitempty"`    // hàng đợi chương chờ viết lại
	RewriteReason     string      `json:"rewrite_reason,omitempty"`      // lý do viết lại
	StrandHistory     []string    `json:"strand_history,omitempty"`      // ghi dominant_strand theo thứ tự chương
	HookHistory       []string    `json:"hook_history,omitempty"`        // ghi hook_type theo thứ tự chương
	// theo dõi phân tầng truyện dài (chỉ dùng ở chế độ truyện dài, truyện ngắn/vừa là giá trị zero)
	CurrentVolume int  `json:"current_volume,omitempty"`
	CurrentArc    int  `json:"current_arc,omitempty"`
	Layered       bool `json:"layered,omitempty"`
	// ReopenedFromComplete đánh dấu cuốn sách này được mở lại từ trạng thái hoàn kết qua reopen để vào làm lại. Làm lại chỉ sửa các chương đã có,
	// không tăng giảm cấu trúc, nên sau khi xả hết nên cho qua theo "cấu trúc đầy đủ là hoàn kết lại" (tránh việc phục bút cuối quyển cuối bị làm lại làm xáo trộn rồi kẹt ở
	// vòng lặp vô hạn writing → viết tiếp vượt giới hạn); viết xuôi không đặt cờ này, phán định hoàn kết giữ ngữ nghĩa bảo thủ về thu gọn manh mối.
	ReopenedFromComplete bool `json:"reopened_from_complete,omitempty"`
}

// IsResumable xác định có thể khôi phục từ điểm dừng hay không.
func (p *Progress) IsResumable() bool {
	return p.Phase == PhaseWriting && p.CurrentChapter > 0
}

// NextChapter trả về số chương kế tiếp cần viết.
func (p *Progress) NextChapter() int {
	return p.LatestCompleted() + 1
}

// LatestCompleted trả về số chương đã hoàn thành lớn nhất; không có chương nào hoàn thành thì trả về 0.
func (p *Progress) LatestCompleted() int {
	max := 0
	for _, ch := range p.CompletedChapters {
		if ch > max {
			max = ch
		}
	}
	return max
}

// ExtractNovelNameFromPremise trích tên sách từ dòng đầu `# 书名` của premise (có thể bọc《》).
// Model đôi khi chép nguyên placeholder trong prompt thay vì sinh tên thật, các giá trị này coi như chưa trích được và trả về rỗng,
// để lớp trên lo dự phòng (UI hiển thị "chưa đặt tên sách"), tránh giao diện hiển thị thẳng hai chữ "书名".
func ExtractNovelNameFromPremise(premise string) string {
	for raw := range strings.SplitSeq(strings.ReplaceAll(premise, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "# ") {
			return ""
		}
		name := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "# ")), "《》\"")
		switch name {
		case "书名", "实际书名", "示例书名":
			return "" // placeholder trong prompt, không phải tên sách thật
		}
		return name
	}
	return ""
}

// ContextProfile là chiến lược nạp context, tự thích ứng theo tổng số chương.
type ContextProfile struct {
	SummaryWindow  int  // nạp tóm tắt N chương gần nhất
	TimelineWindow int  // nạp dòng thời gian N chương gần nhất
	Layered        bool // true = bật nạp tóm tắt phân tầng (tóm tắt quyển+tóm tắt cung truyện+tóm tắt chương)
}

// MemoryPolicy biểu thị chiến lược dùng bộ nhớ được chia sẻ ở runtime.
// Nó vừa dùng cho xuất context, vừa dùng cho quyết định handoff / reminder ở lớp Host.
type MemoryPolicy struct {
	Mode                string `json:"mode,omitempty"`
	SummaryWindow       int    `json:"summary_window,omitempty"`
	TimelineWindow      int    `json:"timeline_window,omitempty"`
	LayeredSummaries    bool   `json:"layered_summaries,omitempty"`
	SummaryStrategy     string `json:"summary_strategy,omitempty"`
	WorkingRefresh      string `json:"working_refresh,omitempty"`
	EpisodicRefresh     string `json:"episodic_refresh,omitempty"`
	PlanningRefresh     string `json:"planning_refresh,omitempty"`
	FoundationRefresh   string `json:"foundation_refresh,omitempty"`
	PlanningFocus       string `json:"planning_focus,omitempty"`
	FoundationFocus     string `json:"foundation_focus,omitempty"`
	PreviousTailChars   int    `json:"previous_tail_chars,omitempty"`
	ChapterPlanEnabled  bool   `json:"chapter_plan_enabled,omitempty"`
	RelatedLookup       bool   `json:"related_chapter_lookup,omitempty"`
	CurrentOutlineBound bool   `json:"current_outline_bound,omitempty"`
	TotalChapters       int    `json:"total_chapters,omitempty"`
	HandoffPreferred    bool   `json:"handoff_preferred,omitempty"`
	ReadOnlyThreshold   int    `json:"read_only_threshold,omitempty"`
}

// NewContextProfile tính chiến lược context theo tổng số chương.
func NewContextProfile(totalChapters int) ContextProfile {
	switch {
	case totalChapters <= 15:
		return ContextProfile{SummaryWindow: 10, TimelineWindow: 10}
	case totalChapters <= 50:
		return ContextProfile{SummaryWindow: 5, TimelineWindow: 8}
	default:
		return ContextProfile{SummaryWindow: 3, TimelineWindow: 5, Layered: true}
	}
}

// NewChapterMemoryPolicy sinh chiến lược bộ nhớ runtime cấp chương theo tiến độ và chiến lược context.
func NewChapterMemoryPolicy(progress *Progress, profile ContextProfile, currentOutlineBound bool) MemoryPolicy {
	policy := MemoryPolicy{
		Mode:                "chapter",
		SummaryWindow:       profile.SummaryWindow,
		TimelineWindow:      profile.TimelineWindow,
		LayeredSummaries:    profile.Layered,
		WorkingRefresh:      contentlang.Pick("每次按章节加载时刷新", "Làm mới mỗi lần tải theo chương"),
		EpisodicRefresh:     contentlang.Pick("随章节提交、评审和长篇状态变更刷新", "Làm mới khi commit chương, rà soát và thay đổi trạng thái truyện dài"),
		PreviousTailChars:   800,
		ChapterPlanEnabled:  true,
		CurrentOutlineBound: currentOutlineBound,
		ReadOnlyThreshold:   5,
	}
	if profile.Layered {
		policy.SummaryStrategy = contentlang.Pick("卷摘要+弧摘要+最近章节摘要", "Tóm tắt quyển + tóm tắt cung truyện + tóm tắt chương gần nhất")
	} else {
		policy.SummaryStrategy = contentlang.Pick("最近章节摘要", "Tóm tắt chương gần nhất")
	}
	if progress != nil {
		policy.TotalChapters = progress.TotalChapters
		if progress.TotalChapters > 30 {
			policy.RelatedLookup = true
		}
		if progress.Flow == FlowReviewing || progress.Flow == FlowRewriting || progress.Flow == FlowPolishing {
			policy.HandoffPreferred = true
		}
		if progress.Layered && len(progress.CompletedChapters) >= 6 {
			policy.HandoffPreferred = true
		}
		if len(progress.CompletedChapters) >= 12 {
			policy.HandoffPreferred = true
		}
		if progress.Layered && len(progress.CompletedChapters) >= 6 {
			policy.ReadOnlyThreshold = 4
		}
		if len(progress.CompletedChapters) >= 12 {
			policy.ReadOnlyThreshold = 4
		}
	}
	return policy
}

// NewArchitectMemoryPolicy trả về chiến lược bộ nhớ dùng ở giai đoạn lập kế hoạch.
func NewArchitectMemoryPolicy() MemoryPolicy {
	return MemoryPolicy{
		Mode:               "architect",
		PlanningRefresh:    contentlang.Pick("卷弧结构、指南针或摘要更新时刷新", "Làm mới khi cấu trúc quyển/cung truyện, compass hoặc tóm tắt cập nhật"),
		FoundationRefresh:  contentlang.Pick("角色、伏笔、设定变更时刷新", "Làm mới khi nhân vật, phục bút, thiết lập thay đổi"),
		PlanningFocus:      contentlang.Pick("分层大纲、指南针、卷摘要", "Dàn ý phân tầng, compass, tóm tắt quyển"),
		FoundationFocus:    contentlang.Pick("角色设定、角色快照、伏笔台账", "Thiết lập nhân vật, snapshot nhân vật, sổ cái phục bút"),
		HandoffPreferred:   true,
		ChapterPlanEnabled: false,
		ReadOnlyThreshold:  4,
	}
}

// RunMeta là metadata phiên chạy, persist vào meta/run.json.
type RunMeta struct {
	StartedAt    string       `json:"started_at"`
	Provider     string       `json:"provider,omitempty"`
	Style        string       `json:"style"`
	Model        string       `json:"model"`
	PlanningTier PlanningTier `json:"planning_tier,omitempty"`
	SteerHistory []SteerEntry `json:"steer_history,omitempty"`
	PendingSteer string       `json:"pending_steer,omitempty"` // lệnh Steer chưa hoàn thành, tiêm lại khi khôi phục sau gián đoạn
}

// SteerEntry là bản ghi can thiệp của người dùng.
type SteerEntry struct {
	Input     string `json:"input"`
	Timestamp string `json:"timestamp"`
}

// UserDirective là yêu cầu sáng tác dài hạn do người dùng đưa ra, có hiệu lực liên tục xuyên suốt các chương.
// Persist vào meta/user_directives.json, do novel_context tiêm vào
// working_memory.user_directives để mọi subagent tuân theo.
//
// Chapter/TotalChapters là snapshot tiến độ tại thời điểm đưa ra: cho lệnh có điểm bắt đầu hiệu lực rõ ràng (không truy ngược
// các chương trước), đồng thời cho phép bên đọc phán định một lệnh dạng tương đối bị lưu nhầm (như "tăng 10 chương") là đã thỏa mãn,
// thay vì mỗi lần đọc lại lại thực thi thêm một lần.
type UserDirective struct {
	Text          string `json:"text"`
	Chapter       int    `json:"chapter"`        // tiến độ viết tại thời điểm đưa ra
	TotalChapters int    `json:"total_chapters"` // tổng số chương theo kế hoạch tại thời điểm đưa ra
	CreatedAt     string `json:"created_at"`     // RFC3339
}
