package i18n

// English mirror of cat_cli_vi.go — operator CLI messages. Keys + fmt verbs must match.
func init() {
	Register(LangEN, map[string]string{
		"cli.update.windows_unsupported":  "Windows does not support in-place self-update, download a new build from https://github.com/%s/releases",
		"cli.update.release_no_tag":       "release missing tag_name",
		"cli.update.no_asset":             "release %s has no installer for the current platform *%s",
		"cli.update.unsupported_os":       "unsupported OS %s",
		"cli.update.unsupported_arch":     "unsupported architecture %s",
		"cli.update.binary_not_found":     "%s not found in the installer package",
		"cli.update.locate_exe_failed":    "cannot locate the current executable",
		"cli.config.project_parse_failed": "failed to parse project config ./.ainovel/config.json (check JSON syntax): %w",
		"cli.config.switch_model_failed":  "failed to switch model: %w",
		"cli.config.provider_type_failed": "failed to resolve provider type: %w",
	})
}
