package i18n

// English catalog for bootstrap/setup + main (die/Errorf). Wave 2 / U2d.
// Mirror of cat_setup_vi.go — every key here must exist in vi and vice versa
// (enforced by TestCatalogParity), with matching fmt verbs.
func init() {
	Register(LangEN, map[string]string{
		// setup.* — first-run wizard (RunSetup)
		"setup.intro.detect":             "No config file detected, starting initial setup...",
		"setup.intro.config_path":        "  Config file path: %s\n",
		"setup.intro.edit_hint":          "  You can edit this file anytime afterwards to adjust advanced settings.\n",
		"setup.step.apikey_optional":     "[2/4] API Key (may be left empty)",
		"setup.step.apikey_empty":        "empty means no API Key is used",
		"setup.step.apikey":              "[2/4] API Key",
		"setup.step.baseurl":             "[3/4] Base URL (Enter for default, proxy users enter the proxy address)",
		"setup.step.baseurl_hint":        "empty uses the official address",
		"setup.step.model":               "[4/4] Model name",
		"setup.step.model_hint":          "e.g. gpt-4o / claude-sonnet-4 / gemini-2.5-pro",
		"setup.value.apikey_unset":       "not set",
		"setup.value.baseurl_default":    "default",
		"setup.prompt.provider_name":     "Provider name",
		"setup.label.provider_select":    "[1/4] Select Provider",
		"setup.label.api_type":           "API protocol type",
		"setup.label.api_type_openai":    "OpenAI compatible",
		"setup.label.api_type_anthropic": "Anthropic compatible",
		"setup.label.api_type_gemini":    "Gemini compatible",
		"setup.saved":                    "%s Config saved to %s\n",
		"setup.saved.default_model":      "  Default model: %s\n",
		"setup.saved.role_hint":          "  To configure different models per role, just edit the config file.",
		"setup.saved.rules_hint":         "  Global writing preferences can go in .md files under %s (see README.txt there)\n",
		"setup.select.help":              "\n  ↑↓ select  Enter confirm  Esc cancel",
		"setup.input.help":               "  (Enter to confirm, Esc to cancel)",

		// error.config.* — config errors wrapped with %w (use fmt.Errorf(T(key), err))
		"error.config.provider_missing_type": "provider %q missing type, and not in litellm's known provider list: %w",
		"error.config.provider_no_creds":     "provider %q has no credentials declared in providers; if you override provider in ./.ainovel/config.json you must also declare providers.%s (with api_key/base_url), not just change the top-level provider: %w",

		// main.* — die()/Errorf in cmd/ainovel-cli/main.go (shown via die)
		"main.die.headless_no_setup":        "error: headless mode does not support first-run setup, please run the TUI once to complete configuration",
		"main.die.no_cli_prompt":            "error: passing the novel request directly via command line is no longer supported, please start and enter it in the TUI input box",
		"main.die.prompt_headless_only":     "error: --prompt/--prompt-file can only be used in --headless mode",
		"main.err.read_prompt":              "read prompt failed: %w",
		"main.update.latest":                "ainovel-cli is already at the latest version %s\n",
		"main.update.updated":               "ainovel-cli updated to %s\n",
		"main.update.install_path":          "Install location: %s\n",
		"main.flag.version_no_arg":          "version takes no arguments",
		"main.flag.update_once":             "update can only be specified once",
		"main.flag.update_one_arg":          "update accepts only one optional version argument",
		"main.flag.config_missing_val":      "--config is missing a value",
		"main.flag.lang_missing_val":        "--lang is missing a value",
		"main.flag.prompt_missing_val":      "--prompt is missing a value",
		"main.flag.prompt_file_missing_val": "--prompt-file is missing a value",
		"main.flag.prompt_conflict":         "--prompt and --prompt-file cannot be used together",
		"main.flag.version_conflict":        "version cannot be combined with other startup arguments",
		"main.flag.update_conflict":         "update cannot be combined with other startup arguments",
	})
}
