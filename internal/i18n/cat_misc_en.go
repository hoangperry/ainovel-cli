package i18n

// English catalog for smaller packages. Wave 2 / U2e.
// Mirrors cat_misc_vi.go key-for-key (parity + verb tests enforce identical keys
// and matching fmt verbs). Vietnamese is primary; this is the English mirror.
func init() {
	Register(LangEN, map[string]string{
		// store.* — tool errors + consistency warnings (operator-facing)
		"store.progress.not_initialized":        "progress not initialized: %w",
		"store.progress.reopen_only_complete":   "reopen only applies to a completed book (current phase=%s): %w",
		"store.progress.rewrite_verb":           "rewrite",
		"store.progress.polish_verb":            "polish",
		"store.progress.chapter_not_in_queue":   "chapter %d is not in the pending-%s queue, current queue: %v. Process the queued chapters first before touching a new chapter: %w",
		"store.progress.pending_completed_only": "pending_rewrites may only contain completed chapters, invalid chapters: %v, completed_chapters=%v: %w",
		"store.directives.limit_reached":        "long-lived directives reached the limit of %d, use remove to delete or merge old requests before adding",
		"store.directives.index_out_of_range":   "index %d out of range (currently %d total)",
		"store.consistency.chapter_missing":     "progress marks chapter %d as completed, but chapters/%02d.md does not exist or is empty",
		"store.consistency.va_not_found":        "current progress V%d A%d has no matching entry in the layered outline",
		"store.outline.volume_index_order":      "volume Index %d must be greater than the current maximum %d",
		"store.outline.volume_needs_arc":        "a new volume must contain at least one arc",
		"store.outline.first_arc_detail":        "the first arc of a new volume must contain detailed chapters",
		"store.outline.ending_required":         "ending_direction must not be empty",

		// rules.* — Conflict.Detail (diagnostics shown on the operator /diag panel)
		"rules.parse.yaml_failed":           "front matter YAML parse failed: %v",
		"rules.parse.unknown_field":         "unknown field %q, not supported in Phase 1; ignored",
		"rules.parse.chapter_words_format":  "chapter_words expects a range \"min-max\" (e.g. 3000-6000) or a single target value (e.g. 2500), got %v",
		"rules.parse.fatigue_blank_key":     "fatigue_words has a blank key, skipped",
		"rules.parse.fatigue_int_expected":  "fatigue_words[%q] expects an int threshold, got %T (%v); discarded this key",
		"rules.parse.fatigue_threshold_pos": "fatigue_words[%q] threshold must be > 0, got %d; discarded this key",
		"rules.parse.fatigue_list_string":   "fatigue_words list element expects string, got %T (%v); discarded this element",
		"rules.parse.fatigue_list_blank":    "fatigue_words list element is blank; discarded",
		"rules.parse.list_string_expected":  "%s list element expects string, got %T (%v); discarded this element",
		"rules.parse.type_error":            "field %s has wrong type, expected %s, got %T (%v); discarded",
		"rules.type.map_or_list":            "map[string]int or []string",
		"rules.merge.fatigue_conflict":      "field fatigue_words[%q] declared in multiple sources with inconsistent thresholds: %s; nearest source wins: %s",
		"rules.merge.field_conflict":        "field %s declared in multiple sources with inconsistent values: %s; nearest source wins: %s",
		"rules.load.read_failed":            "read failed: ",
		"rules.load.dir_read_failed":        "rules directory read failed: ",

		// models.* — diagnostic slog
		"models.refresh_failed": "model metadata refresh failed",
		"models.ready":          "model metadata ready",

		// startup.* — startup mode labels
		"startup.mode.quick":    "Quick Start",
		"startup.mode.cocreate": "Co-Create Planning",
	})
}
