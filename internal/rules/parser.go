package rules

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/voocel/ainovel-cli/internal/i18n"
	"gopkg.in/yaml.v3"
)

// Tập các trường front matter đã biết, dùng để nhận diện trường lạ và ghi vào conflicts.
var knownFrontMatterFields = map[string]struct{}{
	"genre":             {},
	"chapter_words":     {},
	"forbidden_chars":   {},
	"forbidden_phrases": {},
	"fatigue_words":     {},
}

// Parse phân tích nội dung một bản rules.md (front matter + Markdown).
//
// Chiến lược dung lỗi:
//   - parse toàn bộ front matter thất bại: không chặn, nội dung vẫn làm preference, conflicts ghi parse_error
//   - trường lạ: loại bỏ, conflicts ghi unknown_field
//   - trường sai kiểu: loại bỏ trường đó, conflicts ghi type_error
//   - giá trị trường không hợp lệ (ví dụ chapter_words không parse được thành khoảng): loại bỏ, conflicts ghi invalid_value
//
// source là đường dẫn file, chỉ dùng cho conflicts.source; kind quyết định ưu tiên.
func Parse(source string, kind SourceKind, content []byte) Parsed {
	parsed := Parsed{Source: source, Kind: kind}

	fmText, bodyText := splitFrontMatter(content)
	parsed.Preference = strings.TrimSpace(bodyText)

	if strings.TrimSpace(fmText) == "" {
		return parsed
	}

	// Trước hết unmarshal vào map[string]any, rồi parse từng trường theo kiểu chặt chẽ.
	// Cách này phân biệt được "trường không tồn tại" và "trường sai kiểu", đồng thời nhận diện được trường lạ.
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(fmText), &raw); err != nil {
		parsed.Conflicts = append(parsed.Conflicts, Conflict{
			Source: source,
			Kind:   ConflictParseError,
			Detail: i18n.Tf("rules.parse.yaml_failed", err),
		})
		return parsed
	}

	for key, val := range raw {
		if _, ok := knownFrontMatterFields[key]; !ok {
			parsed.Conflicts = append(parsed.Conflicts, Conflict{
				Source: source,
				Kind:   ConflictUnknownField,
				Field:  key,
				Detail: i18n.Tf("rules.parse.unknown_field", key),
			})
			continue
		}
		applyField(&parsed, key, val)
	}

	return parsed
}

// splitFrontMatter tách front matter được bọc bởi `---` khỏi phần nội dung còn lại.
//
// Quy ước:
//   - file phải bắt đầu bằng `---` (cho phép BOM / dòng trống) mới coi là có front matter
//   - phần sau `---` thứ hai là nội dung
//   - không có front matter: toàn văn làm nội dung
//   - chỉ có `---` mở đầu mà không có `---` kết thúc: coi như không có front matter (tránh nuốt cả bài)
func splitFrontMatter(content []byte) (fm, body string) {
	text := string(bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})) // bỏ UTF-8 BOM
	lines := strings.Split(text, "\n")

	// tìm dòng không rỗng đầu tiên; không phải `---` thì toàn bộ là nội dung
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.TrimSpace(line) == "---" {
			start = i
		}
		break
	}
	if start < 0 {
		return "", text
	}

	// tìm `---` thứ hai
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		// có `---` mở đầu nhưng không đóng: bảo thủ coi như không có front matter
		return "", text
	}

	fm = strings.Join(lines[start+1:end], "\n")
	body = strings.Join(lines[end+1:], "\n")
	return fm, body
}

