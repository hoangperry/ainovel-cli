package rules

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/voocel/ainovel-cli/internal/i18n"
)

// Merge gộp nhiều nguồn loader trả về thành Bundle cuối cùng.
//
// Quy tắc merge:
//   - trường có cấu trúc thông thường: ưu tiên gần nhất (cái sau ghi đè cái trước), nhiều nguồn khai báo cùng trường mà giá trị khác nhau thì ghi field_conflict
//   - fatigue_words: merge theo từng từ; cùng một từ được nhiều nguồn khai báo mà ngưỡng khác nhau thì ưu tiên gần nhất và ghi field_conflict
//   - nội dung Markdown: ghép theo thứ tự nguồn, mỗi đoạn thêm tiêu đề nguồn, không ghi đè
//   - sources: mọi đường dẫn file load thành công
//   - conflicts: conflict thời điểm parse + field_conflict thời điểm merge
//
// Tham số layers phải đã được sắp tăng dần theo SourceKind (hình thái output của loader.Load).
func Merge(layers []Parsed) Bundle {
	bundle := Bundle{
		Structured:  Structured{},
		Preferences: "",
		Sources:     make([]string, 0, len(layers)),
		Conflicts:   nil,
	}

	// Giai đoạn A: thu thập mọi nguồn khai báo của từng trường, tiện cho việc phán định conflict sau này
	declarations := map[string][]Parsed{}
	declare := func(field string, p Parsed) {
		declarations[field] = append(declarations[field], p)
	}
	for _, p := range layers {
		if p.Structured.Genre != "" {
			declare("genre", p)
		}
		if p.Structured.ChapterWords != nil {
			declare("chapter_words", p)
		}
		if len(p.Structured.ForbiddenChars) > 0 {
			declare("forbidden_chars", p)
		}
		if len(p.Structured.ForbiddenPhrases) > 0 {
			declare("forbidden_phrases", p)
		}
		if len(p.Structured.FatigueWords) > 0 {
			declare("fatigue_words", p)
		}
	}

	// Giai đoạn B: merge các trường có cấu trúc, thu được trường có cấu trúc cuối cùng.
	// Trường scalar/list giữ ghi đè gần nhất; fatigue_words là map, cộng dồn theo từng từ, tiện cho người dùng chỉ thêm vài fatigue word.
	for _, p := range layers {
		if p.Structured.Genre != "" {
			bundle.Structured.Genre = p.Structured.Genre
		}
		if p.Structured.ChapterWords != nil {
			bundle.Structured.ChapterWords = p.Structured.ChapterWords
		}
		if len(p.Structured.ForbiddenChars) > 0 {
			bundle.Structured.ForbiddenChars = p.Structured.ForbiddenChars
		}
		if len(p.Structured.ForbiddenPhrases) > 0 {
			bundle.Structured.ForbiddenPhrases = p.Structured.ForbiddenPhrases
		}
		if len(p.Structured.FatigueWords) > 0 {
			bundle.Structured.FatigueWords = mergeFatigueWords(bundle.Structured.FatigueWords, p.Structured.FatigueWords)
		}
	}

	// Giai đoạn C: dựng field_conflict (chỉ tính conflict khi nhiều nguồn + giá trị không nhất quán)
	for field, sources := range declarations {
		if len(sources) < 2 {
			continue
		}
		if field == "fatigue_words" {
			bundle.Conflicts = append(bundle.Conflicts, fatigueWordConflicts(sources)...)
			continue
		}
		if allEqual(field, sources) {
			continue
		}
		bundle.Conflicts = append(bundle.Conflicts, Conflict{
			Source: sources[len(sources)-1].Source,
			Kind:   ConflictFieldConflict,
			Field:  field,
			Detail: describeFieldConflict(field, sources),
		})
	}

	// Giai đoạn D: merge nội dung preference Markdown
	var sb strings.Builder
	for _, p := range layers {
		if strings.TrimSpace(p.Preference) == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "## [%s] %s\n\n", p.Kind, p.Source)
		sb.WriteString(p.Preference)
	}
	bundle.Preferences = sb.String()

	// Giai đoạn E: tổng hợp sources và conflict thời điểm parse
	for _, p := range layers {
		bundle.Sources = append(bundle.Sources, p.Source)
		bundle.Conflicts = append(bundle.Conflicts, p.Conflicts...)
	}

	return bundle
}

