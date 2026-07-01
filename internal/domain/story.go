package domain

// Novel là metadata của tiểu thuyết.
type Novel struct {
	Name          string `json:"name"`
	TotalChapters int    `json:"total_chapters"`
}

// OutlineEntry là mục dàn ý, tương ứng một chương.
type OutlineEntry struct {
	Chapter   int      `json:"chapter"`
	Title     string   `json:"title"`
	CoreEvent string   `json:"core_event"`
	Hook      string   `json:"hook"`
	Scenes    []string `json:"scenes"`
}

// Character là hồ sơ nhân vật.
type Character struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"` // bí danh/danh hiệu/biệt hiệu (như "thiếu niên phế vật", "Viêm ca")
	Role        string   `json:"role"`
	Description string   `json:"description"`
	Arc         string   `json:"arc"`
	Traits      []string `json:"traits"`
	Tier        string   `json:"tier,omitempty"` // core / important / secondary / decorative (mặc định important)
}

// VolumeOutline là dàn ý cấp quyển (chế độ phân tầng truyện dài).
type VolumeOutline struct {
	Index int          `json:"index"`
	Title string       `json:"title"`
	Theme string       `json:"theme"` // xung đột/chủ đề cốt lõi của quyển này
	Arcs  []ArcOutline `json:"arcs"`
}

// IsExpanded xác định quyển đã được mở rộng chưa (có cấu trúc cấp cung truyện).
func (v *VolumeOutline) IsExpanded() bool { return len(v.Arcs) > 0 }

// StoryCompass là la bàn hướng kết cục, thay cho danh sách quyển khung xương cố định.
// Architect có thể cập nhật ở mỗi ranh giới quyển, cho phép hướng truyện tiến hóa theo quá trình sáng tác.
type StoryCompass struct {
	EndingDirection string   `json:"ending_direction"`          // hướng kết cục (mô tả mang tính chủ đề)
	OpenThreads     []string `json:"open_threads,omitempty"`    // các tuyến dài đang mở (cần thu gọn mới kết thúc được)
	EstimatedScale  string   `json:"estimated_scale,omitempty"` // quy mô ước lượng (như "dự kiến 4-6 quyển")
	LastUpdated     int      `json:"last_updated,omitempty"`    // số chương đã hoàn thành tại thời điểm cập nhật
}

// ArcOutline là dàn ý cấp cung truyện.
type ArcOutline struct {
	Index             int            `json:"index"` // số thứ tự cung truyện trong quyển
	Title             string         `json:"title"`
	Goal              string         `json:"goal"`                         // mục tiêu cung truyện (khởi-thừa-chuyển-hợp)
	EstimatedChapters int            `json:"estimated_chapters,omitempty"` // số chương ước lượng của cung truyện khung xương (về 0 sau khi mở rộng)
	Chapters          []OutlineEntry `json:"chapters"`
}

// IsExpanded xác định cung truyện đã được mở rộng chưa (có chương chi tiết).
func (a *ArcOutline) IsExpanded() bool { return len(a.Chapters) > 0 }

// TotalChapters tính tổng số chương theo kế hoạch hiện tại của dàn ý phân tầng.
// Cung truyện đã mở rộng tính theo số chương thật, cung truyện khung xương tính theo EstimatedChapters.
// Progress.TotalChapters dùng nó để phán định chiến lược context truyện dài; chương thực sự viết được vẫn đến từ FlattenOutline.
func TotalChapters(volumes []VolumeOutline) int {
	n := 0
	for _, v := range volumes {
		for _, a := range v.Arcs {
			if a.IsExpanded() {
				n += len(a.Chapters)
			} else {
				n += a.EstimatedChapters
			}
		}
	}
	return n
}

// FlattenOutline trải dàn ý phân tầng thành danh sách chương phẳng, giữ số chương toàn cục liên tục.
func FlattenOutline(volumes []VolumeOutline) []OutlineEntry {
	var result []OutlineEntry
	ch := 1
	for _, v := range volumes {
		for _, a := range v.Arcs {
			for _, e := range a.Chapters {
				e.Chapter = ch
				result = append(result, e)
				ch++
			}
		}
	}
	return result
}

// WorldRule là mục quy tắc thế giới quan.
type WorldRule struct {
	Category string `json:"category"` // magic / technology / geography / society / other
	Rule     string `json:"rule"`     // mô tả quy tắc
	Boundary string `json:"boundary"` // ranh giới không thể vi phạm
}
