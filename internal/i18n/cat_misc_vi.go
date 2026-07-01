package i18n

// Catalog tiếng Việt cho các package nhỏ. Wave 2 / U2e.
// Prefix sở hữu: domain.*, store.*, rules.*, models.*, startup.*
// Tự Register khối key của vùng mình, không sửa file catalog vùng khác.
//
// LƯU Ý %w: value chứa %w dùng qua fmt.Errorf(i18n.T(key), err) — KHÔNG dùng Tf.
// Thứ tự động từ (%d/%s/%q/%v/%w) giữ nguyên như bản zh gốc; dấu câu full-width
// （）：đã chuẩn hoá về ASCII.
func init() {
	Register(LangVI, map[string]string{
		// store.* — lỗi tool + cảnh báo nhất quán (hiện cho operator)
		"store.progress.not_initialized":        "progress chưa khởi tạo: %w",
		"store.progress.reopen_only_complete":   "reopen chỉ áp dụng cho sách đã hoàn thành (phase hiện tại=%s): %w",
		"store.progress.rewrite_verb":           "viết lại",
		"store.progress.polish_verb":            "trau chuốt",
		"store.progress.chapter_not_in_queue":   "chương %d không nằm trong hàng đợi chờ %s, hàng đợi hiện tại: %v. Hãy xử lý các chương trong hàng đợi trước, rồi mới động đến chương mới: %w",
		"store.progress.pending_completed_only": "pending_rewrites chỉ được chứa các chương đã hoàn thành, chương không hợp lệ: %v, completed_chapters=%v: %w",
		"store.directives.limit_reached":        "chỉ thị lâu dài đã đạt giới hạn %d mục, hãy dùng remove để xoá hoặc gộp các yêu cầu cũ trước khi thêm",
		"store.directives.index_out_of_range":   "số thứ tự %d ngoài phạm vi (hiện có tổng cộng %d mục)",
		"store.consistency.chapter_missing":     "progress đánh dấu chương %d đã hoàn thành, nhưng chapters/%02d.md không tồn tại hoặc rỗng",
		"store.consistency.va_not_found":        "progress hiện tại V%d A%d không tìm thấy mục tương ứng trong dàn ý phân tầng",
		"store.outline.volume_index_order":      "volume Index %d phải lớn hơn giá trị lớn nhất hiện có %d",
		"store.outline.volume_needs_arc":        "quyển mới phải chứa ít nhất một cung truyện",
		"store.outline.first_arc_detail":        "cung truyện đầu của quyển mới phải chứa chương chi tiết",
		"store.outline.ending_required":         "ending_direction không được để trống",

		// rules.* — Conflict.Detail (chẩn đoán hiện trên panel /diag của operator)
		"rules.parse.yaml_failed":           "phân tích front matter YAML thất bại: %v",
		"rules.parse.unknown_field":         "trường không xác định %q, Phase 1 không hỗ trợ; đã bỏ qua",
		"rules.parse.chapter_words_format":  "chapter_words mong đợi khoảng \"min-max\" (như 3000-6000) hoặc một giá trị mục tiêu (như 2500), nhận được %v",
		"rules.parse.fatigue_blank_key":     "fatigue_words xuất hiện key trống, đã bỏ qua",
		"rules.parse.fatigue_int_expected":  "fatigue_words[%q] mong đợi ngưỡng int, nhận được %T (%v); đã loại bỏ key này",
		"rules.parse.fatigue_threshold_pos": "fatigue_words[%q] ngưỡng phải > 0, nhận được %d; đã loại bỏ key này",
		"rules.parse.fatigue_list_string":   "phần tử danh sách fatigue_words mong đợi string, nhận được %T (%v); đã loại bỏ phần tử này",
		"rules.parse.fatigue_list_blank":    "phần tử danh sách fatigue_words là khoảng trắng; đã loại bỏ",
		"rules.parse.list_string_expected":  "phần tử danh sách %s mong đợi string, nhận được %T (%v); đã loại bỏ phần tử này",
		"rules.parse.type_error":            "trường %s sai kiểu, mong đợi %s, nhận được %T (%v); đã loại bỏ",
		"rules.type.map_or_list":            "map[string]int hoặc []string",
		"rules.merge.fatigue_conflict":      "trường fatigue_words[%q] được khai báo ở nhiều nguồn và ngưỡng không nhất quán: %s; ưu tiên nguồn gần nhất: %s",
		"rules.merge.field_conflict":        "trường %s được khai báo ở nhiều nguồn và giá trị không nhất quán: %s; ưu tiên nguồn gần nhất: %s",
		"rules.load.read_failed":            "đọc thất bại: ",
		"rules.load.dir_read_failed":        "đọc thư mục quy tắc thất bại: ",

		// models.* — slog chẩn đoán
		"models.refresh_failed": "làm mới metadata model thất bại",
		"models.ready":          "metadata model đã sẵn sàng",

		// startup.* — nhãn chế độ khởi động
		"startup.mode.quick":    "Bắt đầu nhanh",
		"startup.mode.cocreate": "Lập kế hoạch đồng sáng tạo",
	})
}