func mergeFatigueWords(dst, src map[string]int) map[string]int {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]int, len(src))
	}
	for word, limit := range src {
		dst[word] = limit
	}
	return dst
}

func fatigueWordConflicts(sources []Parsed) []Conflict {
	type declaration struct {
		source string
		limit  int
	}
	byWord := make(map[string][]declaration)
	for _, p := range sources {
		for word, limit := range p.Structured.FatigueWords {
			if word == "" {
				continue
			}
			byWord[word] = append(byWord[word], declaration{source: p.Source, limit: limit})
		}
	}

	words := make([]string, 0, len(byWord))
	for word := range byWord {
		words = append(words, word)
	}
	sort.Strings(words)

	var conflicts []Conflict
	for _, word := range words {
		ds := byWord[word]
		if len(ds) < 2 {
			continue
		}
		first := ds[0].limit
		allSame := true
		for _, d := range ds[1:] {
			if d.limit != first {
				allSame = false
				break
			}
		}
		if allSame {
			continue
		}
		parts := make([]string, 0, len(ds))
		for _, d := range ds {
			parts = append(parts, fmt.Sprintf("%s=%d", d.source, d.limit))
		}
		winner := ds[len(ds)-1]
		conflicts = append(conflicts, Conflict{
			Source: winner.source,
			Kind:   ConflictFieldConflict,
			Field:  "fatigue_words." + word,
			Detail: i18n.Tf("rules.merge.fatigue_conflict",
				word, strings.Join(parts, " | "), winner.source),
		})
	}
	return conflicts
}

// allEqual phán định giá trị của cùng một trường trong nhiều nguồn có hoàn toàn nhất quán hay không; nhất quán thì không báo conflict.
//
// Trường list về ngữ nghĩa không quan tâm thứ tự, nhưng về hiện thực yaml deserialization đã giữ thứ tự khai báo,
// hai cấu hình hoàn toàn giống nhau thì reflect.DeepEqual trả về true, đã đủ cho phán định "giá trị nhất quán".
// Trường hợp đặc biệt thứ tự khác nhau nhưng phần tử giống nhau bị xử lý là "không nhất quán" là chấp nhận được (vẫn just info, không chặn).
func allEqual(field string, sources []Parsed) bool {
	if len(sources) < 2 {
		return true
	}
	first := extractField(field, sources[0].Structured)
	for _, p := range sources[1:] {
		if !reflect.DeepEqual(first, extractField(field, p.Structured)) {
			return false
		}
	}
	return true
}

func extractField(field string, s Structured) any {
	switch field {
	case "genre":
		return s.Genre
	case "chapter_words":
		if s.ChapterWords == nil {
			return nil
		}
		return *s.ChapterWords
	case "forbidden_chars":
		return s.ForbiddenChars
	case "forbidden_phrases":
		return s.ForbiddenPhrases
	case "fatigue_words":
		return s.FatigueWords
	default:
		return nil
	}
}

// describeFieldConflict mô tả conflict theo cách dễ đọc cho con người: liệt kê mọi nguồn + giá trị của từng nguồn.
// Cuối cùng ghi chú nguồn có hiệu lực sau cùng (ưu tiên gần nhất).
func describeFieldConflict(field string, sources []Parsed) string {
	var parts []string
	for _, p := range sources {
		parts = append(parts, fmt.Sprintf("%s=%s", p.Source, formatFieldValue(field, p.Structured)))
	}
	winner := sources[len(sources)-1]
	return i18n.Tf(
		"rules.merge.field_conflict",
		field, strings.Join(parts, " | "), winner.Source,
	)
}

func formatFieldValue(field string, s Structured) string {
	switch field {
	case "genre":
		return s.Genre
	case "chapter_words":
		if s.ChapterWords == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%d-%d", s.ChapterWords.Min, s.ChapterWords.Max)
	case "forbidden_chars":
		return fmt.Sprintf("%v", s.ForbiddenChars)
	case "forbidden_phrases":
		return fmt.Sprintf("%v", s.ForbiddenPhrases)
	case "fatigue_words":
		return fmt.Sprintf("%v", s.FatigueWords)
	default:
		return "<unknown>"
	}
}
