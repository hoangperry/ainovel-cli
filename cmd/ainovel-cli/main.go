package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/entry/headless"
	"github.com/voocel/ainovel-cli/internal/entry/tui"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/rules"
	buildversion "github.com/voocel/ainovel-cli/internal/version"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// headlessMode ghi lại lần này có khởi động ở chế độ headless hay không, để die quyết định có tạm dừng khi thoát do lỗi không.
var headlessMode bool

func main() {
	// Locale sớm theo AINOVEL_LANG (chưa parse được --lang) để lỗi parse flag đúng ngôn ngữ.
	i18n.SetLang(resolveLang("", ""))

	opts, args, err := parseCLIOptions(os.Args[1:])
	if err != nil {
		die("flags: %v", err)
	}

	// Đã biết --lang: áp ngay cho version / update / die; resolve lại với cfg.UILang trong runWithConfig.
	i18n.SetLang(resolveLang(opts.Lang, ""))

	if opts.Version {
		buildversion.Print(os.Stdout, versionInfo())
		return
	}
	if opts.Update {
		if err := runSelfUpdate(opts.UpdateVersion); err != nil {
			fmt.Fprintf(os.Stderr, "update: %v\n", err)
			os.Exit(1)
		}
		return
	}
	headlessMode = opts.Headless

	// Thiết lập lần đầu
	if bootstrap.NeedsSetup(opts.ConfigPath) {
		if opts.Headless {
			die("%s", i18n.T("main.die.headless_no_setup"))
		}
		setupCfg, err := bootstrap.RunSetup()
		if err != nil {
			die("setup: %v", err)
		}
		// Sau khi thiết lập xong thì dùng config vừa tạo để tiếp tục
		runWithConfig(setupCfg, opts, args)
		return
	}

	// Load config
	cfg, err := bootstrap.LoadConfig(opts.ConfigPath)
	if err != nil {
		die("config: %v", err)
	}

	runWithConfig(cfg, opts, args)
}

// die xử lý thống nhất việc thoát do lỗi nghiêm trọng: in ra stderr, ghi xuống ~/.ainovel/last-error.log,
// và ở terminal tương tác (không headless) thì tạm dừng chờ Enter — khi khởi động bằng double-click, console sẽ đóng
// ngay khi tiến trình thoát, không tạm dừng thì lỗi lóe lên rồi mất, chính là căn nguyên ở issue #37 khiến người dùng không truy được.
func die(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, msg)
	if path := bootstrap.WriteStartupError(msg); path != "" {
		fmt.Fprintf(os.Stderr, i18n.T("main.exit.logged"), path)
	}
	if !headlessMode && stdinIsTerminal() {
		fmt.Fprint(os.Stderr, i18n.T("main.exit.press_enter"))
		fmt.Fscanln(os.Stdin)
	}
	os.Exit(1)
}

// stdinIsTerminal xác định standard input có nối tới terminal (character device) hay không. Khởi động bằng double-click / terminal tương tác
// là true; pipe, redirect, CI là false. Xấp xỉ zero-dependency, đủ để phân biệt có cần tạm dừng hay không.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func runWithConfig(cfg bootstrap.Config, opts cliOptions, args []string) {
	// Resolve lại locale với cfg.UILang đã biết (--lang/env vẫn ưu tiên hơn config).
	i18n.SetLang(resolveLang(opts.Lang, cfg.UILang))
	// Đặt locale nội dung LLM-facing (tool Description/Schema, prompt builder...) theo
	// output_lang, khớp với cây asset; phải đặt trước khi dựng agent/tool.
	contentlang.Set(assets.AssetLocale(cfg.ResolveOutputLang()))

	rules.EnsureHomeRulesDir()

	if len(args) > 0 {
		die("%s", i18n.T("main.die.no_cli_prompt"))
	}

	bundle := assets.Load(cfg.Style, cfg.ResolveOutputLang())
	if opts.Headless {
		prompt, err := loadPrompt(opts)
		if err != nil {
			die("error: %v", err)
		}
		if err := headless.Run(cfg, bundle, headless.Options{Prompt: prompt}); err != nil {
			die("error: %v", err)
		}
		return
	}
	if opts.Prompt != "" || opts.PromptFile != "" {
		die("%s", i18n.T("main.die.prompt_headless_only"))
	}
	if err := tui.Run(cfg, bundle, versionInfo().Version); err != nil {
		die("error: %v", err)
	}
}

type cliOptions struct {
	ConfigPath    string
	Headless      bool
	Prompt        string
	PromptFile    string
	Version       bool
	Update        bool
	UpdateVersion string
	Lang          string // --lang vi|en; rỗng = theo env/config/mặc định vi
}