// applyField nhét một trường raw vào Parsed.Structured, khi kiểu không khớp thì ghi conflicts.
func applyField(p *Parsed, key string, val any) {
	switch key {
	case "genre":
		s, ok := asString(val)
		if !ok {
			p.Conflicts = append(p.Conflicts, typeErr(p.Source, key, "string", val))
			return
		}
		p.Structured.Genre = strings.TrimSpace(s)

	case "chapter_words":
		rng, ok := parseChapterWords(val)
		if !ok {
			p.Conflicts = append(p.Conflicts, Conflict{
				Source: p.Source,
				Kind:   ConflictInvalidValue,
				Field:  key,
				Detail: i18n.Tf("rules.parse.chapter_words_format", val),
			})
			return
		}
		p.Structured.ChapterWords = rng

	case "forbidden_chars":
		list, ok := asStringList(p, key, val)
		if !ok {
			p.Conflicts = append(p.Conflicts, typeErr(p.Source, key, "[]string", val))
			return
		}
		p.Structured.ForbiddenChars = list

	case "forbidden_phrases":
		list, ok := asStringList(p, key, val)
		if !ok {
			p.Conflicts = append(p.Conflicts, typeErr(p.Source, key, "[]string", val))
			return
		}
		p.Structured.ForbiddenPhrases = list

	case "fatigue_words":
		m, ok := parseFatigueWords(p, val)
		if !ok {
			p.Conflicts = append(p.Conflicts, typeErr(p.Source, key, i18n.T("rules.type.map_or_list"), val))
			return
		}
		p.Structured.FatigueWords = m
	}
}

// parseChapterWords parse khoảng số chữ của chương thành *WordRange, chấp nhận ba cách viết:
//   - chuỗi khoảng "min-max" (ví dụ "3000-6000")
//   - map {min, max}
//   - một số nguyên dương N (số trần 2500 hoặc chuỗi "2500")——hiểu là "mục tiêu N chữ/chương", tự động
//     mở rộng thành khoảng N±20%. Nếu không thì người dùng viết theo trực giác một giá trị đơn sẽ bị loại bỏ im lặng, rơi về mặc định tích hợp (issue #41).
func parseChapterWords(val any) (*WordRange, bool) {
	switch v := val.(type) {
	case string:
		s := strings.TrimSpace(v)
		if !strings.Contains(s, "-") { // cách viết giá trị đơn, như "2500"
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				return wordBandAround(n), true
			}
			return nil, false
		}
		parts := strings.Split(s, "-")
		if len(parts) != 2 {
			return nil, false
		}
		minV, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		maxV, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil || minV < 0 || maxV < 0 || minV > maxV {
			return nil, false
		}
		return &WordRange{Min: minV, Max: maxV}, true
	case map[string]any:
		minV, ok1 := asInt(v["min"])
		maxV, ok2 := asInt(v["max"])
		if !ok1 || !ok2 || minV < 0 || maxV < 0 || minV > maxV {
			return nil, false
		}
		return &WordRange{Min: minV, Max: maxV}, true
	default: // số trần, YAML parse thành int / float64
		if n, ok := asInt(v); ok && n > 0 {
			return wordBandAround(n), true
		}
		return nil, false
	}
}

// wordBandAround mở rộng "mục tiêu N chữ/chương" thành khoảng thoải mái ±20% (như 2500 → 2000-3000),
// để cách viết giá trị đơn tương đương một khoảng hợp lý, thay vì bức tường cứng N-N (khoảng quá chặt sẽ ép ra vòng lặp nén vô tận).
func wordBandAround(n int) *WordRange {
	return &WordRange{Min: n * 4 / 5, Max: n * 6 / 5}
}

