package i18n

// English catalog. Mirror of lang_vi.go — every key here must exist in vi and
// vice versa (enforced by TestCatalogParity), with matching fmt verbs.
func init() {
	Register(LangEN, map[string]string{
		// main.* — startup / exit lifecycle
		"main.exit.press_enter": "\nPress Enter to exit...",
		"main.exit.logged":      "(error details logged to %s)\n",

		// error.config.* — config errors wrapped with %w (use fmt.Errorf(T(key), err))
		"error.config.load_failed": "load config failed: %w",
	})
}
