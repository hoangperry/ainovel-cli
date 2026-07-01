package exp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
)

// chapterTitleIndex tra tiêu đề theo số chương, thiếu thì trả về chuỗi rỗng.
type chapterTitleIndex map[int]string

func buildTitleIndex(outline []domain.OutlineEntry) chapterTitleIndex {
	idx := make(chapterTitleIndex, len(outline))
	for _, e := range outline {
		if e.Title != "" {
			idx[e.Chapter] = e.Title
		}
	}
	return idx
}

// chapterLocation là vị trí thuộc về của một chương trong outline phân tầng. Chỉ giữ thông tin quyển mà bố cục export cần —
// cung truyện không vào export (dưới góc nhìn độc giả, cung truyện là cấu trúc nội bộ quá chi tiết).
type chapterLocation struct {
	VolumeIdx       int
	VolumeTitle     string
	IsFirstOfVolume bool
}

// buildLocations dựng {chapter -> location} theo thứ tự chương toàn cục của outline phân tầng.
// Số chương được dựng lại theo cùng quy tắc với FlattenOutline (cộng dồn thứ tự trong từng cung truyện trong từng quyển),
// để giữ nhất quán với số chương của Progress.CompletedChapters. Tầng cung truyện vẫn phải duyệt qua (bắt buộc để tính số chương toàn cục),
// nhưng không rơi vào location — export chỉ chèn phân tách ở đầu quyển.
func buildLocations(volumes []domain.VolumeOutline) map[int]chapterLocation {
	if len(volumes) == 0 {
		return nil
	}
	locs := make(map[int]chapterLocation)
	ch := 0
	for _, v := range volumes {
		firstOfVol := true
		for _, a := range v.Arcs {
			for range a.Chapters {
				ch++
				locs[ch] = chapterLocation{
					VolumeIdx:       v.Index,
					VolumeTitle:     v.Title,
					IsFirstOfVolume: firstOfVol,
				}
				firstOfVol = false
			}
		}
	}
	return locs
}

// chapterHeaderRe khớp dòng đầu tiêu đề Markdown có số chương (ví dụ "# 第N章" / "## 第 12 章" ...).
var chapterHeaderRe = regexp.MustCompile(`^#+\s+第.+?章`)

// atxTitleRe trích phần văn bản của tiêu đề ATX (# tiêu đề).
var atxTitleRe = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*$`)

// stripChapterTitleHeader bóc bỏ dòng đầu nếu nó là tiêu đề chương sẽ trùng với tiêu đề thống nhất do exporter sinh ra.
// Hai tình huống: ① "# 第N章 …" (có số chương); ② tiêu đề markdown mà văn bản của nó đúng là tiêu đề chương này
// (writer thường viết thẳng tên chương thuần làm tiêu đề ở dòng đầu nội dung, ví dụ "# 边村浮生", trùng với
// "第 N 章 边村浮生" do exporter sinh ra). Các h1 khác (ví dụ "# 序章") coi là một phần nội dung, giữ lại.
// Caller chịu trách nhiệm TrimSpace trước, nên các dòng trống dẫn đầu không nằm trong phạm vi xét.
func stripChapterTitleHeader(content, title string) string {
	first, rest, hasNewline := strings.Cut(content, "\n")
	if !isChapterTitleLine(first, title) {
		return content
	}
	if !hasNewline {
		return ""
	}
	return strings.TrimLeft(rest, "\n")
}

func isChapterTitleLine(line, title string) bool {
	if chapterHeaderRe.MatchString(line) {
		return true
	}
	if title = strings.TrimSpace(title); title == "" {
		return false
	}
	m := atxTitleRe.FindStringSubmatch(line)
	return len(m) == 2 && strings.TrimSpace(m[1]) == title
}

// renderTXT ghép thành văn bản cuối cùng.
//
// Thứ tự chương do chapters quyết định (caller đã khử trùng và sắp tăng dần theo số chương). bodies/titleIdx/locations
// đều xử lý theo kiểu "thiếu thì hạ cấp": thiếu tiêu đề chỉ xuất "第 N 章"; thiếu định vị phân tầng thì coi như outline phẳng.
func renderTXT(
	novelName string,
	chapters []int,
	titleIdx chapterTitleIndex,
	locations map[int]chapterLocation,
	bodies map[int]string,
) string {
	var b strings.Builder

	if name := strings.TrimSpace(novelName); name != "" {
		b.WriteString("《")
		b.WriteString(name)
		b.WriteString("》\n\n")
	}

	useLayered := len(locations) > 0

	for i, ch := range chapters {
		if useLayered {
			if loc, ok := locations[ch]; ok && loc.IsFirstOfVolume {
				b.WriteString("\n═══════════════════════════════════════════\n")
				fmt.Fprintf(&b, contentlang.Pick("           第 %d 卷  %s\n", "           Quyển %d  %s\n"), loc.VolumeIdx, strings.TrimSpace(loc.VolumeTitle))
				b.WriteString("═══════════════════════════════════════════\n\n")
			}
		}

		title := strings.TrimSpace(titleIdx[ch])
		if title != "" {
			fmt.Fprintf(&b, contentlang.Pick("第 %d 章  %s\n\n", "Chương %d  %s\n\n"), ch, title)
		} else {
			fmt.Fprintf(&b, contentlang.Pick("第 %d 章\n\n", "Chương %d\n\n"), ch)
		}

		body := stripChapterTitleHeader(strings.TrimSpace(bodies[ch]), title)
		b.WriteString(body)
		b.WriteString("\n")
		if i < len(chapters)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}