// parseFatigueWords chấp nhận đồng thời map[string]int (kèm ngưỡng) và []string (ngưỡng mặc định 1).
//
// Một key sai kiểu hay ngưỡng không hợp lệ đều ghi conflict vào p.Conflicts, tuyệt đối không nuốt im lặng.
// Trả về (map, true) nghĩa là có mục hợp lệ; (nil, false) nghĩa là sai kiểu tổng thể hoặc mọi mục đều không hợp lệ.
func parseFatigueWords(p *Parsed, val any) (map[string]int, bool) {
	switch v := val.(type) {
	case map[string]any:
		out := make(map[string]int, len(v))
		for k, raw := range v {
			trimmed := strings.TrimSpace(k)
			if trimmed == "" {
				p.Conflicts = append(p.Conflicts, Conflict{
					Source: p.Source,
					Kind:   ConflictInvalidValue,
					Field:  "fatigue_words",
					Detail: i18n.T("rules.parse.fatigue_blank_key"),
				})
				continue
			}
			n, ok := asInt(raw)
			if !ok {
				p.Conflicts = append(p.Conflicts, Conflict{
					Source: p.Source,
					Kind:   ConflictTypeError,
					Field:  "fatigue_words." + trimmed,
					Detail: i18n.Tf("rules.parse.fatigue_int_expected", trimmed, raw, raw),
				})
				continue
			}
			if n <= 0 {
				p.Conflicts = append(p.Conflicts, Conflict{
					Source: p.Source,
					Kind:   ConflictInvalidValue,
					Field:  "fatigue_words." + trimmed,
					Detail: i18n.Tf("rules.parse.fatigue_threshold_pos", trimmed, n),
				})
				continue
			}
			out[trimmed] = n
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	case []any:
		out := make(map[string]int, len(v))
		for i, raw := range v {
			s, ok := raw.(string)
			if !ok {
				p.Conflicts = append(p.Conflicts, Conflict{
					Source: p.Source,
					Kind:   ConflictTypeError,
					Field:  fmt.Sprintf("fatigue_words[%d]", i),
					Detail: i18n.Tf("rules.parse.fatigue_list_string", raw, raw),
				})
				continue
			}
			s = strings.TrimSpace(s)
			if s == "" {
				p.Conflicts = append(p.Conflicts, Conflict{
					Source: p.Source,
					Kind:   ConflictInvalidValue,
					Field:  fmt.Sprintf("fatigue_words[%d]", i),
					Detail: i18n.T("rules.parse.fatigue_list_blank"),
				})
				continue
			}
			out[s] = 1
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	default:
		return nil, false
	}
}

// asString / asInt / asStringList là các tiện ích nhỏ chuẩn hóa kiểu sau khi yaml.v3 deserialize.
//
// Chiến lược chặt chẽ (Debug-First): chỉ chấp nhận kiểu mục tiêu, không tự động chuyển các kiểu khác.
// Lỗi kiểu do caller ghi vào conflicts, không sửa im lặng bên trong tiện ích.

// asString chỉ chấp nhận scalar string.
// Lưu ý: trong YAML `genre: 42` (không ngoặc) sẽ bị deserialize thành int, theo hàm này phán định là lỗi kiểu.
// Người dùng nên viết `genre: "42"` để khai báo string một cách tường minh.
func asString(v any) (string, bool) {
	if s, ok := v.(string); ok {
		return s, true
	}
	return "", false
}

// asInt chấp nhận mọi kiểu nguyên; float64 chỉ chấp nhận khi đúng là số nguyên (số YAML mặc định parse thành float64).
// Số dạng chuỗi không còn tự động chuyển——tránh nhầm lẫn với lỗi "đặt nhầm trường thành chuỗi".
func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		// chỉ chấp nhận khi float đúng là số nguyên (như yaml parse `5` → float64(5.0))
		if x == float64(int(x)) {
			return int(x), true
		}
		return 0, false
	default:
		return 0, false
	}
}

// asStringList phần tử phải là string; phần tử kiểu khác bị bỏ qua và ghi vào conflicts.
// Trả về (list, true) nghĩa là có phần tử hợp lệ; (nil, false) nghĩa là sai kiểu tổng thể hoặc mọi phần tử đều không hợp lệ.
func asStringList(p *Parsed, field string, v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for i, raw := range arr {
		s, ok := raw.(string)
		if !ok {
			p.Conflicts = append(p.Conflicts, Conflict{
				Source: p.Source,
				Kind:   ConflictTypeError,
				Field:  fmt.Sprintf("%s[%d]", field, i),
				Detail: i18n.Tf("rules.parse.list_string_expected", field, raw, raw),
			})
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func typeErr(source, field, expected string, got any) Conflict {
	return Conflict{
		Source: source,
		Kind:   ConflictTypeError,
		Field:  field,
		Detail: i18n.Tf("rules.parse.type_error", field, expected, got, got),
	}
}
