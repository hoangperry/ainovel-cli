package imp

import (
	"fmt"
	"regexp"
	"strings"
)

// envelopeTagRe khớp dòng === TAG === (trước sau có thể có khoảng trắng), không phân biệt hoa thường.
var envelopeTagRe = regexp.MustCompile(`(?m)^\s*===\s*([A-Z_]+)\s*===\s*$`)

// parseTaggedEnvelope parse output nhiều đoạn dạng `=== TAG ===\nbody...` thành map.
// key là tên tag viết hoa, value là đoạn tương ứng (đã trim khoảng trắng đầu cuối).
// Khi xuất hiện tag trùng, cái sau ghi đè cái trước.
func parseTaggedEnvelope(text string) map[string]string {
	matches := envelopeTagRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make(map[string]string, len(matches))
	for i, m := range matches {
		tag := strings.ToUpper(text[m[2]:m[3]])
		bodyStart := m[1]
		bodyEnd := len(text)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		out[tag] = strings.TrimSpace(text[bodyStart:bodyEnd])
	}
	return out
}

// requireTags kiểm tra envelope bắt buộc chứa các tag đã cho và không rỗng.
func requireTags(env map[string]string, tags ...string) error {
	var missing []string
	for _, t := range tags {
		if strings.TrimSpace(env[t]) == "" {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required tags: %s", strings.Join(missing, ", "))
	}
	return nil
}
