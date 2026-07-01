package i18n

// Catalog tiếng Việt cho nhãn tool (Label) và mô tả hoạt động (ActivityDescription)
// hiển thị trên TUI. Straggler S1-tool-labels.
// Prefix sở hữu: ui.tool.*
// Chỉ chứa chuỗi hiện cho operator; KHÔNG đụng tới Description()/Schema() (LLM-facing, Wave 5).
func init() {
	Register(LangVI, map[string]string{
		"ui.tool.check_consistency.label":   "kiểm tra nhất quán",
		"ui.tool.ask_user.label":            "hỏi người dùng",
		"ui.tool.commit_chapter.label":      "commit chương",
		"ui.tool.draft_chapter.label":       "viết chương",
		"ui.tool.edit_chapter.label":        "sửa chương",
		"ui.tool.edit_chapter.activity":     "sửa bản nháp chương",
		"ui.tool.context.label":             "nạp context",
		"ui.tool.plan_chapter.label":        "lên kế hoạch chương",
		"ui.tool.reopen_book.label":         "mở lại làm lại",
		"ui.tool.reopen_book.activity":      "mở lại toàn bộ sách để làm lại",
		"ui.tool.read_chapter.label":        "đọc chương",
		"ui.tool.save_directive.label":      "lưu chỉ thị lâu dài",
		"ui.tool.save_directive.activity":   "đang lưu chỉ thị lâu dài",
		"ui.tool.save_review.label":         "lưu rà soát",
		"ui.tool.save_arc_summary.label":    "lưu tóm tắt cung truyện",
		"ui.tool.save_volume_summary.label": "lưu tóm tắt quyển",
		"ui.tool.save_foundation.label":     "lưu thiết lập",
	})
}
