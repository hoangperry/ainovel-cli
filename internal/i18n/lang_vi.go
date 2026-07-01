package i18n

// Catalog tiếng Việt (primary). Wave 2 bổ sung các file cat_<area>_vi.go riêng;
// mỗi file tự Register khối key của vùng mình, không sửa file này, tránh tranh chấp.
func init() {
	Register(LangVI, map[string]string{
		// main.* — vòng đời khởi động / thoát
		"main.exit.press_enter": "\nNhấn Enter để thoát...",
		"main.exit.logged":      "(chi tiết lỗi đã ghi vào %s)\n",

		// error.config.* — lỗi cấu hình bọc bằng %w (dùng fmt.Errorf(T(key), err))
		"error.config.load_failed": "tải cấu hình thất bại: %w",
	})
}
