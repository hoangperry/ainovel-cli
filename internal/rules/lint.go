package rules

import (
	"regexp"
	"strings"
)

// Lint là kiểm tra lằn ranh sản phẩm tích hợp sẵn: quét phần cơ chế còn sót trong nội dung, không liên quan rule người dùng, luôn chạy khi commit.
// Cùng hợp đồng với Check——chỉ trả fact (luật sắt số một), không chặn quy trình, do rà soát/người dùng phán định.
//
// Hiện có ba loại (đều là khiếm khuyết thực chứng từ sản phẩm chạy dài thực tế):
//   - markdown_residue: nội dung còn sót ** in đậm, dòng tiêu đề # ngoài dòng đầu (export txt sẽ lộ ký hiệu)
//   - non_cjk_fragments: đoạn chữ cái Latin liên tục (mô hình lẫn lộn ngôn ngữ, ví dụ nội dung tiếng Trung lẫn trần "pattern")
func Lint(text string) []Violation {
	var vs []Violation
	vs = appendMarkdownResidue(vs, text)
	vs = appendNonCJKFragments(vs, text)
	return vs
}

func appendMarkdownResidue(vs []Violation, text string) []Violation {
	if n := strings.Count(text, "**"); n > 0 {
		vs = append(vs, Violation{
			Rule:     "markdown_residue",
			Target:   "**",
			Actual:   n,
			Severity: SeverityWarning,
		})
	}
	headings := 0
	seenContent := false
	for line := range strings.SplitSeq(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		// tiêu đề # ở dòng không rỗng đầu tiên là định dạng hợp lệ của file chương (không hardcode theo số dòng, dung thứ dòng trống dẫn đầu)
		first := !seenContent
		seenContent = true
		if !first && strings.HasPrefix(t, "#") {
			headings++
		}
	}
	if headings > 0 {
		vs = append(vs, Violation{
			Rule:     "markdown_residue",
			Target:   "#",
			Actual:   headings,
			Severity: SeverityWarning,
		})
	}
	return vs
}

var latinFragmentRe = regexp.MustCompile(`[A-Za-z]{2,}`)

// appendNonCJKFragments báo cáo tổng số lần và ví dụ đã khử trùng lặp của các đoạn chữ cái Latin.
// Tiếng Anh hợp lệ của thể loại hiện đại (tên thương hiệu/viết tắt) cũng sẽ trúng——fact mức warning, do rà soát phán định theo thể loại.
func appendNonCJKFragments(vs []Violation, text string) []Violation {
	matches := latinFragmentRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return vs
	}
	seen := make(map[string]struct{})
	var examples []string
	for _, m := range matches {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		if len(examples) < 3 {
			examples = append(examples, m)
		}
	}
	return append(vs, Violation{
		Rule:     "non_cjk_fragments",
		Target:   strings.Join(examples, "、"),
		Actual:   len(matches),
		Severity: SeverityWarning,
	})
}
