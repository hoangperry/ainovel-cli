// Package contentlang chọn nội dung LLM-facing in-source (tool Description/Schema,
// prompt builder, status block...) theo ngôn ngữ asset đang dùng: "zh" hoặc "vi".
//
// Đây là trục NỘI DUNG gửi cho model, KHÁC với internal/i18n (UI locale của operator).
// Khớp với cơ chế locale-subdir của assets: output_lang=original → "zh", vi/en → "vi".
// Đặt một lần lúc khởi động (Set), sau khi đã biết output_lang; single-user CLI nên
// không cần truyền qua mọi call site.
package contentlang

// current là locale nội dung đang hoạt động; mặc định "vi" (primary).
var current = "vi"

// Set đặt locale nội dung. Chỉ nhận "zh" hoặc "vi"; giá trị khác bị bỏ qua.
// Không an toàn cho goroutine theo thiết kế (đặt một lần lúc khởi động).
func Set(loc string) {
	if loc == "zh" || loc == "vi" {
		current = loc
	}
}

// Current trả về locale nội dung đang hoạt động.
func Current() string { return current }

// Pick trả về bản zh hoặc vi theo locale hiện tại. Dùng inline tại mỗi chuỗi Track C:
//
//	tool.Description() → contentlang.Pick("提交章节终稿...", "Commit bản cuối chương...")
func Pick(zh, vi string) string {
	if current == "zh" {
		return zh
	}
	return vi
}
