package exp

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/contentlang"
)

// renderEPUB đóng gói tập hợp chương thành luồng byte EPUB 3.
//
// Cấu trúc gói (OEBPS là container của OPS package):
//
//	mimetype                    (bắt buộc là mục đầu tiên của zip + Method=Store không nén)
//	META-INF/container.xml      (trỏ tới OEBPS/content.opf)
//	OEBPS/content.opf           (metadata + manifest + spine)
//	OEBPS/nav.xhtml             (EPUB 3 navigation)
//	OEBPS/style.css             (dàn trang tối giản)
//	OEBPS/cover.xhtml           (tên sách, tùy chọn)
//	OEBPS/chapterNNN.xhtml      (mỗi chương một file)
func renderEPUB(
	novelName string,
	chapters []int,
	titleIdx chapterTitleIndex,
	locations map[int]chapterLocation,
	bodies map[int]string,
) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 1. mimetype bắt buộc là mục đầu tiên của zip + Store (không nén) + nội dung chính xác không BOM
	mt, err := zw.CreateHeader(&zip.FileHeader{
		Name:   "mimetype",
		Method: zip.Store,
	})
	if err != nil {
		return nil, fmt.Errorf("create mimetype: %w", err)
	}
	if _, err := mt.Write([]byte("application/epub+zip")); err != nil {
		return nil, err
	}

	if err := zipDeflate(zw, "META-INF/container.xml", containerXML); err != nil {
		return nil, err
	}
	if err := zipDeflate(zw, "OEBPS/style.css", styleCSS); err != nil {
		return nil, err
	}

	hasCover := strings.TrimSpace(novelName) != ""
	if hasCover {
		if err := zipDeflate(zw, "OEBPS/cover.xhtml", renderCoverXHTML(novelName)); err != nil {
			return nil, err
		}
	}

	for _, ch := range chapters {
		loc, hasLoc := locations[ch]
		title := strings.TrimSpace(titleIdx[ch])
		body := stripChapterTitleHeader(strings.TrimSpace(bodies[ch]), title)
		xhtml := renderChapterXHTML(ch, title, loc, hasLoc, body)
		if err := zipDeflate(zw, "OEBPS/"+chapterFileName(ch), xhtml); err != nil {
			return nil, err
		}
	}

	if err := zipDeflate(zw, "OEBPS/nav.xhtml", renderNavXHTML(hasCover, chapters, titleIdx)); err != nil {
		return nil, err
	}

	if err := zipDeflate(zw, "OEBPS/content.opf", renderOPF(novelName, hasCover, chapters)); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("finalize zip: %w", err)
	}
	return buf.Bytes(), nil
}

// zipDeflate ghi một mục thông thường (có nén).
func zipDeflate(zw *zip.Writer, name, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	_, err = w.Write([]byte(content))
	return err
}

func chapterFileName(ch int) string {
	return fmt.Sprintf("chapter%03d.xhtml", ch)
}

// chapterID là id của manifest item; tương ứng một-một với tên file.
func chapterID(ch int) string {
	return fmt.Sprintf("ch%03d", ch)
}

// Template cố định ────────────────────────────────────────────────

const containerXML = `<?xml version="1.0" encoding="utf-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
`

const styleCSS = `body { font-family: serif; line-height: 1.7; margin: 1em; }
h1.book-title { font-size: 2em; text-align: center; margin: 4em 0 1em; }
.volume-divider { font-size: 1.6em; text-align: center; margin: 4em 0 1em; font-weight: bold; }
h1.chapter-title { font-size: 1.4em; text-align: center; margin: 2em 0 1.5em; }
p { text-indent: 2em; margin: 0.5em 0; }
`

// XHTML chương ────────────────────────────────────────────────

