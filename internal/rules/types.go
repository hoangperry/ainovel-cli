// Package rules hiện thực lớp đầu vào lưu trữ bền bỉ cho preference của người dùng (Policy).
//
// Rule là loại fact thứ tư, ngang hàng với Progress / Checkpoint / Artifact, nhưng bản chất ngược lại:
// ba loại trước là output của hệ thống, Rule là đầu vào bền bỉ của ý định người dùng.
//
// Ràng buộc thiết kế (không thỏa hiệp):
//   - tool chỉ trả fact, không trả chỉ thị (Violation là fact, editor quyết định có kích hoạt rewrite hay không)
//   - không thêm đường verdict mới (tái dùng PendingRewrites)
//   - không thêm trường mức độ nghiêm trọng (severity ánh xạ cố định theo loại rule, editor tự phán định theo ngữ nghĩa)
//   - không nuốt im lặng conflict (mọi bất thường vào Bundle.Conflicts, để LLM và /diag thấy được)
//   - không đụng Flow Router (rule không tham gia routing)
package rules

// SourceKind đánh dấu nguồn của rule, dùng để sắp xếp ưu tiên gần nhất khi merge.
// Giá trị càng lớn càng gần: Project > Global > Default.
//
// Từ Phase 1.1 chỉ hỗ trợ ba lớp. Lớp Genre / Learned chưa mở khoét trước khi thư viện thể loại thực sự / save_rule đi vào hoạt động——
// thực sự cần mở rộng thì thêm hằng và bổ sung loader sau, không để khung rỗng.
type SourceKind int

const (
	// SourceDefault — rule mặc định tích hợp sẵn của project (assets/rules/default.md), ưu tiên thấp nhất.
	SourceDefault SourceKind = iota
	// SourceGlobal — preference toàn cục của người dùng (mọi .md dưới thư mục ~/.ainovel/rules/, merge theo thứ tự từ điển tên file), tái dùng xuyên các quyển.
	SourceGlobal
	// SourceProject — rule của quyển này (mọi .md dưới thư mục ./.ainovel/rules/, merge theo thứ tự từ điển tên file), ưu tiên cao nhất.
	SourceProject
)

// String trả về tên dễ đọc của nguồn, dùng cho tiêu đề nguồn khi ghép markdown và conflicts.detail.
func (k SourceKind) String() string {
	switch k {
	case SourceDefault:
		return "default"
	case SourceGlobal:
		return "global"
	case SourceProject:
		return "project"
	default:
		return "unknown"
	}
}

// WordRange biểu diễn khoảng số chữ cho phép của chương; nil nghĩa là chưa khai báo.
type WordRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Structured chứa các trường có cấu trúc của front matter.
//
// Khi parse một file đơn lẻ, Parsed.Structured chỉ điền các trường file đó khai báo, phần còn lại giữ giá trị zero.
// Sau khi merge, Bundle.Structured là kết quả tổng thể sau khi ưu tiên gần nhất giữa các nguồn.
type Structured struct {
	Genre            string         `json:"genre,omitempty"`
	ChapterWords     *WordRange     `json:"chapter_words,omitempty"`
	ForbiddenChars   []string       `json:"forbidden_chars,omitempty"`
	ForbiddenPhrases []string       `json:"forbidden_phrases,omitempty"`
	FatigueWords     map[string]int `json:"fatigue_words,omitempty"`
}

// IsEmpty dùng để xác định có hoàn toàn không có rule có cấu trúc nào hay không; checker có thể dựa vào đó để bỏ qua.
func (s Structured) IsEmpty() bool {
	return s.Genre == "" &&
		s.ChapterWords == nil &&
		len(s.ForbiddenChars) == 0 &&
		len(s.ForbiddenPhrases) == 0 &&
		len(s.FatigueWords) == 0
}

// ConflictKind đánh dấu loại conflict hoặc bất thường, để LLM và panel chẩn đoán phân loại xử lý.
type ConflictKind string

