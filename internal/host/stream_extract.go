package host

import (
	"strings"
	"unicode/utf8"

	"github.com/voocel/ainovel-cli/internal/contentlang"
)

// toolDisplays cấu hình chiến lược hiển thị của từng tool trên panel stream. Tool không có trong bảng này thì không tham gia
// render stream (observer trực tiếp loại bỏ DeltaToolCall).
//
// Chế độ tổng quát (nakedKey rỗng): tokenizer render args JSON do LLM xuất thành văn bản
// "key: value" dạng thụt lề, object/array lồng nhau thụt theo cấp, string/number/bool xuất theo stream.
// Tách rời hoàn toàn với schema — LLM xuất thêm một field thì panel có thêm một dòng, không cần thay đổi code nào.
//
// Chế độ stream trần (nakedKey khác rỗng): chỉ stream nguyên văn giá trị string của field cấp đỉnh mục tiêu, các field khác
// bỏ qua hết. Dùng cho draft_chapter, để markdown cả chương không bị trang trí thành "content: # …".
// header luôn bắt đầu bằng "✻ ": đây là prefix quy ước cho đường highlight renderAgentBlock của TUI renderStreamContent
// (✻ vàng + label nền cyan gạch chân xanh + đường kẻ dim), giữ nhất quán với fallback
// header (streamHeaderFallback); đổi thành chữ thường sẽ rơi vào đường chính văn bị vẽ bằng
// màu mặc định của terminal, title không còn nổi bật.
var toolDisplays = map[string]toolDisplay{
	"draft_chapter": {nakedKey: "content"},

	"plan_chapter":        {headerZH: "✻ 规划", headerVI: "✻ Lập kế hoạch"},
	"edit_chapter":        {headerZH: "✻ 打磨", headerVI: "✻ Trau chuốt"},
	"commit_chapter":      {headerZH: "✻ 章节提交", headerVI: "✻ Nộp chương"},
	"save_review":         {headerZH: "✻ 审阅", headerVI: "✻ Rà soát"},
	"save_arc_summary":    {headerZH: "✻ 弧摘要", headerVI: "✻ Tóm tắt cung truyện"},
	"save_volume_summary": {headerZH: "✻ 卷摘要", headerVI: "✻ Tóm tắt quyển"},
	"save_foundation":     {headerZH: "✻ 设定", headerVI: "✻ Thiết định"},
	"read_chapter":        {headerZH: "✻ 读章节", headerVI: "✻ Đọc chương"},
	"check_consistency":   {headerZH: "✻ 一致性检查", headerVI: "✻ Kiểm tra nhất quán"},
	"novel_context":       {headerZH: "✻ 查询上下文", headerVI: "✻ Truy vấn ngữ cảnh"},
}

type toolDisplay struct {
	headerZH string
	headerVI string
	nakedKey string
}

// header trả về tiêu đề panel theo locale nội dung hiện hành. Resolve tại thời điểm gọi
// (không phải lúc khởi tạo map cấp gói) để tôn trọng contentlang.Set chạy trong main().
func (d toolDisplay) header() string {
	if d.headerZH == "" && d.headerVI == "" {
		return ""
	}
	return contentlang.Pick(d.headerZH, d.headerVI)
}

// jsonFieldExtractor là tokenizer JSON theo stream. Lái máy trạng thái theo từng byte, biến args tool của LLM
// thành văn bản dễ đọc. Mỗi instance chỉ phục vụ một lần gọi tool, sau khi container cấp đỉnh đóng thì Done()=true.
type jsonFieldExtractor struct {
	cfg toolDisplay

	state pState
	stack []byte // stack container: 'O' obj / 'A' arr

	keyBuf strings.Builder

	escape bool
	uHex   []byte

	started bool // đã emit ký tự nào hay chưa (dùng cho việc xuống dòng giữa header và key đầu tiên)

	done bool
}

type pState int

