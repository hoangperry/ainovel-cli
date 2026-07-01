package i18n

// English catalog for operator-facing slog messages. Wave 2 / U2 S2.
// Mirrors cat_log_vi.go key-for-key. Vietnamese is primary; this is the mirror.
// slog messages are static (no fmt verbs); used via i18n.T(key).
func init() {
	Register(LangEN, map[string]string{
		// log.host.* — host/boot lifecycle
		"log.host.starting":             "starting",
		"log.host.models_ready":         "models ready",
		"log.host.run_meta_init_failed": "failed to init run metadata",
		"log.host.create_start":         "creation started",
		"log.host.create_resume":        "creation resumed",
		"log.host.consistency_warning":  "consistency warning",
		"log.host.steer_inject_failed":  "steer inject failed",
		"log.host.save_config_failed":   "failed to save config",

		// log.usage.* — usage/cost accounting
		"log.usage.load_failed_replay":       "usage load failed, will try to replay from sessions",
		"log.usage.replay_failed":            "usage replay failed",
		"log.usage.replay_done":              "usage replay from session complete",
		"log.usage.replay_save_failed":       "saving usage after replay failed",
		"log.usage.flush_before_exit_failed": "flushing usage to disk before exit failed",
		"log.usage.save_failed":              "usage flush to disk failed",
		"log.usage.missing_usage_data":       "LLM response carried no usage data, cache/cost panels will not accumulate — usually the upstream streaming did not emit a final usage chunk per the OpenAI include_usage protocol",

		// log.reminder.* — stop guard / subagent guard
		"log.reminder.subagent_unrecoverable_escalate": "subagent stop_guard detected an unrecoverable stop, escalating immediately",
		"log.reminder.subagent_block_limit_escalate":   "subagent stop_guard consecutive blocks exceeded the limit, escalating to terminate",
		"log.reminder.subagent_block_end_turn":         "subagent stop_guard blocked end_turn",
		"log.reminder.block_limit_escalate":            "stop_guard consecutive blocks exceeded the limit, escalating to terminate",
		"log.reminder.block_end_turn":                  "stop_guard blocked end_turn",

		// log.config.* — bootstrap config / context window
		"log.config.role_model_assign":           "role model assignment",
		"log.config.model_unrecognized_fallback": "unrecognized model, using fallback window (custom proxy or OpenRouter not catalogued, set context_window explicitly)",
		"log.config.context_window_from_config":  "context window (from context_window in config file)",
		"log.config.context_window":              "context window",
		"log.config.global_parse_failed":         "global config parse failed, ignored (can be overridden by project-level/--config)",

		// log.agent.* — agent build / rules load
		"log.agent.context_rewrite":         "context rewrite",
		"log.agent.rules_loaded":            "rules loaded",
		"log.agent.invalid_thinking_config": "ignoring invalid thinking-effort config",
		"log.agent.provider_switch":         "provider switch",

		// log.tool.* — chapter tools
		"log.tool.cast_accumulate_failed":     "supporting-cast roster accumulation failed, skipped",
		"log.tool.final_empty_fallback_draft": "read_chapter read an empty final draft, falling back to draft",
	})
}