const (
	// ConflictParseError — parse toàn bộ front matter thất bại; phần nội dung vẫn được inject như preference.
	ConflictParseError ConflictKind = "parse_error"
	// ConflictUnknownField — người dùng viết trường mà Phase 1 chưa hỗ trợ (forward-compatible).
	ConflictUnknownField ConflictKind = "unknown_field"
	// ConflictTypeError — trường sai kiểu (ví dụ forbidden_chars viết thành chuỗi); trường đó bị loại bỏ.
	ConflictTypeError ConflictKind = "type_error"
	// ConflictFieldConflict — cùng một trường có cấu trúc từ nhiều nguồn có giá trị không nhất quán; ưu tiên gần nhất có hiệu lực.
	ConflictFieldConflict ConflictKind = "field_conflict"
	// ConflictInvalidValue — giá trị trường sai định dạng (ví dụ chapter_words: "abc"); trường đó bị loại bỏ.
	ConflictInvalidValue ConflictKind = "invalid_value"
)

// Conflict là một bản ghi conflict hoặc bất thường.
//
// Không bao giờ chặn việc load——mọi bất thường đều phơi ra ở đây cho LLM và /diag, không xử lý im lặng.
type Conflict struct {
	Source string       `json:"source"`          // đường dẫn file (tuyệt đối hoặc tương đối, ghi theo nguồn)
	Kind   ConflictKind `json:"kind"`            // loại conflict
	Field  string       `json:"field,omitempty"` // tên trường bị ảnh hưởng (ví dụ forbidden_chars); để trống khi parse_error
	Detail string       `json:"detail"`          // chi tiết dễ đọc (gồm danh sách nguồn / thông báo lỗi)
}

// Parsed là kết quả sau khi parse một bản rules.md đơn lẻ.
type Parsed struct {
	Source     string     // đường dẫn file
	Kind       SourceKind // loại nguồn, dùng cho ưu tiên merge
	Structured Structured // các trường front matter file này khai báo
	Preference string     // phần nội dung Markdown của file (phần ngoài front matter)
	Conflicts  []Conflict // các conflict phát sinh khi parse file này (trường lạ / sai kiểu)
}

// Bundle là hình thái cuối cùng sau khi merge, được inject vào working_memory.user_rules.
//
// Ánh xạ trường sang output JSON:
//
//	{
//	  "structured": {...},
//	  "preferences": "...markdown đã merge...",
//	  "sources": ["..."],
//	  "conflicts": [...]
//	}
type Bundle struct {
	Structured  Structured `json:"structured"`
	Preferences string     `json:"preferences"`
	Sources     []string   `json:"sources"`
	Conflicts   []Conflict `json:"conflicts"`
}

// IsEmpty cho biết Bundle hoàn toàn không có nội dung (trường có cấu trúc rỗng + nội dung preference rỗng).
// Khi inject user_rules vẫn nên giữ Bundle rỗng, tránh để LLM phải xử lý nil.
func (b Bundle) IsEmpty() bool {
	return b.Structured.IsEmpty() && b.Preferences == ""
}

// Severity đánh dấu mức độ nghiêm trọng của Violation.
// Ánh xạ cố định (người dùng không cấu hình được):
//
//	forbidden_chars xuất hiện         -> Error
//	forbidden_phrases xuất hiện       -> Error
//	fatigue_words vượt ngưỡng         -> Warning
//	chapter_words lệch < 20%          -> Warning
//	chapter_words lệch >= 20%         -> Error
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// ChapterWordsDeviationThreshold định nghĩa ngưỡng tới hạn (20%) mà độ lệch chapter_words leo thang thành error.
const ChapterWordsDeviationThreshold = 0.20

// Violation là output của checker: phát biểu sự thật rằng chương này vi phạm một rule cơ học nào đó.
//
// Lưu ý: commit_chapter truyền thẳng violations vào JSON trả về, không chặn commit;
// editor khi rà soát ánh xạ các fact này sang bảy chiều hiện có (aesthetic/pacing/character/consistency),
// để LLM tự quyết có leo thang verdict kích hoạt polish/rewrite hay không.
type Violation struct {
	Rule      string   `json:"rule"`                // forbidden_chars / forbidden_phrases / fatigue_words / chapter_words
	Target    string   `json:"target,omitempty"`    // đối tượng vi phạm cụ thể (từ/ký tự nào); chapter_words để trống
	Limit     any      `json:"limit,omitempty"`     // ngưỡng; fatigue_words=int / chapter_words="3000-6000" / forbidden_*=trống
	Actual    any      `json:"actual"`              // giá trị thực tế; fatigue_words/forbidden_*=số lần xuất hiện / chapter_words=số chữ chương này
	Deviation float64  `json:"deviation,omitempty"` // tỷ lệ lệch chapter_words (0~1), các rule khác để trống
	Severity  Severity `json:"severity"`            // error / warning
}
