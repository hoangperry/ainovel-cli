// Package imp hiện thực việc import và suy ngược các chương tiểu thuyết bên ngoài.
//
// Ý tưởng cốt lõi: dùng LLM suy ngược foundation + sự kiện từng chương, tái dùng bộ ba nguyên tử của các tool
// save_foundation / commit_chapter hiện có để ghi xuống. Sau khi import xong trạng thái store tương đương "viết xong N chương
// rồi sập", caller gọi host.Resume() là viết tiếp liền mạch.
//
// Không qua Coordinator: import là phát lại xác định, không thuộc phạm vi quyết định của LLM; để Coordinator
// can dự chỉ đưa thêm tính bất định. Package này gọi trực tiếp client LLM + gọi tool.
package imp

import "time"

// Chapter là một chương đơn lẻ sau khi cắt.
type Chapter struct {
	Title   string
	Content string
}

// Options điều khiển hành vi import.
type Options struct {
	// SourcePath bắt buộc. Đường dẫn một file txt/md đơn.
	SourcePath string

	// ResumeFrom tùy chọn. Import từ chương N; 0 / 1 nghĩa là từ đầu.
	// Nếu > 1, sẽ bỏ qua việc suy ngược Foundation (coi như đã ghi xuống).
	ResumeFrom int
}

// Stage biểu thị giai đoạn hiện tại của luồng import.
type Stage string

const (
	StageSplitting  Stage = "splitting"
	StageFoundation Stage = "foundation"
	StageChapter    Stage = "chapter"
	StageDone       Stage = "done"
	StageError      Stage = "error"
)

// Event là sự kiện tiến độ mà luồng import phát ra bên ngoài.
type Event struct {
	Time    time.Time
	Stage   Stage
	Current int    // số chương hiện tại của giai đoạn chapter; các giai đoạn khác là 0
	Total   int    // tổng số chương
	Message string // mô tả con người đọc được
	Err     error  // mang theo khi StageError
}
