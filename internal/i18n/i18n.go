// Package i18n cung cấp một message catalog tối giản, không phụ thuộc thư viện
// ngoài, để app hiển thị giao diện operator song ngữ Việt/Anh (Vietnamese primary).
//
// Thiết kế (KISS):
//   - Một global locale duy nhất, set một lần lúc khởi động qua SetLang. App là
//     single-user CLI nên không cần truyền message.Printer qua mọi call site.
//   - Catalog đăng ký theo từng vùng qua Register() trong init() của các file
//     cat_<area>_<lang>.go, nên nhiều file có thể bổ sung key mà KHÔNG tranh chấp
//     ghi cùng một file — quan trọng cho việc refactor song song.
//   - Fallback: locale hiện tại -> tiếng Việt (primary) -> raw key (không panic,
//     không rỗng; key hiện nguyên văn để dễ grep khi thiếu bản dịch).
//
// LƯU Ý về %w (CRITICAL): để bọc lỗi mà vẫn giữ nguyên chuỗi errors.Is/As, dùng
//
//	fmt.Errorf(i18n.T("error.key"), err)
//
// KHÔNG dùng Tf cho %w — Tf gọi fmt.Sprintf, vốn không hiểu động từ %w.
package i18n

import "fmt"

// Lang là mã ngôn ngữ giao diện.
type Lang string

const (
	LangVI Lang = "vi"
	LangEN Lang = "en"
)

// DefaultLang là locale mặc định và cũng là tầng fallback chính.
const DefaultLang = LangVI

// catalogs giữ toàn bộ bản dịch đã đăng ký, theo từng ngôn ngữ.
var catalogs = map[Lang]map[string]string{
	LangVI: {},
	LangEN: {},
}

// current là locale đang hoạt động; khởi tạo = DefaultLang để T luôn có giá trị
// ngay cả trước khi SetLang được gọi (ví dụ lỗi parse flag rất sớm trong main).
var current = DefaultLang

// Register nạp một khối bản dịch cho một ngôn ngữ. Gọi trong init() của các file
// catalog theo vùng. Panic nếu trùng key — đó là lỗi lập trình (hai vùng giẫm
// chân nhau), phải lộ ngay lúc khởi động/test thay vì âm thầm ghi đè.
func Register(lang Lang, entries map[string]string) {
	cat, ok := catalogs[lang]
	if !ok {
		cat = map[string]string{}
		catalogs[lang] = cat
	}
	for k, v := range entries {
		if _, dup := cat[k]; dup {
			panic(fmt.Sprintf("i18n: trùng key %q cho ngôn ngữ %q", k, lang))
		}
		cat[k] = v
	}
}

// SetLang đặt locale giao diện đang hoạt động. Gọi một lần lúc khởi động, sau khi
// đã load config. Không an toàn cho goroutine theo thiết kế (single-user CLI).
// Giá trị không hợp lệ rơi về DefaultLang.
func SetLang(l Lang) {
	if _, ok := catalogs[l]; ok {
		current = l
		return
	}
	current = DefaultLang
}

// CurrentLang trả về locale đang hoạt động.
func CurrentLang() Lang { return current }

// ParseLang chuyển chuỗi thành Lang. Trả về (lang, true) nếu hợp lệ.
func ParseLang(s string) (Lang, bool) {
	switch Lang(s) {
	case LangVI:
		return LangVI, true
	case LangEN:
		return LangEN, true
	default:
		return "", false
	}
}

// T trả về chuỗi đã bản địa hoá cho key. Fallback: locale hiện tại -> vi -> raw key.
func T(key string) string {
	if v, ok := catalogs[current][key]; ok {
		return v
	}
	if v, ok := catalogs[DefaultLang][key]; ok {
		return v
	}
	return key
}

// Tf trả về chuỗi đã bản địa hoá và định dạng qua fmt.Sprintf (dùng cho %d/%s...).
// KHÔNG dùng cho %w — xem lưu ý ở đầu package.
func Tf(key string, args ...any) string {
	return fmt.Sprintf(T(key), args...)
}
