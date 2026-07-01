package imp

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/voocel/ainovel-cli/internal/utils"
)

// Regex tiêu đề chương mặc định. Bao phủ các tiêu đề tiếng Trung thường gặp (第N章/回/话/卷/节/幕, 卷N, 序章/楔子/尾声/番外/外传 v.v.)
// và tiếng Anh (Chapter N, Prologue, Epilogue), tương thích tiền tố tiêu đề Markdown (# / ##),
// tiền tố 「正文 第N章」của txt hệ Qidian, cùng các tiêu đề bọc trong【】〖〗.
//
// Nhóm đặt tên: nhóm phụ đề ưu tiên hơn nhóm từ khóa (khi trích thì quay lui theo thứ tự priority):
//   - cn    phụ đề chương có số (phần văn bản sau 第X章/回/话/卷/节/幕)
//   - vol   phụ đề quyển độc lập (phần văn bản sau 卷X)
//   - sp    phụ đề đơn vị đặc biệt (phần văn bản sau 序章/楔子/尾声/番外)
//   - en    phụ đề chương tiếng Anh (phần văn bản sau Chapter X / Prologue / Epilogue)
//   - spkw  bản thân từ khóa đơn vị đặc biệt (làm tiêu đề khi không có phụ đề, ví dụ 「楔子」「番外」)
//   - enkw  bản thân từ khóa đơn vị đặc biệt tiếng Anh (làm tiêu đề khi không có phụ đề, ví dụ 「Prologue」)

// ws là nội dung lớp ký tự: khoảng trắng ASCII + khoảng trắng full-width. \s của Go RE2 chỉ gồm khoảng trắng ASCII,
// trong khi dàn chữ tiếng Trung thường dùng U+3000 để phân tách tiêu đề (「第一章　风起」).
const ws = `\s\x{3000}`

// cnNum là các ký tự số dùng được cho số chương: Ả Rập / full-width / chữ Trung thường / chữ Trung phồn thể hoa (壹贰叁…萬).
const cnNum = `零〇○Ｏ０一二三四五六七八九十百千万两壹贰貳叁參肆伍陆陸柒捌玖拾佰仟萬兩\d`

// sub là phần bắt phụ đề: lấy tới cuối dòng, nhưng không nuốt ký tự bọc phải (】〗), để dành cho dấu đóng tùy chọn ở cuối.
const sub = `[^】〗\n]*`

var defaultChapterRegex = regexp.MustCompile(
	`(?im)^#{0,2}[` + ws + `]*(?:正文[` + ws + `]*)?[【〖]?[` + ws + `]*(?:` +
		`第\s*(?:[` + cnNum + `]+)\s*(?:章|回|话|卷|节|幕)` +
		`(?:[:：．\.` + ws + `]+(?P<cn>` + sub + `))?` +
		`|` +
		`卷\s*(?:[` + cnNum + `]+)` +
		`(?:[:：．\.` + ws + `]+(?P<vol>` + sub + `))?` +
		`|` +
		`(?P<spkw>序章|序幕|楔子|引子|前言|序言|尾声|终章|后记|番外|外传)` +
		`(?:[:：．\.` + ws + `]+(?P<sp>` + sub + `))?` +
		`|` +
		`(?:Chapter\s+(?:\d+|[IVXLCDM]+)|(?P<enkw>Prologue|Epilogue))` +
		`(?:[:：．\.` + ws + `]+(?P<en>` + sub + `))?` +
		`)[` + ws + `]*[】〗]?[` + ws + `]*$`,
)

// SplitFile cắt một file văn bản đơn thành danh sách chương.
func SplitFile(path string) ([]Chapter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}
	text := utils.DecodeText(data)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("source file is empty: %s", path)
	}
	return splitText(text, defaultChapterRegex), nil
}

// splitText là bản cắt dạng hàm thuần, tiện cho unit test.
func splitText(text string, pattern *regexp.Regexp) []Chapter {
	lines := strings.Split(text, "\n")
	type marker struct {
		line  int
		title string
	}
	var marks []marker
	for i, ln := range lines {
		if loc := pattern.FindStringSubmatchIndex(ln); loc != nil {
			marks = append(marks, marker{line: i, title: extractTitle(ln, pattern, loc, len(marks)+1)})
		}
	}
	if len(marks) == 0 {
		return nil
	}

	chapters := make([]Chapter, 0, len(marks))
	for i, m := range marks {
		end := len(lines)
		if i+1 < len(marks) {
			end = marks[i+1].line
		}
		body := strings.Join(lines[m.line+1:end], "\n")
		body = stripTrailingNoise(body)
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		chapters = append(chapters, Chapter{Title: m.title, Content: body})
	}
	return chapters
}

// extractTitle trích tiêu đề chương từ dòng khớp; ưu tiên lấy named capture, nếu không thì quay về placeholder số chương.
func extractTitle(line string, pattern *regexp.Regexp, loc []int, fallbackNum int) string {
	subnames := pattern.SubexpNames()
	priority := []string{"cn", "vol", "sp", "en", "spkw", "enkw"}
	for _, name := range priority {
		idx := pattern.SubexpIndex(name)
		if idx <= 0 {
			continue
		}
		if loc[2*idx] < 0 {
			continue
		}
		if t := strings.TrimSpace(line[loc[2*idx]:loc[2*idx+1]]); t != "" {
			return t
		}
	}
	// Phương án cuối: lấy nhóm bắt không rỗng đầu tiên (phòng thủ, các named group của regex mặc định đã phủ hết các nhánh)
	for i := 1; i < len(subnames); i++ {
		if loc[2*i] < 0 {
			continue
		}
		if t := strings.TrimSpace(line[loc[2*i]:loc[2*i+1]]); t != "" {
			return t
		}
	}
	return fmt.Sprintf("第%d章", fallbackNum)
}

// stripTrailingNoise bóc bỏ nhiễu phần đuôi thường gặp (license trailer như Project Gutenberg).
var trailerRe = regexp.MustCompile(`(?im)^\s*Project Gutenberg(?:\(TM\)|™)?[\s\S]*$`)

func stripTrailingNoise(content string) string {
	if loc := trailerRe.FindStringIndex(content); loc != nil {
		return strings.TrimRight(content[:loc[0]], " \t\n")
	}
	return content
}
