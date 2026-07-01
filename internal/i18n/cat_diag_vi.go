package i18n

// Catalog tiếng Việt cho vùng diag (chẩn đoán chất lượng + quy trình). Wave 2 / U2c.
// Prefix sở hữu: diag.quality.*, diag.flow.*
// Mỗi file tự Register khối key của vùng mình, không sửa file catalog vùng khác.
//
// Các value là fmt template cho Tf (giữ nguyên thứ tự động từ %d/%s/%.0f/%.1f như
// bản zh gốc). Dấu câu full-width đã chuẩn hoá về ASCII. Token cố định mà code/test
// pattern-match (ch%d, payoff, tỉ lệ %) giữ ổn định.
func init() {
	Register(LangVI, map[string]string{
		// diag.quality.* — ChronicLowDimension
		"diag.quality.chronic_low_dim.title":      "Chiều [%s] liên tục điểm thấp (trung bình %.0f)",
		"diag.quality.chronic_low_dim.evidence":   "Tổng %d lần rà soát, điểm trung bình %.1f",
		"diag.quality.chronic_low_dim.suggestion": "Kiểm tra chỉ dẫn về %s trong prompt Writer có rõ ràng không, hoặc tiêu chí chấm %s trong prompt Editor có hợp lý không.",

		// diag.quality.* — ContractMissPattern
		"diag.quality.contract_miss.title":      "Tỉ lệ thực thi contract thấp (%.0f%% không đạt)",
		"diag.quality.contract_miss.evidence":   "Không đạt: [%s], tổng %d/%d",
		"diag.quality.contract_miss.suggestion": "Writer có thể chưa đọc contract, hoặc required_beats của contract quá tham vọng. Kiểm tra sự phối hợp giữa plan_chapter và writer.md.",

		// diag.quality.* — HookWeakChain
		"diag.quality.hook_weak.title":      "Hook cuối chương liên tục yếu (liên tục %d chương)",
		"diag.quality.hook_weak.suggestion": "Kiểm tra việc thực thi hook_goal trong writer.md có rõ ràng không, nếu cần thì nêu rõ ham muốn đọc tiếp của chương trong plan_chapter, và hiệu chỉnh tiêu chí dẫn chứng của Editor với hook.",

		// diag.quality.* — PayoffMissPattern
		"diag.quality.payoff_miss.title":      "Tỉ lệ thực hiện điểm cao trào/tình tiết thấp (%.0f%% không đạt)",
		"diag.quality.payoff_miss.evidence":   "Chương chưa thực hiện: [%s], tổng %d/%d",
		"diag.quality.payoff_miss.detail":     "ch%d(%d payoff)",
		"diag.quality.payoff_miss.suggestion": "Kiểm tra payoff_points của plan_chapter có quá nhiều hoặc quá rỗng không, đảm bảo Writer thực hiện rõ ràng trong nội dung chứ không chỉ cài cắm.",

		// diag.quality.* — ExcessiveRewrites
		"diag.quality.excessive_rewrites.title":      "Tỉ lệ làm lại quá cao (%d/%d = %.0f%%)",
		"diag.quality.excessive_rewrites.evidence":   "Tổng %d lần rà soát, %d lần rewrite",
		"diag.quality.excessive_rewrites.suggestion": "Writer liên tục cho ra nội dung dưới ngưỡng của Editor. Kiểm tra tiêu chí chất lượng trong prompt Writer có khớp với tiêu chí rà soát của Editor không.",

		// diag.quality.* — WordCountAnomaly
		"diag.quality.word_anomaly.title":      "Số chữ chương bất thường (trung bình %d chữ)",
		"diag.quality.word_anomaly.detail":     "ch%d(%d chữ,%.0f%%)",
		"diag.quality.word_anomaly.suggestion": "Chương quá ngắn có thể do output bị cắt (giới hạn token), chương quá dài có thể tiêu hao quá nhiều context window. Kiểm tra cấu hình max_tokens của model.",

		// diag.flow.* — InvalidPendingRewrites
		"diag.flow.invalid_pending.title":      "Hàng đợi làm lại chứa chương chưa hoàn thành: [%s]",
		"diag.flow.invalid_pending.evidence":   "pending_rewrites=[%s], completed_chapters=[%s], flow=%s",
		"diag.flow.invalid_pending.suggestion": "Đây là bất biến trạng thái bị hỏng. Hãy dừng chạy rồi sửa meta/progress.json, loại các chương chưa hoàn thành khỏi pending_rewrites; nếu hàng đợi rỗng, đổi flow thành writing và xoá rewrite_reason.",

		// diag.flow.* — RewritePendingPressure
		"diag.flow.rewrite_pending.title":      "Chương chờ làm lại: [%s]",
		"diag.flow.rewrite_pending.evidence":   "flow=%s, pending_rewrites=[%s]",
		"diag.flow.rewrite_pending.suggestion": "Kiểm tra tiêu chí rà soát của Editor có quá khắt khe không, hoặc prompt làm lại của Writer có hiệu quả không. Nếu cần can thiệp thủ công, hãy gửi chỉ thị can thiệp ở ô nhập.",

		// diag.flow.* — OrphanedSteer
		"diag.flow.orphaned_steer.title":      "Tồn tại chỉ thị chuyển hướng chưa được tiêu thụ",
		"diag.flow.orphaned_steer.evidence":   "pending_steer=%q, flow=%s",
		"diag.flow.orphaned_steer.suggestion": "Lệnh steer này được lưu lại nhưng chưa được Coordinator tiêu thụ. Kiểm tra logic khôi phục sau gián đoạn, hoặc ghi đè bằng cách gửi lại.",

		// diag.flow.* — PhaseFlowMismatch
		"diag.flow.phase_mismatch.title":      "Trạng thái giai đoạn/quy trình không khớp: phase=%s, flow=%s",
		"diag.flow.phase_mismatch.evidence":   "phase=%s không nên xuất hiện flow=%s khác giá trị khởi đầu",
		"diag.flow.phase_mismatch.suggestion": "State machine có thể bị hỏng, cần kiểm tra thủ công trường phase và flow của meta/progress.json.",

		// diag.flow.* — ChapterGaps
		"diag.flow.chapter_gaps.title":      "Chương nhảy số: thiếu [%s]",
		"diag.flow.chapter_gaps.evidence":   "completed=[%s]",
		"diag.flow.chapter_gaps.suggestion": "commit_chapter có thể bị gián đoạn giữa chừng. Kiểm tra meta/pending_commit.json có commit chưa hoàn thành không.",
	})
}
