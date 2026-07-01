package bootstrap

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/utils"
)

// exampleConfig là khuôn mẫu kèm comment ghi vào ~/.ainovel/config.example.jsonc sau khi setup.
// Nguồn dữ liệu duy nhất: nhúng thẳng config.example.jsonc cùng thư mục, tránh trôi lệch với mẫu trong tài liệu.
//
//go:embed config.example.jsonc
var exampleConfig string

// NeedsSetup kiểm tra có cần setup lần đầu hay không (kích hoạt khi file cấu hình không tồn tại).
func NeedsSetup(flagPath string) bool {
	if flagPath != "" {
		_, err := os.Stat(flagPath)
		return os.IsNotExist(err)
	}
	if p := DefaultConfigPath(); p != "" {
		if _, err := os.Stat(p); err == nil {
			return false
		}
	}
	if _, err := os.Stat(projectConfigPath()); err == nil {
		return false
	}
	return true
}

type setupProvider struct {
	name           string
	label          string
	baseURL        string // base_url điền sẵn
	needType       bool   // proxy tuỳ biến cần hỏi thêm type và base_url
	apiKeyOptional bool   // true nghĩa là API Key cho phép để trống
}

var setupProviders = []setupProvider{
	{name: "openrouter", label: "OpenRouter", baseURL: "https://openrouter.ai/api/v1"},
	{name: "anthropic", label: "Anthropic"},
	{name: "gemini", label: "Gemini"},
	{name: "openai", label: "OpenAI"},
	{name: "deepseek", label: "DeepSeek"},
	{name: "qwen", label: "Qwen"},
	{name: "glm", label: "GLM"},
	{name: "grok", label: "Grok"},
	{name: "ollama", label: "Ollama", baseURL: "http://localhost:11434/v1", apiKeyOptional: true},
	{name: "bedrock", label: "Bedrock", apiKeyOptional: true},
	{name: "custom", label: "Custom Proxy", needType: true, apiKeyOptional: true},
}

// RunSetup chạy setup lần đầu, trả về cấu hình đã sinh.
func RunSetup() (Config, error) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).
		Render(i18n.T("setup.intro.detect")))
	fmt.Fprintf(os.Stderr, i18n.T("setup.intro.config_path"), lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(DefaultConfigPath()))
	fmt.Fprint(os.Stderr, i18n.T("setup.intro.edit_hint"))
	fmt.Fprintln(os.Stderr)

	// Step 1: Chọn Provider
	sp, err := runProviderSelect()
	if err != nil {
		return Config{}, err
	}

	providerName := sp.name
	var pc ProviderConfig
	printStepDone("Provider", sp.label)

	// Proxy tuỳ biến: hỏi thêm tên và loại giao thức API
	if sp.needType {
		providerName, err = runTextInput(i18n.T("setup.prompt.provider_name"), "my-proxy")
		if err != nil {
			return Config{}, err
		}
		providerType, err := runTypeSelect()
		if err != nil {
			return Config{}, err
		}
		pc.Type = providerType
	}

	// Step 2: Nhập API Key
	var apiKey string
	if sp.apiKeyOptional {
		apiKey, err = runOptionalTextInput(i18n.T("setup.step.apikey_optional"), i18n.T("setup.step.apikey_empty"))
	} else {
		apiKey, err = runTextInput(i18n.T("setup.step.apikey"), "sk-xxx")
	}
	if err != nil {
		return Config{}, err
	}
	pc.APIKey = apiKey
	if apiKey == "" {
		printStepDone("API Key", i18n.T("setup.value.apikey_unset"))
	} else {
		printStepDone("API Key", maskKey(apiKey))
	}

	// Step 3: Base URL (nhấn Enter trực tiếp để dùng địa chỉ mặc định chính thức)
	baseDefault := sp.baseURL
	baseHint := i18n.T("setup.step.baseurl_hint")
	if baseDefault != "" {
		baseHint = baseDefault
	}
	baseURL, err := runTextInputWithDefault(i18n.T("setup.step.baseurl"), baseHint, baseDefault)
	if err != nil {
		return Config{}, err
	}
	pc.BaseURL = baseURL
	if baseURL != "" {
		printStepDone("Base URL", baseURL)
	} else {
		printStepDone("Base URL", i18n.T("setup.value.baseurl_default"))
	}

	// Step 4: Tên model (bắt buộc)
	modelName, err := runTextInput(i18n.T("setup.step.model"), i18n.T("setup.step.model_hint"))
	if err != nil {
		return Config{}, err
	}
	printStepDone("Model", modelName)

	cfg := Config{
		Provider:  providerName,
		ModelName: modelName,
		Providers: map[string]ProviderConfig{providerName: pc},
		Roles:     map[string]RoleConfig{},
		Style:     "default",
	}

	// Lưu
	path := DefaultConfigPath()
	if err := SaveConfig(path, cfg); err != nil {
		return cfg, fmt.Errorf("save config: %w", err)
	}

	// Sinh khuôn mẫu kèm comment
	saveExampleConfig()

	// Thư mục preference toàn cục do quy trình khởi động (runWithConfig) tạo thống nhất, ở đây chỉ lấy đường dẫn để hiển thị gợi ý
	rulesDir := rules.DefaultHomeRulesDir()

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, i18n.T("setup.saved"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓"), path)
	fmt.Fprintf(os.Stderr, i18n.T("setup.saved.default_model"), modelName)
	fmt.Fprintln(os.Stderr, i18n.T("setup.saved.role_hint"))
	if rulesDir != "" {
		fmt.Fprintf(os.Stderr, i18n.T("setup.saved.rules_hint"), rulesDir)
	}
	fmt.Fprintln(os.Stderr)

	return cfg, nil
}

