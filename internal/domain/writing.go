package domain

// ChapterPlan là ý tưởng viết chương, Writer tự sinh.
// Không còn bắt buộc tách cảnh, Agent tự quyết định cách tổ chức nội dung.
type ChapterPlan struct {
	Chapter    int             `json:"chapter"`
	Title      string          `json:"title"`
	Goal       string          `json:"goal"`
	Conflict   string          `json:"conflict"`
	Hook       string          `json:"hook"`
	EmotionArc string          `json:"emotion_arc,omitempty"`
	Notes      string          `json:"notes,omitempty"` // ghi chú tự do của Agent
	Contract   ChapterContract `json:"contract,omitempty"`
}

// ChapterContract là cam kết nghiệm thu chương dùng chung giữa Writer và Editor.
// Nó định nghĩa các mục đẩy tiến chương này phải hoàn thành, các mục cấm vượt giới hạn và điểm cần lưu ý khi rà soát.
type ChapterContract struct {
	RequiredBeats    []string `json:"required_beats,omitempty"`    // các mục đẩy tiến chương này bắt buộc phải có
	ForbiddenMoves   []string `json:"forbidden_moves,omitempty"`   // các bước rõ ràng không được xảy ra trong chương này
	ContinuityChecks []string `json:"continuity_checks,omitempty"` // các điểm liên tục cần đối chiếu đặc biệt ở chương này
	EvaluationFocus  []string `json:"evaluation_focus,omitempty"`  // điểm Editor cần kiểm tra trọng tâm
	EmotionTarget    string   `json:"emotion_target,omitempty"`    // tùy chọn: cảm xúc chính chương này muốn người đọc cảm nhận
	PayoffPoints     []string `json:"payoff_points,omitempty"`     // tùy chọn: điểm tình tiết/điểm tất toán mà chương then chốt muốn hồi đáp
	HookGoal         string   `json:"hook_goal,omitempty"`         // tùy chọn: ham muốn đọc tiếp mà hook cuối chương muốn thúc đẩy
}

// ChapterSummary là tóm tắt chương, dùng cho context window của các chương sau.
type ChapterSummary struct {
	Chapter    int      `json:"chapter"`
	Summary    string   `json:"summary"`
	Characters []string `json:"characters"`
	KeyEvents  []string `json:"key_events"`
}

// ArcSummary là tóm tắt cấp cung truyện, do Editor sinh khi cung truyện kết thúc.
type ArcSummary struct {
	Volume    int      `json:"volume"`
	Arc       int      `json:"arc"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	KeyEvents []string `json:"key_events"`
}

// VolumeSummary là tóm tắt cấp quyển, sinh khi quyển kết thúc.
type VolumeSummary struct {
	Volume    int      `json:"volume"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	KeyEvents []string `json:"key_events"`
}

// CharacterSnapshot là snapshot trạng thái nhân vật, ghi lại ở ranh giới cung truyện.
type CharacterSnapshot struct {
	Volume     int    `json:"volume"`
	Arc        int    `json:"arc"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Power      string `json:"power,omitempty"`
	Motivation string `json:"motivation"`
	Relations  string `json:"relations,omitempty"`
}

// OutlineFeedback là phản hồi của Writer về dàn ý, tùy chọn khi commit chương.
type OutlineFeedback struct {
	Deviation  string `json:"deviation"`  // mô tả độ lệch
	Suggestion string `json:"suggestion"` // gợi ý điều chỉnh
}

// WritingStyleRules là quy tắc viết được chắt lọc từ các chương đã viết, do Editor sinh ở ranh giới cung truyện.
// Thay cho đoạn nguyên văn (style_anchors / voice_samples), dùng quy tắc thay vì bê nguyên văn.
type WritingStyleRules struct {
	Volume    int              `json:"volume"`
	Arc       int              `json:"arc"`
	Prose     []string         `json:"prose"`      // 3-5 quy tắc phong cách trần thuật, mỗi quy tắc ≤50 chữ
	Dialogue  []CharacterVoice `json:"dialogue"`   // quy tắc phong cách đối thoại của nhân vật
	Taboos    []string         `json:"taboos"`     // danh sách điều cấm kỵ
	UpdatedAt string           `json:"updated_at"` // dấu thời gian ISO8601
}

// CharacterVoice là quy tắc phong cách đối thoại của một nhân vật.
type CharacterVoice struct {
	Name  string   `json:"name"`
	Rules []string `json:"rules"` // 2-3 quy tắc đặc trưng ngôn ngữ, mỗi quy tắc ≤30 chữ
}

// RelatedChapter là chương liên quan được đề xuất đọc lại.
type RelatedChapter struct {
	Chapter int    `json:"chapter"`
	Reason  string `json:"reason"`
}

// RecallItem là thông tin dài hạn được gọi lại có chọn lọc theo tác vụ hiện tại.
// Nó không thay thế artifact chính thức, chỉ lo việc tiêm lại cho model một lượng nhỏ thông tin lịch sử thực sự liên quan ở lượt hiện tại.
type RecallItem struct {
	Kind    string `json:"kind"`
	Key     string `json:"key,omitempty"`
	Chapter int    `json:"chapter,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// CommitResult là giá trị trả về có cấu trúc của tool commit_chapter.
// Chỉ chứa các field sự thật; "bước tiếp theo làm gì" do kênh Reminder tự sinh dựa trên Progress hiện tại.
type CommitResult struct {
	Chapter        int              `json:"chapter"`
	Committed      bool             `json:"committed"`
	WordCount      int              `json:"word_count"`
	NextChapter    int              `json:"next_chapter"`
	ReviewRequired bool             `json:"review_required"`
	ReviewReason   string           `json:"review_reason,omitempty"`
	HookType       string           `json:"hook_type,omitempty"`
	DominantStrand string           `json:"dominant_strand,omitempty"`
	Feedback       *OutlineFeedback `json:"feedback,omitempty"`
	// tín hiệu phân tầng truyện dài
	ArcEnd         bool `json:"arc_end,omitempty"`
	VolumeEnd      bool `json:"volume_end,omitempty"`
	Volume         int  `json:"volume,omitempty"`
	Arc            int  `json:"arc,omitempty"`
	NeedsExpansion bool `json:"needs_expansion,omitempty"`  // cung truyện kế là khung xương, cần mở rộng chương
	NeedsNewVolume bool `json:"needs_new_volume,omitempty"` // cần Architect tạo quyển kế tiếp
	NextVolume     int  `json:"next_volume,omitempty"`      // số thứ tự cung truyện/quyển kế tiếp
	NextArc        int  `json:"next_arc,omitempty"`         // số thứ tự cung truyện kế tiếp
	// sự thật trạng thái hoàn thành: sau commit này cả cuốn sách đã hoàn thành chưa
	BookComplete bool `json:"book_complete,omitempty"`
	// snapshot Progress.Flow hiện tại (writing / reviewing / rewriting / polishing)
	Flow string `json:"flow,omitempty"`
}
