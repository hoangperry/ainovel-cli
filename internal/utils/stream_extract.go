package utils

import "strings"

// JSONFieldExtractor trích xuất giá trị chuỗi của trường chỉ định từ các mảnh JSON dạng stream.
//
// Khi LLM sinh tool call theo kiểu stream, tham số đến theo từng mảnh (OpenAI/Anthropic)
// hoặc đến một lần (Gemini). Extractor này dùng máy trạng thái quét theo từng ký tự,
// phát hiện key mục tiêu thì trích xuất giá trị chuỗi của nó, xử lý escape JSON.
type JSONFieldExtractor struct {
	key      string // mục tiêu khớp, ví dụ `"content"` hoặc `"task"`
	state    extractState
	matchPos int
	escape   bool
	buf      strings.Builder
}

type extractState int

const (
	stateScan    extractState = iota // quét, tìm key mục tiêu
	stateColon                       // đã khớp key, chờ dấu hai chấm và dấu ngoặc kép mở đầu
	stateExtract                     // đang trích xuất giá trị chuỗi
)

func NewFieldExtractor(fieldName string) *JSONFieldExtractor {
	return &JSONFieldExtractor{key: `"` + fieldName + `"`}
}

// Feed xử lý một đoạn delta, trả về văn bản trích xuất được (có thể rỗng).
func (e *JSONFieldExtractor) Feed(delta string) string {
	e.buf.Reset()
	for _, r := range delta {
		switch e.state {
		case stateScan:
			e.feedScan(r)
		case stateColon:
			e.feedColon(r)
		case stateExtract:
			e.feedExtract(r)
		}
	}
	return e.buf.String()
}

func (e *JSONFieldExtractor) feedScan(r rune) {
	if e.matchPos < len(e.key) && byte(r) == e.key[e.matchPos] {
		e.matchPos++
		if e.matchPos == len(e.key) {
			e.state = stateColon
			e.matchPos = 0
		}
		return
	}
	e.matchPos = 0
	if byte(r) == e.key[0] {
		e.matchPos = 1
	}
}

func (e *JSONFieldExtractor) feedColon(r rune) {
	switch r {
	case ':', ' ', '\t':
		// bỏ qua
	case '"':
		e.state = stateExtract
		e.escape = false
	default:
		e.state = stateScan
		e.matchPos = 0
		if byte(r) == e.key[0] {
			e.matchPos = 1
		}
	}
}

func (e *JSONFieldExtractor) feedExtract(r rune) {
	if e.escape {
		e.escape = false
		switch r {
		case 'n':
			e.buf.WriteByte('\n')
		case 't':
			e.buf.WriteByte('\t')
		case 'r':
			e.buf.WriteByte('\r')
		case '"', '\\', '/':
			e.buf.WriteRune(r)
		default:
			e.buf.WriteByte('\\')
			e.buf.WriteRune(r)
		}
		return
	}
	switch r {
	case '\\':
		e.escape = true
	case '"':
		e.state = stateScan
		e.matchPos = 0
	default:
		e.buf.WriteRune(r)
	}
}

// Reset đặt lại trạng thái (gọi khi sang lượt message LLM mới).
func (e *JSONFieldExtractor) Reset() {
	e.state = stateScan
	e.matchPos = 0
	e.escape = false
}

// ThinkingSep là marker phân tách giữa văn bản thinking và nội dung chính.
// StreamFilter chèn marker này trước đoạn văn bản thinking, TUI dựa vào đó để chuyển kiểu render.
const ThinkingSep = "\x02"

// StreamFilter phân biệt phần văn bản trả lời và phần JSON tool call của SubAgent.
// Phần văn bản trả lời được đánh dấu là nội dung thinking (tiền tố ThinkingSep); JSON tool call chỉ trích xuất trường chỉ định.
//
// Căn cứ phán đoán: gặp { thì vào chế độ JSON (theo dõi độ sâu dấu ngoặc nhọn),
// độ sâu về 0 thì quay lại chế độ văn bản.
type StreamFilter struct {
	fieldExt   *JSONFieldExtractor
	mode       filterMode
	braceDepth int
	inString   bool // đang trong chuỗi JSON (không đếm dấu ngoặc nhọn)
	escJSON    bool // escape bên trong chuỗi JSON
	thinking   bool // hiện đang ở đoạn văn bản thinking
	buf        strings.Builder
}

type filterMode int

const (
	filterText filterMode = iota // văn bản trả lời, truyền thẳng qua
	filterJSON                   // JSON tool call, trích xuất trường mục tiêu
)

func NewStreamFilter(fieldName string) *StreamFilter {
	return &StreamFilter{fieldExt: NewFieldExtractor(fieldName)}
}

// Feed xử lý một đoạn delta, trả về văn bản có thể hiển thị.
// Văn bản trả lời xuất thẳng; giá trị trường mục tiêu trong JSON được trích xuất rồi xuất; phần cấu trúc JSON còn lại bị bỏ.
func (f *StreamFilter) Feed(delta string) string {
	f.buf.Reset()
	for _, r := range delta {
		switch f.mode {
		case filterText:
			if r == '{' {
				f.thinking = false
				f.mode = filterJSON
				f.braceDepth = 1
				f.inString = false
				f.escJSON = false
				f.fieldExt.Reset()
				f.feedExtractor(r)
			} else {
				if !f.thinking {
					f.thinking = true
					f.buf.WriteString(ThinkingSep)
				}
				f.buf.WriteRune(r)
			}
		case filterJSON:
			f.feedExtractor(r)
			f.trackBraces(r)
		}
	}
	return f.buf.String()
}

// feedExtractor đưa một ký tự đơn vào fieldExt, kết quả trích xuất ghi vào buf.
func (f *StreamFilter) feedExtractor(r rune) {
	if text := f.fieldExt.Feed(string(r)); text != "" {
		f.buf.WriteString(text)
	}
}

// trackBraces theo dõi độ sâu dấu ngoặc nhọn JSON, khi độ sâu về 0 thì chuyển lại chế độ văn bản.
func (f *StreamFilter) trackBraces(r rune) {
	if f.escJSON {
		f.escJSON = false
		return
	}
	if f.inString {
		switch r {
		case '\\':
			f.escJSON = true
		case '"':
			f.inString = false
		}
		return
	}
	switch r {
	case '"':
		f.inString = true
	case '{':
		f.braceDepth++
	case '}':
		f.braceDepth--
		if f.braceDepth <= 0 {
			f.mode = filterText
		}
	}
}

// Reset đặt lại trạng thái.
func (f *StreamFilter) Reset() {
	f.mode = filterText
	f.braceDepth = 0
	f.inString = false
	f.escJSON = false
	f.thinking = false
	f.fieldExt.Reset()
}
