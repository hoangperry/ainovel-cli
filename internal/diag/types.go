package diag

// Severity biểu thị mức độ nghiêm trọng của một phát hiện.
type Severity string

const (
	SevCritical Severity = "critical" // Chặn tiến độ hoặc hỏng dữ liệu
	SevWarning  Severity = "warning"  // Có thể giảm chất lượng hoặc phí token
	SevInfo     Severity = "info"     // Hạng mục có thể tối ưu
)

// Category nhóm các phát hiện theo chiều.
type Category string

const (
	CatFlow     Category = "flow"     // Quy trình kẹt, trạng thái bất thường, vấn đề khôi phục
	CatQuality  Category = "quality"  // Điểm rà soát, mức đạt hợp đồng, tính nhất quán
	CatPlanning Category = "planning" // Lỗ hổng dàn ý, trôi lệch phục bút, la bàn lỗi thời
	CatContext  Category = "context"  // Bất thường nhân vật/dòng thời gian/quan hệ
)

// Confidence biểu thị độ tin cậy của phán định quy tắc.
type Confidence string

const (
	ConfHigh   Confidence = "high"   // Tính xác định mạnh, đáng tin
	ConfMedium Confidence = "medium" // Phán đoán heuristic, có thể phán nhầm
	ConfLow    Confidence = "low"    // Tín hiệu thô, chỉ để tham khảo
)

// AutoLevel biểu thị liệu Finding có thể chuyển thành action tự động hoá hay không.
type AutoLevel string

const (
	AutoNone    AutoLevel = "none"    // Chỉ báo cáo, không tự động
	AutoSuggest AutoLevel = "suggest" // Đề xuất action nhưng cần con người xác nhận
	AutoSafe    AutoLevel = "safe"    // Có thể tự động thực thi an toàn
)

// Finding là một kết quả chẩn đoán có thể hành động.
type Finding struct {
	Rule       string     // Tên quy tắc, ví dụ "StaleForeshadow"
	Category   Category   // Phân loại
	Severity   Severity   // Mức nghiêm trọng
	Confidence Confidence // Độ tin cậy phán định
	AutoLevel  AutoLevel  // Mức tự động hoá
	Target     string     // Mặt tác động đề xuất, ví dụ "runtime.flow"
	Title      string     // Tóm tắt một dòng
	Evidence   string     // Bằng chứng dữ liệu cụ thể
	Suggestion string     // Đề xuất cải thiện (trỏ tới prompt/flow/config)
}

// RuleFunc là chữ ký thống nhất của một quy tắc chẩn đoán.
type RuleFunc func(snap *Snapshot) []Finding

// ActionKind biểu thị loại action chẩn đoán.
type ActionKind string

const (
	ActionEmitNotice      ActionKind = "emit_notice"       // Phát thông báo hệ thống
	ActionEnqueueFollowUp ActionKind = "enqueue_follow_up" // Tiêm follow-up cho coordinator
)

// Action là action có thể thực thi do Planner sinh ra dựa trên Finding độ tin cậy cao.
type Action struct {
	SourceRule  string     // Tên quy tắc nguồn
	Kind        ActionKind // Loại action
	Severity    Severity   // Kế thừa từ Finding
	Summary     string     // Mô tả ngắn
	Message     string     // Thông điệp truyền cho luồng điều khiển
	Fingerprint string     // Vân tay ổn định của Finding nguồn, dùng để khử trùng lặp lúc chạy
}

// Stats là các chỉ số tổng quan trình bày song song với phát hiện.
type Stats struct {
	CompletedChapters int
	TotalChapters     int
	TotalWords        int
	AvgWordsPerCh     int
	Phase             string
	Flow              string
	PlanningTier      string
	ReviewCount       int
	RewriteCount      int
	AvgReviewScore    float64
	ForeshadowOpen    int
	ForeshadowStale   int
}

// Report là output đầy đủ của một lần chạy chẩn đoán.
type Report struct {
	Stats    Stats
	Findings []Finding
	Actions  []Action
}