func saveExampleConfig() {
	dir, err := configDir()
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "config.example.jsonc"), []byte(exampleConfig), 0o644)
}

// printStepDone in ra dòng xác nhận một bước đã hoàn tất.
func printStepDone(label, value string) {
	fmt.Fprintf(os.Stderr, "  %s %s: %s\n",
		lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓"),
		label,
		lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(value))
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

// ---------- Thành phần TUI ----------

func runProviderSelect() (setupProvider, error) {
	m := setupSelectModel{
		title: i18n.T("setup.label.provider_select"),
		items: setupProviders,
	}
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return setupProvider{}, err
	}
	result := final.(setupSelectModel)
	if result.cancelled {
		return setupProvider{}, fmt.Errorf("setup cancelled")
	}
	return result.items[result.cursor], nil
}

// apiTypeOptions xây danh sách loại giao thức API tại thời điểm gọi, để label
// dịch theo locale đã set (không cố định lúc init khi locale chưa biết).
func apiTypeOptions() []setupProvider {
	return []setupProvider{
		{name: "openai", label: i18n.T("setup.label.api_type_openai")},
		{name: "anthropic", label: i18n.T("setup.label.api_type_anthropic")},
		{name: "gemini", label: i18n.T("setup.label.api_type_gemini")},
	}
}

func runTypeSelect() (string, error) {
	m := setupSelectModel{
		title: i18n.T("setup.label.api_type"),
		items: apiTypeOptions(),
	}
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	result := final.(setupSelectModel)
	if result.cancelled {
		return "", fmt.Errorf("setup cancelled")
	}
	return result.items[result.cursor].name, nil
}

func runTextInput(label, placeholder string) (string, error) {
	return runTextInputWithDefault(label, placeholder, "")
}

func runOptionalTextInput(label, placeholder string) (string, error) {
	m := setupInputModel{label: label, placeholder: placeholder, allowEmpty: true}
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	result := final.(setupInputModel)
	if result.cancelled {
		return "", fmt.Errorf("setup cancelled")
	}
	return utils.CleanInputLine(result.value), nil
}

func runTextInputWithDefault(label, placeholder, defaultValue string) (string, error) {
	m := setupInputModel{label: label, placeholder: placeholder, defaultValue: defaultValue}
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	result := final.(setupInputModel)
	if result.cancelled {
		return "", fmt.Errorf("setup cancelled")
	}
	if result.value == "" && result.defaultValue != "" {
		return result.defaultValue, nil
	}
	return utils.CleanInputLine(result.value), nil
}

// ---------- Bộ chọn ----------

var (
	setupCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	setupDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	setupHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	setupInputStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
)

type setupSelectModel struct {
	title     string
	items     []setupProvider
	cursor    int
	cancelled bool
}

func (m setupSelectModel) Init() tea.Cmd { return nil }

func (m setupSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			return m, tea.Quit
		case "q", "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m setupSelectModel) View() string {
	var b strings.Builder
	b.WriteString(setupHeaderStyle.Render(m.title))
	b.WriteString("\n\n")
	for i, item := range m.items {
		cursor := "  "
		label := item.label
		if i == m.cursor {
			cursor = setupCursorStyle.Render("❯ ")
			label = setupCursorStyle.Render(label)
		}
		b.WriteString(cursor + label + "\n")
	}
	b.WriteString(setupDimStyle.Render(i18n.T("setup.select.help")))
	return b.String()
}

// ---------- Nhập văn bản ----------

type setupInputModel struct {
	label        string
	placeholder  string
	defaultValue string // Giá trị mặc định dùng khi nhấn Enter trực tiếp
	allowEmpty   bool   // Cho phép nhập giá trị rỗng trực tiếp
	value        string
	cancelled    bool
}

func (m setupInputModel) Init() tea.Cmd { return nil }

func (m setupInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "enter":
			if utils.CleanInputLine(m.value) != "" || m.defaultValue != "" || m.allowEmpty {
				return m, tea.Quit
			}
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "backspace":
			if len(m.value) > 0 {
				runes := []rune(m.value)
				m.value = string(runes[:len(runes)-1])
			}
		default:
			if msg.Type == tea.KeyRunes {
				m.value += utils.CleanInputRunes(msg.Runes)
			} else if msg.Type == tea.KeySpace {
				m.value += " "
			}
		}
	}
	return m, nil
}

func (m setupInputModel) View() string {
	var b strings.Builder
	b.WriteString(setupHeaderStyle.Render(m.label))
	b.WriteString("\n\n")
	b.WriteString(setupInputStyle.Render("❯ "))
	if m.value == "" {
		b.WriteString(setupCursorStyle.Render("▌"))
		b.WriteString(setupDimStyle.Render(m.placeholder))
	} else {
		b.WriteString(m.value)
		b.WriteString(setupCursorStyle.Render("▌"))
	}
	b.WriteString(setupDimStyle.Render(i18n.T("setup.input.help")))
	b.WriteString("\n")
	return b.String()
}