func renderChapterXHTML(ch int, title string, loc chapterLocation, hasLoc bool, body string) string {
	var b strings.Builder
	displayTitle := fmt.Sprintf(contentlang.Pick("第 %d 章", "Chương %d"), ch)
	if title != "" {
		displayTitle = fmt.Sprintf(contentlang.Pick("第 %d 章 %s", "Chương %d %s"), ch, title)
	}

	fmt.Fprintf(&b, `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="zh-CN">
<head>
  <title>%s</title>
  <link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
`, html.EscapeString(displayTitle))

	if hasLoc && loc.IsFirstOfVolume {
		fmt.Fprintf(&b, contentlang.Pick("  <div class=\"volume-divider\">第 %d 卷 %s</div>\n", "  <div class=\"volume-divider\">Quyển %d %s</div>\n"),
			loc.VolumeIdx, html.EscapeString(strings.TrimSpace(loc.VolumeTitle)))
	}

	fmt.Fprintf(&b, "  <h1 class=\"chapter-title\">%s</h1>\n", html.EscapeString(displayTitle))
	for _, para := range splitParagraphs(body) {
		fmt.Fprintf(&b, "  <p>%s</p>\n", html.EscapeString(para))
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// splitParagraphs cắt đoạn theo dòng trống; nhiều dòng trống liên tiếp coi như một lần ngắt đoạn. Các đoạn trả về đều đã TrimSpace và không rỗng.
// Xuống dòng trong đoạn (một \n đơn) giữ lại thành khoảng trắng trong đoạn — thẻ <p> của XHTML không giữ ngắt dòng, trình duyệt tự wrap.
func splitParagraphs(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	parts := strings.Split(body, "\n\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Xuống dòng trong đoạn đổi thành khoảng trắng, tránh mất nội dung khi XHTML render
		p = strings.ReplaceAll(p, "\n", " ")
		out = append(out, p)
	}
	return out
}

// Bìa sách ────────────────────────────────────────────────

func renderCoverXHTML(novelName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="zh-CN">
<head>
  <title>%s</title>
  <link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
`, contentlang.Pick("封面", "Bìa"))
	if name := strings.TrimSpace(novelName); name != "" {
		fmt.Fprintf(&b, "  <h1 class=\"book-title\">%s</h1>\n", html.EscapeString(name))
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// nav.xhtml (EPUB 3 navigation) ────────────────────────────────────────────────

func renderNavXHTML(hasCover bool, chapters []int, titleIdx chapterTitleIndex) string {
	var b strings.Builder
	tocLabel := contentlang.Pick("目录", "Mục lục")
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="zh-CN">
<head>
  <title>%s</title>
  <link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
  <nav epub:type="toc">
    <h1>%s</h1>
    <ol>
`, tocLabel, tocLabel)
	if hasCover {
		fmt.Fprintf(&b, "      <li><a href=\"cover.xhtml\">%s</a></li>\n", contentlang.Pick("封面", "Bìa"))
	}

	// Danh sách chương dạng phẳng. Gom nhóm theo quyển/cung truyện trong reader lại không gọn bằng mục lục một tầng (reader tự gập lại),
	// hơn nữa ol lồng nhau trong nav EPUB 3 render lạ trên một số reader. Giữ đơn giản.
	for _, ch := range chapters {
		title := strings.TrimSpace(titleIdx[ch])
		display := fmt.Sprintf(contentlang.Pick("第 %d 章", "Chương %d"), ch)
		if title != "" {
			display = fmt.Sprintf(contentlang.Pick("第 %d 章 %s", "Chương %d %s"), ch, title)
		}
		fmt.Fprintf(&b, "      <li><a href=\"%s\">%s</a></li>\n",
			chapterFileName(ch), html.EscapeString(display))
	}

	b.WriteString(`    </ol>
  </nav>
</body>
</html>
`)
	return b.String()
}

// content.opf ────────────────────────────────────────────────

func renderOPF(novelName string, hasCover bool, chapters []int) string {
	bookID := bookIdentifier(novelName)
	modified := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	title := strings.TrimSpace(novelName)
	if title == "" {
		title = "Untitled"
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid" xml:lang="zh-CN">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">%s</dc:identifier>
    <dc:title>%s</dc:title>
    <dc:language>zh-CN</dc:language>
    <dc:creator>ainovel-cli</dc:creator>
    <meta property="dcterms:modified">%s</meta>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="css" href="style.css" media-type="text/css"/>
`, html.EscapeString(bookID), html.EscapeString(title), modified)

	if hasCover {
		b.WriteString(`    <item id="cover" href="cover.xhtml" media-type="application/xhtml+xml"/>` + "\n")
	}
	for _, ch := range chapters {
		fmt.Fprintf(&b, `    <item id="%s" href="%s" media-type="application/xhtml+xml"/>`+"\n",
			chapterID(ch), chapterFileName(ch))
	}

	b.WriteString("  </manifest>\n  <spine>\n")
	if hasCover {
		b.WriteString(`    <itemref idref="cover"/>` + "\n")
	}
	b.WriteString(`    <itemref idref="nav"/>` + "\n")
	for _, ch := range chapters {
		fmt.Fprintf(&b, `    <itemref idref="%s"/>`+"\n", chapterID(ch))
	}
	b.WriteString("  </spine>\n</package>\n")
	return b.String()
}

// bookIdentifier dẫn xuất chuỗi UUID ổn định từ tên tiểu thuyết.
//
// **Chỉ dùng novelName, không pha danh sách chương**: danh tính tác phẩm nên gắn với "đây là cuốn sách nào", không gắn với "phạm vi export"
// hay "tại thời điểm export đã viết tới chương mấy". Export lại cùng một cuốn thì ID không đổi, reader dựa vào đó nhận diện là cùng một tác phẩm
// ở bản cập nhật (cập nhật hay không do timestamp dcterms:modified đảm nhận). novelName rỗng dùng chung ID là
// edge case đã biết: người dùng không đặt tên cho cả hai cuốn thì tự chịu trách nhiệm.
func bookIdentifier(novelName string) string {
	h := sha1.New()
	h.Write([]byte(novelName))
	sum := h.Sum(nil)
	// Định dạng theo kiểu UUID (8-4-4-4-12), không yêu cầu RFC 4122 nghiêm ngặt — EPUB chỉ yêu cầu chuỗi duy nhất và ổn định.
	return fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x",
		sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
