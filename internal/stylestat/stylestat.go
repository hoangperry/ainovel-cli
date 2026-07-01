// Package stylestat làm thống kê văn phong cấp toàn sách trên phần nội dung đã viết, cho ra dữ kiện thuần.
//
// Động cơ: cửa sổ rà soát trong cung truyện (~10 chương) vốn mù trước những mẫu lặp cố hóa ở cấp toàn sách — tic câu cú trung bình vài chục lần mỗi chương,
// hình thái cuối chương đồng dạng, lặp lại xuyên chương; nhìn từng chương thì chỗ nào cũng "bình thường", chỉ thống kê toàn sách mới phơi bày được. Thống kê giao cho code
// (xác định, zero ảo giác), phán định giao cho LLM (editor chấm điểm chiều theo con số, writer dựa vào đó tự tránh).
package stylestat

import (
	"regexp"
	"sort"
	"strings"
)

// minChapters dưới số chương này thì không xuất thống kê — mẫu quá nhỏ, tần suất không có ý nghĩa.
const minChapters = 5

// phraseWindow đào cụm từ động chỉ xét N chương gần nhất: cái writer cần tránh là "câu cửa miệng hiện tại".
const phraseWindow = 20

// Input là đầu vào thống kê. Chapters sắp theo số chương tăng dần; Stopwords là tên nhân vật và các danh từ riêng khác,
// khi đào cụm từ động sẽ bỏ qua (tên nhân vật xuất hiện vốn dĩ tần suất cao, không phải vấn đề văn phong).
type Input struct {
	Chapters  []string
	Titles    []string
	Stopwords []string
}

// Stats là kết quả thống kê văn phong toàn sách. Mọi trường đều là đếm dữ kiện, không chứa bất kỳ phán định hay chỉ thị nào.
type Stats struct {
	Chapters          int            `json:"chapters"`
	Patterns          []PatternStat  `json:"patterns,omitempty"`
	TopPhrases        []PhraseStat   `json:"top_phrases,omitempty"`
	RepeatedSentences []SentenceStat `json:"repeated_sentences,omitempty"`
	Ending            EndingStat     `json:"ending"`
	OpeningTimeRate   float64        `json:"opening_time_rate"`
	TitleFormats      *TitleStat     `json:"title_formats,omitempty"`
}

// PatternStat đếm toàn sách cho lớp mẫu câu cú cố định (tic văn phong AI phổ biến).
type PatternStat struct {
	Name       string  `json:"name"`
	Total      int     `json:"total"`
	PerChapter float64 `json:"per_chapter"`
}

// PhraseStat là cụm từ tần suất cao đào được trong phraseWindow chương gần nhất.
type PhraseStat struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// SentenceStat là câu dài lặp lại nguyên văn xuyên chương (bằng chứng trực tiếp của việc lặp lại trần thuật).
type SentenceStat struct {
	Text     string `json:"text"`
	Chapters int    `json:"chapters"`
	Count    int    `json:"count"`
}

// EndingStat là phân bố hình thái dòng cuối chương. Kết ngắn tự thân hợp lệ, đồng dạng toàn sách mới là vấn đề.
type EndingStat struct {
	ShortRatio  float64 `json:"short_ratio"`
	MedianRunes int     `json:"median_runes"`
}

// TitleStat đếm việc dùng lẫn lộn tiền tố kiểu "Chương N" trong tiêu đề chương (dùng lẫn = dấu vết cơ chế lộ ra trong sản phẩm).
type TitleStat struct {
	WithPrefix    int `json:"with_prefix"`
	WithoutPrefix int `json:"without_prefix"`
}

// patternDefs là các mẫu câu cú văn phong AI phổ biến. Đếm chỉ là xấp xỉ (regex không phân tích ngữ pháp),
// mục đích là so sánh baseline theo chiều dọc của chính cuốn sách này, độ chính xác tuyệt đối không quan trọng.
var patternDefs = []struct {
	name string
	re   *regexp.Regexp
}{
	{"矫正句『不是…(而)是…』", regexp.MustCompile(`不是[^。！？\n]{1,24}?[，、]?(?:而)?是`)},
	{"计时量词『X息/X瞬』", regexp.MustCompile(`[一两二三四五六七八九十几数半][息瞬]`)},
	{"明喻『像一/仿佛/如同/宛如』", regexp.MustCompile(`像一|仿佛|如同|宛如`)},
	{"沉默节拍『沉默了/没有说话/没有回头』", regexp.MustCompile(`沉默了|没有说话|没有回头`)},
}

