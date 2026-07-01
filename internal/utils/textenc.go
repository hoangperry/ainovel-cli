package utils

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// DecodeText giải mã byte của file văn bản người dùng cung cấp thành UTF-8: khi UTF-8 không hợp lệ thì chuyển mã theo GB18030
// (siêu tập của GBK) — phần lớn file txt tiểu thuyết tiếng Trung lưu truyền trên mạng dùng mã GBK, đọc thẳng như UTF-8
// sẽ ra toàn ký tự lỗi. Chuỗi byte không phải GBK sẽ bị decoder thay bằng U+FFFD (vốn đã là rác, để cơ chế báo lỗi
// zero-hit của caller dẫn dắt người dùng). Cuối cùng bóc UTF-8 BOM (nếu không, khớp đầu dòng sẽ dính nó vào).
func DecodeText(data []byte) string {
	if !utf8.Valid(data) {
		if decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data); err == nil {
			data = decoded
		}
	}
	return strings.TrimPrefix(string(data), "\uFEFF")
}