// resolveLang chọn locale giao diện theo thứ tự ưu tiên: --lang > AINOVEL_LANG >
// cfg.UILang > mặc định vi. Giá trị không hợp lệ ở mỗi tầng bị bỏ qua, rơi xuống tầng sau.
func resolveLang(flagLang, cfgLang string) i18n.Lang {
	if l, ok := i18n.ParseLang(flagLang); ok {
		return l
	}
	if l, ok := i18n.ParseLang(os.Getenv("AINOVEL_LANG")); ok {
		return l
	}
	if l, ok := i18n.ParseLang(cfgLang); ok {
		return l
	}
	return i18n.DefaultLang
}

// parseCLIOptions trích xuất các CLI flag, trả về options và các tham số còn lại.
func parseCLIOptions(argv []string) (cliOptions, []string, error) {
	var opts cliOptions
	var args []string
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--version", "-v":
			opts.Version = true
		case "version":
			if i+1 < len(argv) {
				return opts, nil, errors.New(i18n.T("main.flag.version_no_arg"))
			}
			opts.Version = true
		case "update":
			if opts.Update {
				return opts, nil, errors.New(i18n.T("main.flag.update_once"))
			}
			opts.Update = true
			if i+1 < len(argv) {
				if strings.HasPrefix(argv[i+1], "-") {
					return opts, nil, errors.New(i18n.T("main.flag.update_one_arg"))
				}
				opts.UpdateVersion = argv[i+1]
				i++
			}
			if i+1 < len(argv) {
				return opts, nil, errors.New(i18n.T("main.flag.update_one_arg"))
			}
		case "--config":
			if i+1 >= len(argv) {
				return opts, nil, errors.New(i18n.T("main.flag.config_missing_val"))
			}
			opts.ConfigPath = argv[i+1]
			i++
		case "--lang":
			if i+1 >= len(argv) {
				return opts, nil, errors.New(i18n.T("main.flag.lang_missing_val"))
			}
			opts.Lang = argv[i+1]
			i++
		case "--headless":
			opts.Headless = true
		case "--prompt":
			if i+1 >= len(argv) {
				return opts, nil, errors.New(i18n.T("main.flag.prompt_missing_val"))
			}
			opts.Prompt = argv[i+1]
			i++
		case "--prompt-file":
			if i+1 >= len(argv) {
				return opts, nil, errors.New(i18n.T("main.flag.prompt_file_missing_val"))
			}
			opts.PromptFile = argv[i+1]
			i++
		default:
			args = append(args, argv[i])
		}
	}
	if opts.Prompt != "" && opts.PromptFile != "" {
		return opts, nil, errors.New(i18n.T("main.flag.prompt_conflict"))
	}
	if opts.Version && (opts.Update || opts.ConfigPath != "" || opts.Headless || opts.Prompt != "" || opts.PromptFile != "" || len(args) > 0) {
		return opts, nil, errors.New(i18n.T("main.flag.version_conflict"))
	}
	if opts.Update && (opts.ConfigPath != "" || opts.Headless || opts.Prompt != "" || opts.PromptFile != "" || len(args) > 0) {
		return opts, nil, errors.New(i18n.T("main.flag.update_conflict"))
	}
	return opts, args, nil
}

func versionInfo() buildversion.Info {
	return buildversion.Resolve(buildversion.Info{
		Version: version,
		Commit:  commit,
		Date:    date,
	})
}

func runSelfUpdate(target string) error {
	info := versionInfo()
	result, err := buildversion.Update(context.Background(), buildversion.UpdateOptions{
		Repo:           "voocel/ainovel-cli",
		BinaryName:     "ainovel-cli",
		TargetVersion:  target,
		CurrentVersion: info.Version,
	})
	if err != nil {
		return err
	}
	if !result.Updated {
		fmt.Printf(i18n.T("main.update.latest"), result.Version)
		return nil
	}
	fmt.Printf(i18n.T("main.update.updated"), result.Version)
	fmt.Printf(i18n.T("main.update.install_path"), result.Path)
	return nil
}

func loadPrompt(opts cliOptions) (string, error) {
	if opts.PromptFile == "" {
		return strings.TrimSpace(opts.Prompt), nil
	}

	var data []byte
	var err error
	if opts.PromptFile == "-" {
		data, err = os.ReadFile("/dev/stdin")
	} else {
		data, err = os.ReadFile(opts.PromptFile)
	}
	if err != nil {
		return "", fmt.Errorf(i18n.T("main.err.read_prompt"), err)
	}
	return strings.TrimSpace(string(data)), nil
}