const (
	psRoot         pState = iota
	psBeforeKey           // trong obj: chờ key kế tiếp hoặc }
	psInKey               // trong obj: parse key
	psAfterKey            // trong obj: chờ :
	psBeforeValue         // chờ ký tự khởi đầu của value
	psStringStream        // giá trị string, emit theo stream ký tự đã cook
	psStringSkip          // giá trị string, bỏ qua (field không phải mục tiêu ở chế độ stream trần)
	psNumberStream        // số, emit theo stream
	psNumberSkip          // số, bỏ qua
	psPrimStream          // true/false/null, emit theo stream
	psPrimSkip            // true/false/null, bỏ qua
	psDone                // container cấp đỉnh đã đóng
)

func newToolExtractor(tool string) *jsonFieldExtractor {
	cfg, ok := toolDisplays[tool]
	if !ok {
		return nil
	}
	return &jsonFieldExtractor{cfg: cfg}
}

func (e *jsonFieldExtractor) Done() bool { return e.done }

func (e *jsonFieldExtractor) Feed(chunk string) string {
	if e.done || chunk == "" {
		return ""
	}
	var out strings.Builder
	for i := 0; i < len(chunk); i++ {
		e.step(chunk[i], &out)
		if e.done {
			break
		}
	}
	return out.String()
}

// ── stack container / thụt lề ──

func (e *jsonFieldExtractor) push(kind byte) {
	e.stack = append(e.stack, kind)
}

func (e *jsonFieldExtractor) pop() {
	if len(e.stack) == 0 {
		return
	}
	e.stack = e.stack[:len(e.stack)-1]
}

func (e *jsonFieldExtractor) parent() byte {
	if len(e.stack) == 0 {
		return 0
	}
	return e.stack[len(e.stack)-1]
}

// writeIndent ghi thụt lề hiện tại. Độ sâu = số cấp lồng = len(stack)-1 (bên trong container root không thụt).
func (e *jsonFieldExtractor) writeIndent(out *strings.Builder) {
	depth := len(e.stack) - 1
	for range depth {
		out.WriteString("  ")
	}
}

// ── máy trạng thái ──

func (e *jsonFieldExtractor) step(c byte, out *strings.Builder) {
	switch e.state {
	case psRoot:
		switch c {
		case '{':
			e.push('O')
			e.state = psBeforeKey
		case '[':
			// Thực tế không xảy ra (tool args luôn là obj); dung thứ: coi như root arr
			e.push('A')
			e.state = psBeforeValue
		}
	case psBeforeKey:
		switch c {
		case '"':
			e.keyBuf.Reset()
			e.escape = false
			e.state = psInKey
		case '}':
			e.closeContainer(out)
		case ' ', '\t', '\n', '\r', ',':
		}
	case psInKey:
		if e.escape {
			e.keyBuf.WriteByte(c)
			e.escape = false
			return
		}
		if c == '\\' {
			e.escape = true
			return
		}
		if c == '"' {
			e.emitKeyLine(out, e.keyBuf.String())
			e.state = psAfterKey
			return
		}
		e.keyBuf.WriteByte(c)
	case psAfterKey:
		if c == ':' {
			e.state = psBeforeValue
		}
	case psBeforeValue:
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' {
			return
		}
		switch c {
		case '"':
			e.beginString(out)
		case '{':
			e.beginNested('O', out)
		case '[':
			e.beginNested('A', out)
		case ']', '}':
			e.closeContainer(out)
		case 't', 'f', 'n':
			e.beginPrim(c, out)
		default:
			if c == '-' || (c >= '0' && c <= '9') {
				e.beginNumber(c, out)
			}
		}
	case psStringStream:
		e.handleStringByte(c, out, false)
	case psStringSkip:
		e.handleStringByte(c, out, true)
	case psNumberStream:
		if isNumberByte(c) {
			out.WriteByte(c)
			return
		}
		e.afterValueChar(c, out)
	case psNumberSkip:
		if isNumberByte(c) {
			return
		}
		e.afterValueChar(c, out)
	case psPrimStream:
		if c >= 'a' && c <= 'z' {
			out.WriteByte(c)
			return
		}
		e.afterValueChar(c, out)
	case psPrimSkip:
		if c >= 'a' && c <= 'z' {
			return
		}
		e.afterValueChar(c, out)
	case psDone:
	}
}

