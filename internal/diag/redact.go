package diag

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SkelEvent là khung hành vi của một message session sau khi khử nhạy cảm: giữ tín hiệu cấu trúc (role / tool / lỗi /
// vân tay trùng lặp), mọi văn bản tự do (chính văn, prompt, tư duy) đều bị che. Đây là một lớp chiếu chặt hơn
// store.compactMessage — cái sau nén theo dung lượng (>4KB), ở đây không nhìn dung lượng,
// bất kỳ văn bản nào cũng không ra gói.
type SkelEvent struct {
	Agent    string     // Session nguồn: coordinator / writer-ch07 …
	Role     string     // assistant / tool / user
	Tools    []SkelTool // Các lời gọi tool trong message này
	ErrClass string     // role=tool và is_error: dòng đầu của lỗi (chuỗi lỗi framework, không chứa chính văn)
	TextSha  string     // Hash ngắn của chính văn đã che; cùng sha = sinh lặp cùng một đoạn (tín hiệu vòng lặp)
	Redacted int        // Số khối văn bản/tư duy bị che trong dòng này (dùng cho tự kiểm khử nhạy cảm)
}

// SkelTool là phép chiếu khử nhạy cảm của một lời gọi tool.
type SkelTool struct {
	Name     string            // Tên tool (tín hiệu cấu trúc, không chứa chính văn)
	Args     map[string]string // key → giá trị scalar gốc / chuỗi ngắn có ngoặc kép / "<redacted len sha>"
	Invalid  bool              // ArgsInvalid: tham số model gửi tới không phân giải được (tín hiệu #34)
	ParseErr string            // ArgsParseError: lý do phân giải thất bại
}

// redactMessage chiếu một agentcore.Message thành khung hành vi.
func redactMessage(agent string, m agentcore.Message) SkelEvent {
	ev := SkelEvent{Agent: agent, Role: string(m.Role)}
	isErr, _ := m.Metadata["is_error"].(bool)

	var text strings.Builder
	for _, b := range m.Content {
		switch b.Type {
		case agentcore.ContentText:
			// Kết quả lỗi của tool giữ dòng đầu: đây là chuỗi lỗi của chính ta (như InputValidationError),
			// không chứa chính văn, và là mấu chốt để định vị vòng lặp. Văn bản còn lại đều vào bể che.
			if m.Role == agentcore.RoleTool && isErr && ev.ErrClass == "" {
				ev.ErrClass = firstLine(b.Text, 160)
				continue
			}
			if strings.TrimSpace(b.Text) != "" {
				text.WriteString(b.Text)
				ev.Redacted++
			}
		case agentcore.ContentThinking:
			if strings.TrimSpace(b.Thinking) != "" {
				text.WriteString(b.Thinking)
				ev.Redacted++
			}
		case agentcore.ContentToolCall:
			if b.ToolCall != nil {
				ev.Tools = append(ev.Tools, redactToolCall(b.ToolCall))
			}
		}
	}
	if t := text.String(); t != "" {
		ev.TextSha = shortHash(t)
	}
	return ev
}

// redactToolCall chiếu một lời gọi tool: tên tool + tham số (giá trị đã khử nhạy cảm) + cờ lỗi phân giải.
func redactToolCall(tc *agentcore.ToolCall) SkelTool {
	return SkelTool{
		Name:     tc.Name,
		Args:     redactArgs(tc.Args),
		Invalid:  tc.ArgsInvalid,
		ParseErr: tc.ArgsParseError,
	}
}

// redactArgs chiếu object tham số tool thành key → giá trị đã khử nhạy cảm. Tham số không phải object trả về nil
// (ArgsInvalid/ParseErr đã được ghi riêng trong SkelTool).
func redactArgs(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = projectValue(v)
	}
	return out
}

// projectValue chiếu một giá trị tham số theo kiểu JSON:
//   - scalar (số / bool / null): giá trị gốc chính là tín hiệu cấu trúc, giữ lại (chapter: 7)
//   - chuỗi kiểu định danh ngắn: giữ kèm ngoặc kép, lộ kiểu (chapter: "7" ← tín hiệu số bị chuỗi-hoá của #34)
//   - chuỗi chứa tiếng Trung / khoảng trắng / văn bản dài, object, array: che thành <redacted …> (chính văn không ra gói)
//   - đã là placeholder [session_compact: …]: an toàn và có thông tin, giữ nguyên
func projectValue(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return ""
	}
	switch s[0] {
	case '"':
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return redactPlaceholder(s)
		}
		if strings.HasPrefix(str, store.CompactTag) {
			return str
		}
		// Chỉ giữ giá trị ngắn "giống định danh/số/enum" (chapter:"7", type:"premise", agent:"writer");
		// bất kỳ chuỗi nào chứa tiếng Trung, khoảng trắng hoặc ký hiệu khác đều xem là chính văn, đều bị che.
		if utf8.RuneCountInString(str) <= 32 && isStructuralToken(str) {
			return strconv.Quote(str)
		}
		return redactPlaceholder(str)
	case '{':
		return fmt.Sprintf("<redacted object len=%d>", len(raw))
	case '[':
		return fmt.Sprintf("<redacted array len=%d>", len(raw))
	default:
		return s
	}
}

// isStructuralToken phán định chuỗi có "giống định danh" hay không — chỉ chữ cái / số ASCII / `_-.:/`,
// không khoảng trắng, không tiếng Trung. Dùng để phân biệt tín hiệu cấu trúc (giữ) với mảnh chính văn (che).
func isStructuralToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.' || r == ':' || r == '/':
		default:
			return false
		}
	}
	return true
}

func redactPlaceholder(s string) string {
	return fmt.Sprintf("<redacted len=%d sha=%s>", utf8.RuneCountInString(s), shortHash(s))
}

// shortHash lấy hash ngắn của văn bản; chỉ dùng để phán định "cùng một đoạn văn bản có xuất hiện lặp lại hay không", không dùng cho mục đích mã hoá.
func shortHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

// firstLine lấy dòng đầu và cắt cụt theo rune, dùng cho tóm tắt chuỗi lỗi.
func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}
	if utf8.RuneCountInString(s) > max {
		r := []rune(s)
		s = string(r[:max]) + "…"
	}
	return s
}
