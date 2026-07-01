package tools

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/rules"
)

// filterCJKAssumingRules tắt non_cjk_fragments + chapter_words cho vi/en, giữ rule
// còn lại; với original/zh giữ nguyên tất cả.
func TestFilterCJKAssumingRules(t *testing.T) {
	in := []rules.Violation{
		{Rule: "non_cjk_fragments"},
		{Rule: "chapter_words"},
		{Rule: "forbidden_chars"},
		{Rule: "fatigue_words"},
	}

	for _, lang := range []string{"vi", "en"} {
		got := filterCJKAssumingRules(append([]rules.Violation(nil), in...), lang)
		for _, v := range got {
			if v.Rule == "non_cjk_fragments" || v.Rule == "chapter_words" {
				t.Errorf("lang=%s: rule giả định CJK %q phải bị lọc", lang, v.Rule)
			}
		}
		if len(got) != 2 {
			t.Errorf("lang=%s: còn lại %d rule, want 2 (forbidden_chars + fatigue_words)", lang, len(got))
		}
	}

	for _, lang := range []string{"original", "", "zh"} {
		got := filterCJKAssumingRules(append([]rules.Violation(nil), in...), lang)
		if len(got) != 4 {
			t.Errorf("lang=%s: phải giữ đủ 4 rule, nhận %d", lang, len(got))
		}
	}
}