// ── render dòng ──

// emitKeyLine được gọi khi parse xong key trong obj, ghi ra prefix "<lf><indent>key:".
// Ở chế độ stream trần không ghi prefix key (key được lưu trong keyBuf để beginString xét).
func (e *jsonFieldExtractor) emitKeyLine(out *strings.Builder, key string) {
	if e.cfg.nakedKey != "" {
		return
	}
	if !e.started {
		if h := e.cfg.header(); h != "" {
			out.WriteString(h)
			out.WriteByte('\n')
		}
		e.started = true
	} else {
		out.WriteByte('\n')
	}
	e.writeIndent(out)
	out.WriteString(key)
	out.WriteByte(':')
}

// emitArrayItem được gọi tại đầu mỗi phần tử trong arr, ghi ra "<lf><indent>-". Phần tử primitive
// thì theo sau là dấu cách rồi emit giá trị; phần tử struct do phần lồng kế tiếp tự nhiên xuống dòng xử lý.
func (e *jsonFieldExtractor) emitArrayItem(out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		return
	}
	if !e.started {
		if h := e.cfg.header(); h != "" {
			out.WriteString(h)
			out.WriteByte('\n')
		}
		e.started = true
	} else {
		out.WriteByte('\n')
	}
	e.writeIndent(out)
	out.WriteByte('-')
}

// ── khởi đầu value ──

func (e *jsonFieldExtractor) beginString(out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		// Stream trần: chỉ xuất giá trị string của key mục tiêu trong obj cấp đỉnh
		if e.cfg.nakedKey == e.keyBuf.String() && len(e.stack) == 1 && e.stack[0] == 'O' {
			e.state = psStringStream
		} else {
			e.state = psStringSkip
		}
		e.escape = false
		e.uHex = nil
		return
	}
	// Tổng quát: field obj theo sau "key: " (đã emit "key:", bù thêm dấu cách); phần tử arr theo sau "- "
	if e.parent() == 'A' {
		e.emitArrayItem(out)
		out.WriteByte(' ')
	} else {
		out.WriteByte(' ')
	}
	e.state = psStringStream
	e.escape = false
	e.uHex = nil
}

func (e *jsonFieldExtractor) beginNumber(first byte, out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		e.state = psNumberSkip
		return
	}
	if e.parent() == 'A' {
		e.emitArrayItem(out)
		out.WriteByte(' ')
	} else {
		out.WriteByte(' ')
	}
	out.WriteByte(first)
	e.state = psNumberStream
}

func (e *jsonFieldExtractor) beginPrim(first byte, out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		e.state = psPrimSkip
		return
	}
	if e.parent() == 'A' {
		e.emitArrayItem(out)
		out.WriteByte(' ')
	} else {
		out.WriteByte(' ')
	}
	out.WriteByte(first)
	e.state = psPrimStream
}

func (e *jsonFieldExtractor) beginNested(kind byte, out *strings.Builder) {
	if e.cfg.nakedKey != "" {
		// Chế độ stream trần không mở phần lồng; dùng độ sâu stack để theo dõi tới khi khớp } / ]
		e.push(kind)
		if kind == 'O' {
			e.state = psBeforeKey
		} else {
			e.state = psBeforeValue
		}
		return
	}
	// Chế độ tổng quát: khi phần tử arr là cấu trúc lồng, trước hết emit "<indent>-" trên một dòng riêng
	// (sau dấu ":" của obj key không có dấu cách, để sub-key lồng tự nhiên xuống dòng kế tiếp)
	if e.parent() == 'A' {
		e.emitArrayItem(out)
	}
	e.push(kind)
	if kind == 'O' {
		e.state = psBeforeKey
	} else {
		e.state = psBeforeValue
	}
}

