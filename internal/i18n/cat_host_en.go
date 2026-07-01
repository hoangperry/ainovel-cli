package i18n

// English catalog for the host/tools/notify area. Mirror of cat_host_vi.go —
// every key here must exist in vi and vice versa (enforced by TestCatalogParity),
// with matching fmt verbs. Wave 2 / U2b.
func init() {
	Register(LangEN, map[string]string{
		// notify.* — off-screen alerts (title/body) + notify-channel diagnostics
		"notify.title.budget":            "ainovel: budget",
		"notify.title.repeat":            "ainovel: repeated directive",
		"notify.title.run_done":          "ainovel: writing done",
		"notify.title.run_stopped":       "ainovel: writing stopped",
		"notify.budget.blind":            "Budget blind spot: model returned no usage data, cost is counted as 0, the budget cap will never trigger (for custom models verify the registry price or upstream include_usage)",
		"notify.repeat.body":             "The same directive has been issued %d times (%s): %s",
		"notify.repeat.event_prefix":     "Repeated directive: ",
		"notify.run_end.novel_prefix":    "<%s> ",
		"notify.run_end.cost_suffix":     " · cost $%.2f",
		"notify.run_end.done_summary":    "Writing done: %d chapters %d words",
		"notify.run_end.stopped_summary": "Coordinator stopped (%d chapters completed)",
		"notify.deliver_failed":          "notification delivery failed",
		"notify.degraded_no_send":        "notification degraded to log (no notify-send)",
		"notify.degraded_no_channel":     "notification degraded to log (platform has no system channel)",

		// error.export.* — export errors (exp/exporter.go)
		"error.export.load_progress":    "load progress failed: %w",
		"error.export.no_completed":     "no completed chapters yet, nothing to export",
		"error.export.range_invalid":    "invalid chapter range: from=%d > to=%d",
		"error.export.range_empty":      "range %d..%d has no completed chapters",
		"error.export.read_chapter":     "read chapter %d failed: %w",
		"error.export.chapter_missing":  "progress marks chapter %d as completed, but chapters/%02d.md is missing or empty",
		"error.export.file_exists":      "file already exists: %s (add --overwrite to overwrite)",
		"error.export.stat_outpath":     "check output path failed: %w",
		"error.export.render_epub":      "render EPUB failed: %w",
		"error.export.write_failed":     "write file failed: %w",
		"error.export.format_unsupport": "exp: unsupported format %q",
		"error.export.infer_extension":  "cannot infer format from extension %q (supports .txt / .epub)",

		// error.tool.* — tool errors returned to the LLM and shown in the TUI
		// ask_user
		"error.tool.ask.need_question":   "at least one question is required",
		"error.tool.ask.too_many":        "at most 4 questions, currently %d",
		"error.tool.ask.q_text_empty":    "question %d: question text must not be empty",
		"error.tool.ask.q_header_empty":  "question %d: header must not be empty",
		"error.tool.ask.q_header_long":   "question %d: header %q exceeds 12 characters",
		"error.tool.ask.q_opt_count":     "question %d: need 2-4 options, currently %d",
		"error.tool.ask.opt_label_empty": "question %d option %d: label must not be empty",
		"error.tool.ask.opt_desc_empty":  "question %d option %d: description must not be empty",
		// edit_chapter
		"error.tool.edit.old_empty":   "old_string must not be empty: %w",
		"error.tool.edit.old_eq_new":  "old_string equals new_string, nothing to change: %w",
		"error.tool.edit.done_locked": "chapter %d is completed and not in the PendingRewrites queue, cannot edit; to modify, let the editor review and trigger a rewrite/polish first: %w",
		"error.tool.edit.no_draft":    "chapter %d has neither a draft nor a final, call draft_chapter(mode=write, chapter=%d) to create the first draft first: %w",
		// commit_chapter
		"error.tool.commit.pending_unrecovered": "an unrecovered chapter commit exists: chapter %d (stage %s), recover or recommit that chapter first: %w",
		"error.tool.commit.not_allowed":         "chapter currently not allowed to commit: %w: %w",
		"error.tool.commit.arc_boundary_failed": "arc boundary detection failed chapter=%d: %w: %w",
		"error.tool.commit.no_change":           "chapter %d drafts and chapters content are identical, no %s change detected. Call draft_chapter(mode=write, chapter=%d) to write the new body after %s, then commit_chapter: %w",
		"error.tool.commit.mode_rewrite":        "rewrite",
		"error.tool.commit.mode_polish":         "polish",
		// save_foundation
		"error.tool.foundation.book_complete":  "the whole book is finished (phase=complete), appending a new volume is not allowed: %w",
		"error.tool.foundation.no_progress":    "progress is not initialized: %w",
		"error.tool.foundation.not_writing":    "complete_book can only be called in the writing stage (currently phase=%s): %w",
		"error.tool.foundation.rework_pending": "%d chapters still in the rework queue, finish them then call complete_book: %w",
		// reopen_book
		"error.tool.reopen.chapters_empty": "chapters must not be empty, specify the chapters to rework: %w",
		"error.tool.reopen.no_progress":    "progress is not initialized: %w",
		"error.tool.reopen.not_done":       "chapter %v is not finished, reopen only reworks completed chapters (to add/expand plot use length adjustment): %w",
		// save_directive
		"error.tool.directive.add_need_text":   "add needs non-empty text: %w",
		"error.tool.directive.remove_need_idx": "remove needs index >= 1: %w",
	})
}
