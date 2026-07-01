package assets

import (
	"strings"
	"testing"
)

// languageDirective phải đúng cho từng giá trị; "original"/rỗng/lạ = no-op.
func TestLanguageDirective(t *testing.T) {
	if d := languageDirective("vi"); !strings.Contains(d, "tiếng Việt") {
		t.Errorf("vi directive thiếu chỉ thị tiếng Việt: %q", d)
	}
	if d := languageDirective("en"); !strings.Contains(d, "English") {
		t.Errorf("en directive thiếu chỉ thị tiếng Anh: %q", d)
	}
	for _, lang := range []string{"original", "", "zh", "xx"} {
		if d := languageDirective(lang); d != "" {
			t.Errorf("languageDirective(%q) phải rỗng (no-op), nhận %q", lang, d)
		}
	}
}

// Load(vi) phải chèn directive tiếng Việt vào MỌI role sáng tác; "original" thì không.
func TestLoadInjectsLanguageDirective(t *testing.T) {
	vi := Load("default", "vi").Prompts
	for name, p := range map[string]string{
		"coordinator":     vi.Coordinator,
		"architect-short": vi.ArchitectShort,
		"architect-long":  vi.ArchitectLong,
		"writer":          vi.Writer,
		"editor":          vi.Editor,
	} {
		if !strings.Contains(p, "nội dung truyện bằng tiếng Việt") {
			t.Errorf("role %q thiếu languageDirective tiếng Việt", name)
		}
	}

	orig := Load("default", "original").Prompts
	if strings.Contains(orig.Writer, "nội dung truyện bằng tiếng Việt") || strings.Contains(orig.Writer, "Writing language") {
		t.Error("output_lang=original không được chèn directive (giữ hành vi gốc)")
	}

	// import/simulation KHÔNG nhận directive (không phải role sáng tác chính văn).
	if strings.Contains(vi.ImportFoundation, "nội dung truyện bằng tiếng Việt") {
		t.Error("ImportFoundation không nên nhận languageDirective")
	}
}