var (
	sentenceSplit = regexp.MustCompile(`[。！？\n]+`)
	openingTimeRe = regexp.MustCompile(`夜|清晨|黎明|天亮|醒来|晨光|一整夜`)
	titlePrefixRe = regexp.MustCompile(`^#{0,2}\s*第[零〇一二三四五六七八九十百千万\d]+章`)
)

// shortEndingRunes dòng cuối không vượt quá số chữ này thì tính là "kết ngắn".
const shortEndingRunes = 30

// Compute tính thống kê văn phong toàn sách; số chương không đủ thì trả về nil.
func Compute(in Input) *Stats {
	n := len(in.Chapters)
	if n < minChapters {
		return nil
	}
	all := strings.Join(in.Chapters, "\n")

	s := &Stats{Chapters: n}
	for _, def := range patternDefs {
		total := len(def.re.FindAllStringIndex(all, -1))
		if total == 0 {
			continue
		}
		s.Patterns = append(s.Patterns, PatternStat{
			Name:       def.name,
			Total:      total,
			PerChapter: round1(float64(total) / float64(n)),
		})
	}
	s.TopPhrases = minePhrases(recentWindow(in.Chapters), in.Stopwords)
	s.RepeatedSentences = repeatedSentences(in.Chapters)
	s.Ending = endingShape(in.Chapters)
	s.OpeningTimeRate = openingTimeRate(in.Chapters)
	s.TitleFormats = titleFormats(in.Titles)
	return s
}

func recentWindow(chapters []string) []string {
	if len(chapters) <= phraseWindow {
		return chapters
	}
	return chapters[len(chapters)-phraseWindow:]
}

// minePhrases đào cụm từ tần suất cao 3-6 chữ trong cửa sổ.
// Lọc: chứa dấu câu/khoảng trắng, hư từ ở đầu/cuối, trúng danh từ riêng; khử trùng: cụm là chuỗi con của cụm đã chọn (hoặc ngược lại) thì bỏ.
func minePhrases(chapters []string, stopwords []string) []PhraseStat {
	text := strings.Join(chapters, "\n")
	runes := []rune(text)
	threshold := max(8, len(chapters)/2)

	counts := make(map[string]int)
	for size := 3; size <= 6; size++ {
		for i := 0; i+size <= len(runes); i++ {
			gram := runes[i : i+size]
			if !validGram(gram) {
				continue
			}
			counts[string(gram)]++
		}
	}

	stopGrams := stopwordBigrams(stopwords)
	type cand struct {
		text  string
		count int
	}
	var cands []cand
	for g, c := range counts {
		if c < threshold || hitStopword(g, stopGrams) {
			continue
		}
		cands = append(cands, cand{g, c})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].count != cands[j].count {
			return cands[i].count > cands[j].count
		}
		// Cùng tần suất thì lấy cụm dài hơn (lượng thông tin lớn hơn), rồi sắp ổn định theo thứ tự từ điển
		if len(cands[i].text) != len(cands[j].text) {
			return len(cands[i].text) > len(cands[j].text)
		}
		return cands[i].text < cands[j].text
	})

	var out []PhraseStat
	for _, c := range cands {
		if len(out) >= 8 {
			break
		}
		dup := false
		for _, picked := range out {
			if strings.Contains(picked.Text, c.text) || strings.Contains(c.text, picked.Text) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, PhraseStat{Text: c.text, Count: c.count})
		}
	}
	return out
}

// gramEdgeStop các n-gram có đầu/cuối là những hư từ/đại từ này không phải cụm văn phong, bỏ qua.
const gramEdgeStop = "的了着是在和与就也都还又把被他她它我你这那"

