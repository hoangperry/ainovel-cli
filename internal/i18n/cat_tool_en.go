package i18n

// English mirror of cat_tool_vi.go — tool Label / ActivityDescription shown in the TUI.
// Every key here must exist in cat_tool_vi.go (enforced by TestCatalogParity).
func init() {
	Register(LangEN, map[string]string{
		"ui.tool.check_consistency.label":   "check consistency",
		"ui.tool.ask_user.label":            "ask user",
		"ui.tool.commit_chapter.label":      "commit chapter",
		"ui.tool.draft_chapter.label":       "draft chapter",
		"ui.tool.edit_chapter.label":        "edit chapter",
		"ui.tool.edit_chapter.activity":     "editing chapter draft",
		"ui.tool.context.label":             "load context",
		"ui.tool.plan_chapter.label":        "plan chapter",
		"ui.tool.reopen_book.label":         "reopen for rework",
		"ui.tool.reopen_book.activity":      "reopening the whole book for rework",
		"ui.tool.read_chapter.label":        "read chapter",
		"ui.tool.save_directive.label":      "save directive",
		"ui.tool.save_directive.activity":   "saving directive",
		"ui.tool.save_review.label":         "save review",
		"ui.tool.save_arc_summary.label":    "save arc summary",
		"ui.tool.save_volume_summary.label": "save volume summary",
		"ui.tool.save_foundation.label":     "save foundation",
	})
}