// closeContainer xử lý } hoặc ].
func (e *jsonFieldExtractor) closeContainer(out *strings.Builder) {
	e.pop()
	if len(e.stack) == 0 {
		// Lưới đỡ cho args rỗng (như novel_context không truyền tham số): emitKeyLine không có cơ hội xuất header,
		// ở đây bù một lần, tránh rơi vào "không tiêu đề cũng không nội dung".
		if h := e.cfg.header(); !e.started && e.cfg.nakedKey == "" && h != "" {
			out.WriteString(h)
			out.WriteByte('\n')
			e.started = true
		}
		// Xuống dòng kết thúc để giữa panel và đoạn output kế tiếp có biên rõ ràng
		if e.started {
			out.WriteByte('\n')
		}
		e.state = psDone
		e.done = true
		return
	}
	if e.parent() == 'O' {
		e.state = psBeforeKey
	} else {
		e.state = psBeforeValue
	}
}

// ── string theo stream ──

func (e *jsonFieldExtractor) handleStringByte(c byte, out *strings.Builder, skipping bool) {
	if e.uHex != nil {
		e.uHex = append(e.uHex, c)
		if len(e.uHex) == 4 {
			if r, ok := parseHex4(e.uHex); ok && !skipping {
				var buf [4]byte
				n := utf8.EncodeRune(buf[:], r)
				out.Write(buf[:n])
			}
			e.uHex = nil
		}
		return
	}
	if e.escape {
		e.escape = false
		if !skipping {
			writeEscapedByte(out, c)
		}
		if c == 'u' {
			e.uHex = make([]byte, 0, 4)
		}
		return
	}
	if c == '\\' {
		e.escape = true
		return
	}
	if c == '"' {
		e.afterValueDone()
		return
	}
	if !skipping {
		out.WriteByte(c)
	}
}

func writeEscapedByte(out *strings.Builder, c byte) {
	switch c {
	case 'n':
		out.WriteByte('\n')
	case 't':
		out.WriteByte('\t')
	case 'r':
		out.WriteByte('\r')
	case '"':
		out.WriteByte('"')
	case '\\':
		out.WriteByte('\\')
	case '/':
		out.WriteByte('/')
	case 'b', 'f':
		// Backspace / form feed: bỏ qua
	case 'u':
		// Bên gọi sẽ lập buffer uHex; ở đây không xuất
	default:
		out.WriteByte('\\')
		out.WriteByte(c)
	}
}

// ── kết thúc ──

// afterValueDone chuyển sang trạng thái kế tiếp sau khi string đóng (đọc tới `"` ở cuối).
func (e *jsonFieldExtractor) afterValueDone() {
	e.escape = false
	e.uHex = nil
	if len(e.stack) == 0 {
		e.state = psDone
		e.done = true
		return
	}
	if e.parent() == 'O' {
		e.state = psBeforeKey
	} else {
		e.state = psBeforeValue
	}
}

// afterValueChar khi "ký tự kết thúc" của number / primitive đã được đọc thì dựa theo ký tự quyết định trạng thái kế tiếp.
// Ký tự này có thể là , / } / ] / khoảng trắng, do hàm này chuyển tiếp phân phối.
func (e *jsonFieldExtractor) afterValueChar(c byte, out *strings.Builder) {
	switch c {
	case '}', ']':
		e.closeContainer(out)
	case ',', ' ', '\t', '\n', '\r':
		if len(e.stack) == 0 {
			e.state = psDone
			e.done = true
			return
		}
		if e.parent() == 'O' {
			e.state = psBeforeKey
		} else {
			e.state = psBeforeValue
		}
	}
}

// ── tiện ích ──

func isNumberByte(c byte) bool {
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		'-', '+', '.', 'e', 'E':
		return true
	}
	return false
}

func parseHex4(b []byte) (rune, bool) {
	var r rune
	for _, d := range b {
		var v rune
		switch {
		case d >= '0' && d <= '9':
			v = rune(d - '0')
		case d >= 'a' && d <= 'f':
			v = rune(d-'a') + 10
		case d >= 'A' && d <= 'F':
			v = rune(d-'A') + 10
		default:
			return 0, false
		}
		r = r*16 + v
	}
	return r, true
}