func validGram(gram []rune) bool {
	for _, r := range gram {
		if r < 0x4E00 || r > 0x9FFF { // chỉ giữ đoạn thuần chữ Hán
			return false
		}
	}
	if strings.ContainsRune(gramEdgeStop, gram[0]) || strings.ContainsRune(gramEdgeStop, gram[len(gram)-1]) {
		return false
	}
	return true
}

// stopwordBigrams tách danh từ riêng thành các đoạn 2 chữ: tên người thường xuất hiện trong văn dưới dạng một phần
// (ví dụ một tên hai chữ nằm trong cụm bốn chữ), khớp theo nguyên tên sẽ lọt lưới. Thà lọc hơi gắt — thiếu một dữ kiện cụm từ
// cũng không sao, để tên người lẫn vào danh sách câu cửa miệng mới là nhiễu.
func stopwordBigrams(stopwords []string) []string {
	var grams []string
	for _, w := range stopwords {
		runes := []rune(strings.TrimSpace(w))
		if len(runes) < 2 {
			continue
		}
		for i := 0; i+2 <= len(runes); i++ {
			grams = append(grams, string(runes[i:i+2]))
		}
	}
	return grams
}

func hitStopword(gram string, stopGrams []string) bool {
	for _, g := range stopGrams {
		if strings.Contains(gram, g) {
			return true
		}
	}
	return false
}

// repeatedSentences tìm các câu ≥12 chữ lặp lại nguyên văn qua ≥3 chương, lấy top 5 theo số lần.
func repeatedSentences(chapters []string) []SentenceStat {
	type rec struct {
		count    int
		chapters map[int]struct{}
	}
	seen := make(map[string]*rec)
	for ci, text := range chapters {
		for _, sent := range sentenceSplit.Split(text, -1) {
			// Bóc dấu ngoặc kép bao quanh rồi mới gộp: cùng một câu thoại có/không có ngoặc đầu không nên tính thành hai dòng
			sent = strings.Trim(strings.TrimSpace(sent), `"“”‘’「」『』`)
			if len([]rune(sent)) < 12 {
				continue
			}
			r := seen[sent]
			if r == nil {
				r = &rec{chapters: make(map[int]struct{})}
				seen[sent] = r
			}
			r.count++
			r.chapters[ci] = struct{}{}
		}
	}

	var out []SentenceStat
	for sent, r := range seen {
		if len(r.chapters) < 3 {
			continue
		}
		out = append(out, SentenceStat{Text: truncateRunes(sent, 40), Chapters: len(r.chapters), Count: r.count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Text < out[j].Text
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func endingShape(chapters []string) EndingStat {
	var lengths []int
	short := 0
	for _, text := range chapters {
		line := lastNonEmptyLine(text)
		if line == "" {
			continue
		}
		n := len([]rune(line))
		lengths = append(lengths, n)
		if n <= shortEndingRunes {
			short++
		}
	}
	if len(lengths) == 0 {
		return EndingStat{}
	}
	sort.Ints(lengths)
	return EndingStat{
		ShortRatio:  round2(float64(short) / float64(len(lengths))),
		MedianRunes: lengths[len(lengths)/2],
	}
}

func openingTimeRate(chapters []string) float64 {
	hit := 0
	for _, text := range chapters {
		if openingTimeRe.MatchString(firstParagraph(text)) {
			hit++
		}
	}
	return round2(float64(hit) / float64(len(chapters)))
}

func titleFormats(titles []string) *TitleStat {
	if len(titles) == 0 {
		return nil
	}
	t := &TitleStat{}
	for _, title := range titles {
		if strings.TrimSpace(title) == "" {
			continue
		}
		if titlePrefixRe.MatchString(title) {
			t.WithPrefix++
		} else {
			t.WithoutPrefix++
		}
	}
	// Chỉ dùng lẫn lộn mới đáng báo cáo; định dạng đồng nhất không phải vấn đề về mặt dữ kiện
	if t.WithPrefix == 0 || t.WithoutPrefix == 0 {
		return nil
	}
	return t
}

func lastNonEmptyLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// firstParagraph lấy dòng đầu tiên không rỗng và không phải tiêu đề Markdown (dòng đầu file chương thường là tiêu đề #).
func firstParagraph(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
