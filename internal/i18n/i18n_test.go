package i18n

import (
	"errors"
	"fmt"
	"testing"
)

// TestCatalogParity bảo đảm mọi key tồn tại đối xứng giữa vi và en. Lệch một bên
// nghĩa là người dùng locale kia sẽ thấy raw key — phải bắt ngay khi test.
func TestCatalogParity(t *testing.T) {
	vi := catalogs[LangVI]
	en := catalogs[LangEN]
	for k := range vi {
		if _, ok := en[k]; !ok {
			t.Errorf("key %q có ở vi nhưng thiếu ở en", k)
		}
	}
	for k := range en {
		if _, ok := vi[k]; !ok {
			t.Errorf("key %q có ở en nhưng thiếu ở vi", k)
		}
	}
}

// TestVerbParity bảo đảm số fmt verb (%w/%s/%d...) khớp giữa vi và en cho cùng key.
// Lệch verb sẽ làm bọc lỗi / format vỡ ở một trong hai ngôn ngữ.
func TestVerbParity(t *testing.T) {
	for k, viVal := range catalogs[LangVI] {
		enVal, ok := catalogs[LangEN][k]
		if !ok {
			continue
		}
		if got, want := countVerbs(viVal), countVerbs(enVal); got != want {
			t.Errorf("key %q: số fmt verb lệch vi=%d en=%d", k, got, want)
		}
	}
}

// countVerbs đếm động từ fmt (bỏ qua %% literal). Xấp xỉ nhưng đủ cho parity.
func countVerbs(s string) int {
	n := 0
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '%' {
			if s[i+1] == '%' {
				i++
				continue
			}
			n++
		}
	}
	return n
}

// TestFallbackRawKey: thiếu key trả về nguyên văn key, không rỗng, không panic.
func TestFallbackRawKey(t *testing.T) {
	SetLang(LangEN)
	defer SetLang(DefaultLang)
	if got := T("__khong_ton_tai__"); got != "__khong_ton_tai__" {
		t.Errorf("kỳ vọng raw key, nhận %q", got)
	}
}

// TestFallbackToDefault: thiếu key ở locale hiện tại nhưng có ở vi -> rơi về vi.
// Dọn key test khỏi catalog global để không phá vỡ parity bất kể thứ tự chạy.
func TestFallbackToDefault(t *testing.T) {
	Register(LangVI, map[string]string{"test.only_vi": "chỉ có ở vi"})
	defer delete(catalogs[LangVI], "test.only_vi")
	SetLang(LangEN)
	defer SetLang(DefaultLang)
	if got := T("test.only_vi"); got != "chỉ có ở vi" {
		t.Errorf("kỳ vọng fallback về vi, nhận %q", got)
	}
}

// TestErrorWrapPreserved: bọc lỗi bằng fmt.Errorf(T(key), err) giữ nguyên errors.Is.
func TestErrorWrapPreserved(t *testing.T) {
	SetLang(LangVI)
	defer SetLang(DefaultLang)
	sentinel := errors.New("boom")
	err := fmt.Errorf(T("error.config.load_failed"), sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is vỡ sau khi bọc đã bản địa hoá: %v", err)
	}
}

// TestRegisterDuplicatePanics: trùng key phải panic để lộ va chạm vùng.
func TestRegisterDuplicatePanics(t *testing.T) {
	defer delete(catalogs[LangVI], "test.dup")
	defer func() {
		if recover() == nil {
			t.Fatal("kỳ vọng panic khi Register trùng key")
		}
	}()
	Register(LangVI, map[string]string{"test.dup": "a"})
	Register(LangVI, map[string]string{"test.dup": "b"})
}
