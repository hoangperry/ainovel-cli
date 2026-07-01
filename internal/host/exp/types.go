// Package exp hiện thực khả năng export các chương đã hoàn thành.
//
// Đối xứng với imp/: thuần IO cục bộ, không phụ thuộc LLM, không đổi trạng thái store. Export có thể chạy
// đồng thời với Coordinator (chỉ đọc Progress + bản cuối của chương), thuộc khả năng ngang.
//
// Phiên bản đầu chỉ hỗ trợ TXT; EPUB để dành vòng sau.
package exp

import "github.com/voocel/ainovel-cli/internal/store"

// Format định danh định dạng export.
type Format string

const (
	// FormatTXT output văn bản thuần.
	FormatTXT Format = "txt"
	// FormatEPUB container EPUB 3 chuẩn (zip + xhtml).
	FormatEPUB Format = "epub"
)

// Options điều khiển hành vi export. zero-value tương đương "export toàn bộ ra đường dẫn mặc định, file đã tồn tại thì báo lỗi".
//
// Bố cục: tên sách → phân tách quyển → nội dung chương. Hai loại dữ liệu nội bộ không vào export: premise (bản thiết kế sáng tác,
// chứa độc giả mục tiêu / điểm tiêu thụ cốt lõi / vùng cấm khi viết và các metadata hậu trường, dành cho tác giả và engine, không phải lời tựa cho độc giả);
// phân tách cung truyện (dưới góc nhìn độc giả, cung truyện là cấu trúc nội bộ quá chi tiết). Tên sách và phân tách quyển luôn được giữ lại.
type Options struct {
	// Format khi là chuỗi rỗng thì suy ra từ phần mở rộng của OutPath (.txt → TXT, .epub → EPUB);
	// OutPath cũng rỗng thì quay về FormatTXT. Caller SDK có thể chỉ định tường minh để bỏ qua việc suy ra.
	Format Format

	// OutPath đường dẫn file output; rỗng nghĩa là {novelDir}/{NovelName}.{ext},
	// ext do Format quyết định (NovelName rỗng thì dùng tên thư mục).
	OutPath string

	// From / To phạm vi chương, đoạn đóng. 0 nghĩa là từ chương 1 / đến chương cuối.
	// Các chương chưa hoàn thành trong phạm vi sẽ bị bỏ qua và ghi vào Result.Skipped, không coi là lỗi.
	From, To int

	// Overwrite file đã tồn tại thì có ghi đè hay không; mặc định từ chối.
	Overwrite bool
}

// Deps là các dependency mà Run cần. Chỉ store; export không cần LLM, prompt, bundle.
type Deps struct {
	Store *store.Store
}

// Result là bản tóm tắt sản phẩm của một lần export thành công.
type Result struct {
	// Path đường dẫn file thực sự được ghi (tuyệt đối hoặc tương đối do caller truyền vào).
	Path string
	// Chapters số chương thực sự được ghi.
	Chapters int
	// Bytes số byte của file (UTF-8).
	Bytes int
	// Skipped số hiệu các chương nằm trong phạm vi yêu cầu nhưng chưa hoàn thành.
	Skipped []int
}
