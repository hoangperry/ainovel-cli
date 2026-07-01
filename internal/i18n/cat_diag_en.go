package i18n

// English mirror for the diag area (quality + flow diagnostics). Wave 2 / U2c.
// Keys MUST match cat_diag_vi.go exactly; fmt verbs MUST match (parity + verb tests
// enforce). Stable tokens code/tests pattern-match (ch%d, payoff, percent) kept stable.
func init() {
	Register(LangEN, map[string]string{
		// diag.quality.* — ChronicLowDimension
		"diag.quality.chronic_low_dim.title":      "Dimension [%s] chronically low (avg %.0f)",
		"diag.quality.chronic_low_dim.evidence":   "%d reviews total, average score %.1f",
		"diag.quality.chronic_low_dim.suggestion": "Check whether the Writer prompt guidance about %s is clear, or whether the Editor prompt scoring criteria for %s are reasonable.",

		// diag.quality.* — ContractMissPattern
		"diag.quality.contract_miss.title":      "Low contract fulfillment rate (%.0f%% unmet)",
		"diag.quality.contract_miss.evidence":   "Unmet: [%s], %d/%d total",
		"diag.quality.contract_miss.suggestion": "Writer may not have read the contract, or the contract required_beats are too aggressive. Check the coordination between plan_chapter and writer.md.",

		// diag.quality.* — HookWeakChain
		"diag.quality.hook_weak.title":      "Chapter-end hooks chronically weak (%d chapters in a row)",
		"diag.quality.hook_weak.suggestion": "Check whether hook_goal execution in writer.md is clear; if needed, spell out this chapter's read-on appeal in plan_chapter, and calibrate the Editor's evidence standard for hook.",

		// diag.quality.* — PayoffMissPattern
		"diag.quality.payoff_miss.title":      "Low payoff/plot-point delivery rate (%.0f%% unmet)",
		"diag.quality.payoff_miss.evidence":   "Undelivered chapters: [%s], %d/%d total",
		"diag.quality.payoff_miss.detail":     "ch%d(%d payoff)",
		"diag.quality.payoff_miss.suggestion": "Check whether plan_chapter payoff_points are too many or too empty, and ensure the Writer delivers them explicitly in the body rather than only setting them up.",

		// diag.quality.* — ExcessiveRewrites
		"diag.quality.excessive_rewrites.title":      "Rewrite rate too high (%d/%d = %.0f%%)",
		"diag.quality.excessive_rewrites.evidence":   "%d reviews total, %d rewrites",
		"diag.quality.excessive_rewrites.suggestion": "Writer keeps producing content below the Editor threshold. Check whether the Writer prompt quality standard aligns with the Editor review standard.",

		// diag.quality.* — WordCountAnomaly
		"diag.quality.word_anomaly.title":      "Chapter word-count anomaly (average %d words)",
		"diag.quality.word_anomaly.detail":     "ch%d(%d words,%.0f%%)",
		"diag.quality.word_anomaly.suggestion": "Very short chapters may be output truncation (token limit); very long chapters may consume too much context window. Check the model max_tokens config.",

		// diag.flow.* — InvalidPendingRewrites
		"diag.flow.invalid_pending.title":      "Rewrite queue contains incomplete chapters: [%s]",
		"diag.flow.invalid_pending.evidence":   "pending_rewrites=[%s], completed_chapters=[%s], flow=%s",
		"diag.flow.invalid_pending.suggestion": "This is a corrupted state invariant. Stop the run then edit meta/progress.json to remove incomplete chapters from pending_rewrites; if the queue is empty, set flow to writing and clear rewrite_reason.",

		// diag.flow.* — RewritePendingPressure
		"diag.flow.rewrite_pending.title":      "Chapters pending rewrite: [%s]",
		"diag.flow.rewrite_pending.evidence":   "flow=%s, pending_rewrites=[%s]",
		"diag.flow.rewrite_pending.suggestion": "Check whether the Editor review standard is too strict, or whether the Writer rewrite prompt is effective. To intervene manually, submit an intervention instruction in the input box.",

		// diag.flow.* — OrphanedSteer
		"diag.flow.orphaned_steer.title":      "An unconsumed steer instruction exists",
		"diag.flow.orphaned_steer.evidence":   "pending_steer=%q, flow=%s",
		"diag.flow.orphaned_steer.suggestion": "This steer was persisted but never consumed by the Coordinator. Check the interrupt-recovery logic, or override by resubmitting.",

		// diag.flow.* — PhaseFlowMismatch
		"diag.flow.phase_mismatch.title":      "Phase/flow state mismatch: phase=%s, flow=%s",
		"diag.flow.phase_mismatch.evidence":   "phase=%s should not show a non-initial flow=%s",
		"diag.flow.phase_mismatch.suggestion": "The state machine may be corrupted; manually inspect the phase and flow fields of meta/progress.json.",

		// diag.flow.* — ChapterGaps
		"diag.flow.chapter_gaps.title":      "Chapter gap: missing [%s]",
		"diag.flow.chapter_gaps.evidence":   "completed=[%s]",
		"diag.flow.chapter_gaps.suggestion": "commit_chapter may have been interrupted midway. Check whether meta/pending_commit.json has an incomplete commit.",
	})
}
