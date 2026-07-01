package i18n

// Catalog tiếng Việt cho thông báo CLI operator (self-update, lỗi cấu hình mức CLI).
func init() {
	Register(LangVI, map[string]string{
		"cli.update.windows_unsupported":  "Windows không hỗ trợ tự cập nhật tại chỗ, hãy vào https://github.com/%s/releases để tải bản mới",
		"cli.update.release_no_tag":       "release thiếu tag_name",
		"cli.update.no_asset":             "release %s không tìm thấy gói cài cho nền tảng hiện tại *%s",
		"cli.update.unsupported_os":       "hệ điều hành không hỗ trợ %s",
		"cli.update.unsupported_arch":     "kiến trúc không hỗ trợ %s",
		"cli.update.binary_not_found":     "không tìm thấy %s trong gói cài",
		"cli.update.locate_exe_failed":    "không thể định vị file thực thi hiện tại",
		"cli.config.project_parse_failed": "phân tích cấu hình mức project ./.ainovel/config.json thất bại (kiểm tra cú pháp JSON): %w",
		"cli.config.switch_model_failed":  "chuyển model thất bại: %w",
		"cli.config.provider_type_failed": "phân giải loại provider thất bại: %w",
	})
}
