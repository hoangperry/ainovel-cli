package i18n

// Catalog tiếng Việt cho vùng bootstrap/setup + main (die/Errorf). Wave 2 / U2d.
// Prefix sở hữu: setup.*, error.config.* (trừ error.config.load_failed đã seed),
// main.* (trừ main.exit.press_enter/logged đã seed).
// Tự Register khối key của vùng mình, không sửa file catalog vùng khác.
//
// LƯU Ý %w: value chứa %w dùng qua fmt.Errorf(i18n.T(key), err) — KHÔNG dùng Tf.
// Thứ tự động từ (%d/%s/%q/%w) giữ nguyên như bản zh gốc; dấu câu full-width đã
// chuẩn hoá về ASCII.
func init() {
	Register(LangVI, map[string]string{
		// setup.* — luồng khởi tạo lần đầu (RunSetup)
		"setup.intro.detect":             "Chưa phát hiện tệp cấu hình, bắt đầu khởi tạo thiết lập...",
		"setup.intro.config_path":        "  Đường dẫn tệp cấu hình: %s\n",
		"setup.intro.edit_hint":          "  Hoàn tất xong có thể chỉnh tệp này bất cứ lúc nào để tinh chỉnh thiết lập nâng cao.\n",
		"setup.step.apikey_optional":     "[2/4] API Key (có thể để trống)",
		"setup.step.apikey_empty":        "để trống nghĩa là không dùng API Key",
		"setup.step.apikey":              "[2/4] API Key",
		"setup.step.baseurl":             "[3/4] Base URL (Enter để dùng mặc định, người dùng proxy điền địa chỉ proxy)",
		"setup.step.baseurl_hint":        "để trống dùng địa chỉ chính thức",
		"setup.step.model":               "[4/4] Tên model",
		"setup.step.model_hint":          "ví dụ: gpt-4o / claude-sonnet-4 / gemini-2.5-pro",
		"setup.value.apikey_unset":       "chưa đặt",
		"setup.value.baseurl_default":    "mặc định",
		"setup.prompt.provider_name":     "Tên Provider",
		"setup.label.provider_select":    "[1/4] Chọn Provider",
		"setup.label.api_type":           "Loại giao thức API",
		"setup.label.api_type_openai":    "Tương thích OpenAI",
		"setup.label.api_type_anthropic": "Tương thích Anthropic",
		"setup.label.api_type_gemini":    "Tương thích Gemini",
		"setup.saved":                    "%s Cấu hình đã lưu vào %s\n",
		"setup.saved.default_model":      "  Model mặc định: %s\n",
		"setup.saved.role_hint":          "  Nếu muốn cấu hình model khác nhau theo vai trò, chỉ cần chỉnh tệp cấu hình.",
		"setup.saved.rules_hint":         "  Sở thích viết toàn cục có thể đặt vào tệp .md trong %s (xem README.txt trong đó)\n",
		"setup.select.help":              "\n  ↑↓ chọn  Enter xác nhận  Esc huỷ",
		"setup.input.help":               "  (Enter xác nhận, Esc huỷ)",

		// error.config.* — lỗi cấu hình bọc bằng %w (dùng fmt.Errorf(T(key), err))
		"error.config.provider_missing_type": "provider %q thiếu type, và không nằm trong danh sách provider đã biết của litellm: %w",
		"error.config.provider_no_creds":     "provider %q chưa khai báo chứng thực trong providers; nếu bạn override provider trong ./.ainovel/config.json thì phải đồng thời khai báo providers.%s (gồm api_key/base_url), không thể chỉ đổi provider ở tầng trên: %w",

		// main.* — die()/Errorf trong cmd/ainovel-cli/main.go (hiện qua die)
		"main.die.headless_no_setup":        "error: chế độ headless không hỗ trợ khởi tạo lần đầu, hãy chạy TUI một lần để hoàn tất cấu hình",
		"main.die.no_cli_prompt":            "error: không còn hỗ trợ truyền yêu cầu truyện trực tiếp qua dòng lệnh, hãy khởi động rồi nhập vào ô nhập của TUI",
		"main.die.prompt_headless_only":     "error: --prompt/--prompt-file chỉ dùng được trong chế độ --headless",
		"main.err.read_prompt":              "đọc prompt thất bại: %w",
		"main.update.latest":                "ainovel-cli đã là phiên bản mới nhất %s\n",
		"main.update.updated":               "ainovel-cli đã cập nhật lên %s\n",
		"main.update.install_path":          "Vị trí cài đặt: %s\n",
		"main.flag.version_no_arg":          "version không nhận tham số",
		"main.flag.update_once":             "update chỉ được chỉ định một lần",
		"main.flag.update_one_arg":          "update chỉ nhận một tham số phiên bản tuỳ chọn",
		"main.flag.config_missing_val":      "--config thiếu giá trị",
		"main.flag.lang_missing_val":        "--lang thiếu giá trị",
		"main.flag.prompt_missing_val":      "--prompt thiếu giá trị",
		"main.flag.prompt_file_missing_val": "--prompt-file thiếu giá trị",
		"main.flag.prompt_conflict":         "--prompt và --prompt-file không thể dùng đồng thời",
		"main.flag.version_conflict":        "version không thể dùng chung với tham số khởi động khác",
		"main.flag.update_conflict":         "update không thể dùng chung với tham số khởi động khác",
	})
}
