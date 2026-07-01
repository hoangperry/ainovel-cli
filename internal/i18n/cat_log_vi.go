package i18n

// Catalog tiếng Việt cho các thông điệp slog hiện cho operator. Wave 2 / U2 S2.
// Prefix sở hữu: log.*
// Các message slog là tĩnh (không có verb fmt) nên dùng qua i18n.T(key).
// Attr key/value có cấu trúc giữ nguyên (đã là định danh tiếng Anh).
func init() {
	Register(LangVI, map[string]string{
		// log.host.* — vòng đời host/boot
		"log.host.starting":             "khởi động",
		"log.host.models_ready":         "model đã sẵn sàng",
		"log.host.run_meta_init_failed": "khởi tạo metadata phiên chạy thất bại",
		"log.host.create_start":         "bắt đầu sáng tác",
		"log.host.create_resume":        "tiếp tục sáng tác",
		"log.host.consistency_warning":  "cảnh báo nhất quán",
		"log.host.steer_inject_failed":  "chèn steer thất bại",
		"log.host.save_config_failed":   "lưu cấu hình thất bại",

		// log.usage.* — kế toán usage/chi phí
		"log.usage.load_failed_replay":       "tải usage thất bại, sẽ thử bù từ sessions",
		"log.usage.replay_failed":            "replay usage thất bại",
		"log.usage.replay_done":              "bù usage từ session hoàn tất",
		"log.usage.replay_save_failed":       "lưu usage sau khi bù thất bại",
		"log.usage.flush_before_exit_failed": "ghi usage xuống đĩa trước khi thoát thất bại",
		"log.usage.save_failed":              "lưu usage xuống đĩa thất bại",
		"log.usage.missing_usage_data":       "phản hồi LLM không kèm dữ liệu usage, bảng cache/chi phí sẽ không cộng dồn — thường do streaming phía trên không gửi final usage chunk theo giao thức OpenAI include_usage",

		// log.reminder.* — stop guard / subagent guard
		"log.reminder.subagent_unrecoverable_escalate": "subagent stop_guard phát hiện dừng không thể khôi phục, nâng cấp ngay",
		"log.reminder.subagent_block_limit_escalate":   "subagent stop_guard chặn liên tiếp vượt ngưỡng, nâng cấp thành kết thúc",
		"log.reminder.subagent_block_end_turn":         "subagent stop_guard chặn end_turn",
		"log.reminder.block_limit_escalate":            "stop_guard chặn liên tiếp vượt ngưỡng, nâng cấp thành kết thúc",
		"log.reminder.block_end_turn":                  "stop_guard chặn end_turn",

		// log.config.* — bootstrap config / cửa sổ ngữ cảnh
		"log.config.role_model_assign":           "phân bổ model theo vai trò",
		"log.config.model_unrecognized_fallback": "model không nhận diện được, dùng cửa sổ dự phòng (proxy tuỳ chỉnh hoặc OpenRouter chưa thu thập, có thể chỉ định rõ qua context_window)",
		"log.config.context_window_from_config":  "cửa sổ ngữ cảnh (từ context_window trong file cấu hình)",
		"log.config.context_window":              "cửa sổ ngữ cảnh",
		"log.config.global_parse_failed":         "phân tích cấu hình toàn cục thất bại, đã bỏ qua (có thể bị cấp project/--config ghi đè)",

		// log.agent.* — dựng agent / nạp quy tắc
		"log.agent.context_rewrite":         "viết lại ngữ cảnh",
		"log.agent.rules_loaded":            "nạp quy tắc",
		"log.agent.invalid_thinking_config": "bỏ qua cấu hình cường độ suy nghĩ không hợp lệ",
		"log.agent.provider_switch":         "chuyển provider",

		// log.tool.* — công cụ chương
		"log.tool.cast_accumulate_failed":     "tích luỹ danh sách nhân vật phụ thất bại, bỏ qua",
		"log.tool.final_empty_fallback_draft": "read_chapter đọc bản hoàn thiện rỗng, quay về bản nháp",
	})
}
