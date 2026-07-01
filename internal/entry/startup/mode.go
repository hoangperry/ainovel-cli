package startup

import "fmt"

// Lớp startup đảm nhận việc điều phối khởi động "trước khi vào Engine".
// Quy ước phân lớp:
// 1. entry/tui, entry/headless là entry của host;
// 2. startup lo các chiến lược khởi động như nhanh/đồng sáng tác/viết tiếp;
// 3. orchestrator.Engine chỉ lo thực thi phiên chính thức, không lo chuẩn bị trước chế độ.

// Mode biểu thị loại chiến lược khởi động trước khi vào Engine.
type Mode string

const (
	// ModeQuick lấy trực tiếp input người dùng làm điểm khởi đầu sáng tác.
	ModeQuick Mode = "quick"
	// ModeCoCreate làm rõ qua nhiều vòng trước, rồi sản bản nháp sáng tác để vào Engine.
	ModeCoCreate Mode = "cocreate"
	// ModeContinueFromNovel lắp ghép context dựa trên nội dung tiểu thuyết có sẵn rồi viết tiếp.
	ModeContinueFromNovel Mode = "continue_from_novel"
)

// Request mô tả input thô mà lớp entry gửi cho lớp chiến lược khởi động.
// Entry của host thu thập input người dùng trước, rồi startup chỉnh nó thành kế hoạch có thể vào Engine.
type Request struct {
	Mode        Mode
	UserPrompt  string
	NovelPath   string
	OutputDir   string
	Interactive bool
}

// Plan mô tả kết quả mà lớp chiến lược khởi động sản ra.
// Entry của host không nên tự ghép prompt khởi động chính thức, mà nên tiêu thụ Plan rồi điều khiển Engine.
type Plan struct {
	Mode        Mode
	DisplayName string
	StartPrompt string
	ResumeOnly  bool
}

// ErrNotImplemented đánh dấu chiến lược placeholder chưa được triển khai.
var ErrNotImplemented = fmt.Errorf("startup mode not implemented")

// PrepareContinueFromNovel là điểm dự trữ thống nhất cho "viết tiếp dựa trên tiểu thuyết có sẵn".
// TUI/headless sau này đều nên chỉnh input vào Request trước, rồi từ đây sản ra Plan có thể vào Engine.
func PrepareContinueFromNovel(req Request) (Plan, error) {
	return Plan{}, fmt.Errorf("%w: %s", ErrNotImplemented, ModeContinueFromNovel)
}
