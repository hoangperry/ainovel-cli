package rules

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Check kiểm tra cơ học nội dung chương theo các rule có cấu trúc, trả về danh sách fact vi phạm.
//
// Hợp đồng thiết kế:
//   - chỉ trả fact, không ra chỉ thị (luật sắt số một)
//   - không chặn bất kỳ quy trình nào của caller
//   - severity ánh xạ cố định theo loại rule (xem bảng chú thích trong types.go)
//
// Tham số:
//   - text: nội dung chương (bản cuối hay bản nháp đều được)
//   - wordCount: số chữ chương (đếm theo rune). Khi <0 thì checker tự tính, tránh để caller quét O(n) lặp lại.
//   - s: rule có cấu trúc đã merge; khi IsEmpty thì trả về nil ngay.
func Check(text string, wordCount int, s Structured) []Violation {
	if s.IsEmpty() {
		return nil
	}
	if wordCount < 0 {
		wordCount = utf8.RuneCountInString(text)
	}

	var violations []Violation
	violations = appendForbiddenChars(violations, text, s.ForbiddenChars)
	violations = appendForbiddenPhrases(violations, text, s.ForbiddenPhrases)
	violations = appendFatigueWords(violations, text, s.FatigueWords)
	violations = appendChapterWords(violations, wordCount, s.ChapterWords)
	return violations
}

// forbidden_chars: xuất hiện ≥1 lần là error.
// Cùng một rule chỉ sinh một violation, actual là số lần xuất hiện.
func appendForbiddenChars(vs []Violation, text string, list []string) []Violation {
	for _, ch := range list {
		if ch == "" {
			continue
		}
		n := strings.Count(text, ch)
		if n == 0 {
			continue
		}
		vs = append(vs, Violation{
			Rule:     "forbidden_chars",
			Target:   ch,
			Actual:   n,
			Severity: SeverityError,
		})
	}
	return vs
}

// forbidden_phrases: xuất hiện ≥1 lần là error; hành vi giống forbidden_chars, chỉ khác tên rule.
func appendForbiddenPhrases(vs []Violation, text string, list []string) []Violation {
	for _, ph := range list {
		if ph == "" {
			continue
		}
		n := strings.Count(text, ph)
		if n == 0 {
			continue
		}
		vs = append(vs, Violation{
			Rule:     "forbidden_phrases",
			Target:   ph,
			Actual:   n,
			Severity: SeverityError,
		})
	}
	return vs
}

// fatigue_words: chỉ vi phạm khi số lần xuất hiện trong chương này vượt ngưỡng, mức warning.
// Không cộng dồn xuyên chương——vấn đề xuyên chương để chẩn đoán sau.
func appendFatigueWords(vs []Violation, text string, m map[string]int) []Violation {
	for word, limit := range m {
		if word == "" || limit <= 0 {
			continue
		}
		n := strings.Count(text, word)
		if n <= limit {
			continue
		}
		vs = append(vs, Violation{
			Rule:     "fatigue_words",
			Target:   word,
			Limit:    limit,
			Actual:   n,
			Severity: SeverityWarning,
		})
	}
	return vs
}

// chapter_words: độ lệch số chữ.
// Lệch < 20%: warning; lệch ≥ 20%: error.
// Công thức lệch: dưới min dùng (min-actual)/min; trên max dùng (actual-max)/max.
func appendChapterWords(vs []Violation, wordCount int, rng *WordRange) []Violation {
	if rng == nil {
		return vs
	}
	var deviation float64
	switch {
	case wordCount < rng.Min:
		if rng.Min == 0 {
			return vs
		}
		deviation = float64(rng.Min-wordCount) / float64(rng.Min)
	case wordCount > rng.Max:
		if rng.Max == 0 {
			return vs
		}
		deviation = float64(wordCount-rng.Max) / float64(rng.Max)
	default:
		return vs // trong khoảng
	}

	severity := SeverityWarning
	if deviation >= ChapterWordsDeviationThreshold {
		severity = SeverityError
	}
	vs = append(vs, Violation{
		Rule:      "chapter_words",
		Limit:     fmt.Sprintf("%d-%d", rng.Min, rng.Max),
		Actual:    wordCount,
		Deviation: deviation,
		Severity:  severity,
	})
	return vs
}
