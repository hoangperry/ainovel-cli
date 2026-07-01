package i18n

// Catalog tiếng Việt cho vùng host/tools/notify. Wave 2 / U2b.
// Prefix sở hữu: error.host.*, error.tool.*, error.export.*, notify.*
// Tự Register khối key của vùng mình, không sửa file catalog vùng khác.
//
// LƯU Ý %w: các value chứa %w dùng qua fmt.Errorf(i18n.T(key), err) — KHÔNG dùng
// Tf. Thứ tự động từ (%d/%s/%w) giữ nguyên như bản zh gốc; dấu câu full-width đã
// chuẩn hoá về ASCII.
func init() {
	Register(LangVI, map[string]string{
		// notify.* — thông báo ngoài lề (title/body) + chẩn đoán kênh notify
		"notify.title.budget":            "ainovel: ngân sách",
		"notify.title.repeat":            "ainovel: lặp chỉ thị",
		"notify.title.run_done":          "ainovel: hoàn tất sáng tác",
		"notify.title.run_stopped":       "ainovel: dừng sáng tác",
		"notify.budget.blind":            "Vùng mù ngân sách: model không trả về dữ liệu usage, thống kê chi phí bằng 0, ngưỡng ngân sách sẽ không kích hoạt (model tuỳ chỉnh hãy kiểm tra giá trong registry hoặc include_usage phía upstream)",
		"notify.repeat.body":             "Cùng một chỉ thị đã được phát lần thứ %d (%s): %s",
		"notify.repeat.event_prefix":     "Lặp chỉ thị: ",
		"notify.run_end.novel_prefix":    "《%s》",
		"notify.run_end.cost_suffix":     " · chi phí $%.2f",
		"notify.run_end.done_summary":    "Hoàn tất sáng tác: %d chương %d chữ",
		"notify.run_end.stopped_summary": "Coordinator dừng (đã hoàn thành %d chương)",
		"notify.deliver_failed":          "gửi thông báo thất bại",
		"notify.degraded_no_send":        "thông báo hạ cấp về log (không có notify-send)",
		"notify.degraded_no_channel":     "thông báo hạ cấp về log (nền tảng không có kênh system)",

		// error.export.* — lỗi xuất bản (exp/exporter.go)
		"error.export.load_progress":    "tải progress thất bại: %w",
		"error.export.no_completed":     "chưa có chương nào hoàn thành, không có nội dung để xuất",
		"error.export.range_invalid":    "khoảng chương không hợp lệ: from=%d > to=%d",
		"error.export.range_empty":      "khoảng %d..%d không có chương nào đã hoàn thành",
		"error.export.read_chapter":     "đọc chương %d thất bại: %w",
		"error.export.chapter_missing":  "progress đánh dấu chương %d đã hoàn thành, nhưng chapters/%02d.md thiếu hoặc rỗng",
		"error.export.file_exists":      "tệp đã tồn tại: %s (thêm --overwrite để ghi đè)",
		"error.export.stat_outpath":     "kiểm tra đường dẫn xuất thất bại: %w",
		"error.export.render_epub":      "render EPUB thất bại: %w",
		"error.export.write_failed":     "ghi tệp thất bại: %w",
		"error.export.format_unsupport": "exp: định dạng chưa hỗ trợ %q",
		"error.export.infer_extension":  "không suy ra được định dạng từ đuôi mở rộng %q (hỗ trợ .txt / .epub)",

		// error.tool.* — lỗi công cụ trả về cho LLM và hiện trong TUI
		// ask_user
		"error.tool.ask.need_question":   "cần ít nhất một câu hỏi",
		"error.tool.ask.too_many":        "tối đa 4 câu hỏi, hiện có %d",
		"error.tool.ask.q_text_empty":    "câu hỏi %d: nội dung câu hỏi không được rỗng",
		"error.tool.ask.q_header_empty":  "câu hỏi %d: header không được rỗng",
		"error.tool.ask.q_header_long":   "câu hỏi %d: header %q vượt quá 12 ký tự",
		"error.tool.ask.q_opt_count":     "câu hỏi %d: cần 2-4 lựa chọn, hiện có %d",
		"error.tool.ask.opt_label_empty": "câu hỏi %d lựa chọn %d: label không được rỗng",
		"error.tool.ask.opt_desc_empty":  "câu hỏi %d lựa chọn %d: description không được rỗng",
		// edit_chapter
		"error.tool.edit.old_empty":   "old_string không được rỗng: %w",
		"error.tool.edit.old_eq_new":  "old_string trùng với new_string, không cần sửa: %w",
		"error.tool.edit.done_locked": "chương %d đã hoàn thành và không nằm trong hàng đợi PendingRewrites, không thể sửa; muốn sửa hãy để editor rà soát kích hoạt viết lại/đánh bóng trước: %w",
		"error.tool.edit.no_draft":    "chương %d không có bản nháp lẫn bản chốt, hãy gọi draft_chapter(mode=write, chapter=%d) tạo bản đầu trước: %w",
		// commit_chapter
		"error.tool.commit.pending_unrecovered": "tồn tại commit chương chưa khôi phục: chương %d (giai đoạn %s), hãy khôi phục hoặc commit lại chương đó trước: %w",
		"error.tool.commit.not_allowed":         "chương hiện chưa được phép commit: %w: %w",
		"error.tool.commit.arc_boundary_failed": "phát hiện ranh giới cung truyện thất bại chapter=%d: %w: %w",
		"error.tool.commit.no_change":           "chương %d nội dung drafts và chapters hoàn toàn giống nhau, không phát hiện thay đổi %s. Hãy gọi draft_chapter(mode=write, chapter=%d) ghi bản chính mới sau %s rồi commit_chapter: %w",
		"error.tool.commit.mode_rewrite":        "viết lại",
		"error.tool.commit.mode_polish":         "đánh bóng",
		// save_foundation
		"error.tool.foundation.book_complete":  "toàn bộ sách đã kết thúc (phase=complete), không cho phép thêm quyển mới: %w",
		"error.tool.foundation.no_progress":    "progress chưa được khởi tạo: %w",
		"error.tool.foundation.not_writing":    "complete_book chỉ gọi được ở giai đoạn writing (hiện phase=%s): %w",
		"error.tool.foundation.rework_pending": "còn %d chương trong hàng đợi làm lại, xử lý xong rồi gọi complete_book: %w",
		// reopen_book
		"error.tool.reopen.chapters_empty": "chapters không được rỗng, cần chỉ rõ chương cần làm lại: %w",
		"error.tool.reopen.no_progress":    "progress chưa được khởi tạo: %w",
		"error.tool.reopen.not_done":       "chương %v chưa viết xong, reopen chỉ làm lại chương đã hoàn thành (thêm/mở rộng tình tiết hãy đi qua điều chỉnh độ dài): %w",
		// save_directive
		"error.tool.directive.add_need_text":   "add cần text không rỗng: %w",
		"error.tool.directive.remove_need_idx": "remove cần index >= 1: %w",
	})
}
