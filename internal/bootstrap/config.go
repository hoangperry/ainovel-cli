package bootstrap

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/voocel/agentcore/llm"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/models"
	"github.com/voocel/ainovel-cli/internal/utils"
)

// DefaultContextWindow là kích thước window dự phòng khi model chưa được đăng ký trong registry.
const DefaultContextWindow = 200000

// CompactRatio là ngưỡng tương đối kích hoạt nén context window: nén khi tokens >= window * CompactRatio.
// 0.85 là giá trị kinh nghiệm, chừa 15% không gian đầu cho "prompt vòng sau + kết quả tool lớn", đồng thời
// giúp model window lớn cũng chủ động nén tại 85%, tránh ăn đầy mới nén dưới window danh nghĩa 1M (vùng suy giảm attention).
//
// Không phơi ra cho người dùng cấu hình: cùng nguồn với context_window đã bị xoá — trong kiến trúc đa model,
// để người dùng vặn núm số nhảy qua nhảy lại không bằng cố định một giá trị hợp lý trong code.
const CompactRatio = 0.85

// MinCompactReserve là cận dưới của ReserveTokens. Model window nhỏ (như qwen3:8b 32k local)
// tính reserve theo tỉ lệ 0.15 chỉ được 4800, một response tool commit_chapter đã chiếm 5-8k,
// chính văn một chương 8-15k — sẽ gặp cảnh "vừa nén xong lại tràn ngay". Mức 8000 dự phòng đảm bảo
// trường hợp xấu nhất vẫn còn nửa vòng đệm.
const MinCompactReserve = 8000

// CompactReserveTokens tính ngược ReserveTokens theo CompactRatio và áp dụng floor MinCompactReserve:
//
//	threshold = window - reserve = window * CompactRatio
//	reserve   = max(MinCompactReserve, window * (1 - CompactRatio))
//
// Dùng cho EngineConfig.ReserveTokens của agentcore.context.Engine.
func CompactReserveTokens(window int) int {
	if window <= 0 {
		return 0
	}
	reserve := window - int(float64(window)*CompactRatio)
	if reserve < MinCompactReserve {
		return MinCompactReserve
	}
	return reserve
}

// ProviderConfig định nghĩa credential của một nhà cung cấp LLM.
type ProviderConfig struct {
	Type    string   `json:"type,omitempty"`     // Loại giao thức API (openai/anthropic/gemini), chỉ định khi dùng proxy tuỳ biến
	APIKey  string   `json:"api_key,omitempty"`  // API Key
	BaseURL string   `json:"base_url,omitempty"` // API Base URL
	Models  []string `json:"models,omitempty"`   // Danh sách model tuỳ chọn, hiển thị khi chuyển đổi trong TUI
	// ExtraBody truyền thẳng các tham số bổ sung cho mỗi request của provider này (như temperature/top_p/min_p/
	// presence_penalty, hoặc key đặc thù nhà cung cấp như chat_template_kwargs để bật think của nvidia).
	// Đầu tương thích OpenAI ghép nguyên văn vào body request (tức quy ước extra_body); giá trị do người dùng tự chịu trách nhiệm.
	ExtraBody map[string]any `json:"extra_body,omitempty"`
	// Extra truyền thẳng vào cấu hình mức provider (litellm.ProviderConfig.Extra), dùng cho các tuỳ chọn
	// tầng client/transport như HTTP headers, user_agent, anthropic_beta.
	Extra map[string]any `json:"extra,omitempty"`
}

// RequiresAPIKey trả về liệu provider này có bắt buộc cấu hình api_key tường minh hay không.
// Quy ước:
// 1. ollama / bedrock cho phép không có key;
// 2. cấu hình chỉ định Type tường minh được xem là proxy tuỳ biến, cho phép không có key;
// 3. các provider khác mặc định yêu cầu key, giữ kiểm tra bảo thủ với endpoint do hãng host chính thức.
func (pc ProviderConfig) RequiresAPIKey(name string) bool {
	switch name {
	case "ollama", "bedrock":
		return false
	}
	return pc.Type == ""
}

// ProviderType trả về loại giao thức API hợp lệ.
// Ưu tiên dùng Type tường minh; nếu không thì yêu cầu tên provider đã có trong registry của litellm.
func (pc ProviderConfig) ProviderType(name string) (string, error) {
	if pc.Type != "" {
		return pc.Type, nil
	}
	if llm.IsProviderRegistered(name) {
		return name, nil
	}
	return "", fmt.Errorf(i18n.T("error.config.provider_missing_type"), name, errs.ErrConfig)
}

// ModelRef biểu thị một tổ hợp provider/model.
type ModelRef struct {
	Provider string `json:"provider"` // Tên provider (key trong map Providers)
	Model    string `json:"model"`    // Tên model (truyền nguyên văn, không phân giải gì)
}

// RoleConfig định nghĩa override model của một role.
type RoleConfig struct {
	Provider  string     `json:"provider"`            // Tên provider chính (key trong map Providers)
	Model     string     `json:"model"`               // Tên model chính (truyền nguyên văn, không phân giải gì)
	Fallbacks []ModelRef `json:"fallbacks,omitempty"` // Danh sách provider/model dự phòng tường minh
	// Thinking là cường độ tư duy của role này (off/minimal/low/medium/high/xhigh/max), rỗng = kế thừa mặc định tầng đỉnh.
	// Được áp dụng sau khi agents.ParseThinkingLevel kiểm tra, giá trị vượt cấp xem như rỗng.
	Thinking string `json:"thinking,omitempty"`
}

// knownRoles là các tên role được hỗ trợ.
var knownRoles = map[string]bool{
	"coordinator": true,
	"architect":   true,
	"writer":      true,
	"editor":      true,
}

// Giá trị ngôn ngữ mặc định và tập hợp lệ.
const (
	defaultUILang     = "vi"
	defaultOutputLang = "vi"
)

var (
	knownUILangs     = map[string]bool{"vi": true, "en": true}
	knownOutputLangs = map[string]bool{"vi": true, "en": true, "original": true}
)

// Config là cấu hình ứng dụng tiểu thuyết.
type Config struct {
	// Trường runtime (không serialize vào JSON)
	OutputDir string `json:"-"` // Thư mục output gốc

	// Cấu hình LLM mặc định
	Provider  string `json:"provider"` // provider mặc định (key trong map Providers)
	ModelName string `json:"model"`    // Tên model mặc định
	// Thinking là cường độ tư duy mặc định tầng đỉnh (off/minimal/low/medium/high/xhigh/max), rỗng = không override (giữ mặc định model/provider).
	// Role chưa cấu hình thinking riêng thì rơi về giá trị này.
	Thinking string `json:"thinking,omitempty"`

	// Kho credential của Provider
	Providers map[string]ProviderConfig `json:"providers,omitempty"`

	// Override model ở mức role
	Roles map[string]RoleConfig `json:"roles,omitempty"`

	// Tham số sáng tác
	Style string `json:"style,omitempty"`

	// Bản địa hoá. Hai trục độc lập, không trộn lẫn:
	//   UILang     — ngôn ngữ giao diện operator (TUI/lỗi/setup): vi|en; mặc định vi.
	//   OutputLang — ngôn ngữ AI viết truyện: vi|en|original (original = giữ ngôn ngữ
	//                gốc của prompt); mặc định vi.
	// Một người đọc giao diện tiếng Anh vẫn có thể muốn truyện tiếng Việt, nên tách riêng.
	UILang     string `json:"ui_lang,omitempty"`
	OutputLang string `json:"output_lang,omitempty"`

	// ContextWindow là kích thước window dùng cho nén context window. Để trống (0) thì tự phân giải theo tên model:
	// registry trúng thì dùng window thật của model, không trúng thì dự phòng DefaultContextWindow.
	// Cấu hình tường minh thì ưu tiên — dùng để chỉ định window thật cho model tuỳ biến mà registry không tra được,
	// hoặc ghim model window lớn ở giá trị nhỏ hơn để kích hoạt nén sớm (window danh nghĩa 1M ở mức 200k+ thường đã suy giảm attention).
	// Chỉ ảnh hưởng ngưỡng nén, không thay đổi độ dài request thật của LLM API; giá trị cấu hình do người dùng tự chịu trách nhiệm.
	ContextWindow int `json:"context_window,omitempty"`

	// Budget là chính sách ngân sách chi phí cho một quyển sách; chỉ bật khi book_usd > 0.
	Budget BudgetConfig `json:"budget,omitzero"`

	// Notify là cấu hình cảnh báo không người trực; mặc định bật (kênh system làm lưới đỡ).
	Notify NotifyConfig `json:"notify,omitzero"`
}

// BudgetConfig là tuyên bố chính sách của người dùng cho ví của một quyển sách. Vượt mức dừng máy tương đương
// người dùng tại thời điểm đó tự tay Abort — Host chỉ thực thi thay, không đánh giá hành vi model (biên hiến định §10 của kiến trúc).
type BudgetConfig struct {
	BookUSD   float64 `json:"book_usd,omitempty"`   // Bắt buộc điền mới bật; 0/để trống = không giới hạn
	WarnRatio float64 `json:"warn_ratio,omitempty"` // Mức cảnh báo, mặc định 0.8
	HardStop  bool    `json:"hard_stop,omitempty"`  // true = vượt mức dừng ngay; mặc định chờ tác vụ subagent hiện tại kết thúc
}

// Enabled trả về liệu chính sách budget có được bật hay không.
func (b BudgetConfig) Enabled() bool { return b.BookUSD > 0 }

// NotifyConfig là cấu hình kênh cảnh báo không người trực.
type NotifyConfig struct {
	Enabled *bool    `json:"enabled,omitempty"` // Mặc định true (kênh system dùng được không cần cấu hình)
	Command string   `json:"command,omitempty"` // Tuỳ chọn, cấu hình rồi sẽ thay kênh system (đẩy thông báo lên điện thoại đi qua đây)
	Events  []string `json:"events,omitempty"`  // Tuỳ chọn, lọc kind (run_end/repeat/budget), mặc định bật tất cả
}

// IsEnabled trả về liệu cảnh báo có được bật hay không (mặc định true).
func (n NotifyConfig) IsEnabled() bool { return n.Enabled == nil || *n.Enabled }

// ValidateBase kiểm tra cấu hình cơ bản.
func (c *Config) ValidateBase() error {
	if err := validateConfigText("provider", c.Provider); err != nil {
		return err
	}
	if err := validateConfigText("model", c.ModelName); err != nil {
		return err
	}

	if c.Provider == "" {
		return fmt.Errorf("provider is required: %w", errs.ErrConfig)
	}
	if c.ModelName == "" {
		return fmt.Errorf("model is required: %w", errs.ErrConfig)
	}

	// provider mặc định bắt buộc phải có credential
	pc, ok := c.Providers[c.Provider]
	if !ok {
		return fmt.Errorf(i18n.T("error.config.provider_no_creds"), c.Provider, c.Provider, errs.ErrConfig)
	}
	if pc.RequiresAPIKey(c.Provider) && pc.APIKey == "" {
		return fmt.Errorf("provider %q has no api_key configured: %w", c.Provider, errs.ErrConfig)
	}
	if err := validateProviderConfigText(c.Provider, pc); err != nil {
		return err
	}
	for name, provider := range c.Providers {
		if err := validateConfigText("provider name", name); err != nil {
			return err
		}
		if err := validateProviderConfigText(name, provider); err != nil {
			return err
		}
	}

	// kiểm tra override theo role
	for role, rc := range c.Roles {
		if err := validateConfigText("role name", role); err != nil {
			return err
		}
		if err := validateConfigText(fmt.Sprintf("role %q provider", role), rc.Provider); err != nil {
			return err
		}
		if err := validateConfigText(fmt.Sprintf("role %q model", role), rc.Model); err != nil {
			return err
		}
		if !knownRoles[role] {
			return fmt.Errorf("unknown role %q in roles config (valid: coordinator/architect/writer/editor): %w", role, errs.ErrConfig)
		}
		if rc.Provider == "" || rc.Model == "" {
			return fmt.Errorf("role %q must have both provider and model: %w", role, errs.ErrConfig)
		}
		if err := c.validateModelRef(
			fmt.Sprintf("role %q", role),
			ModelRef{Provider: rc.Provider, Model: rc.Model},
		); err != nil {
			return err
		}
		for i, fallback := range rc.Fallbacks {
			if err := validateConfigText(fmt.Sprintf("role %q fallback[%d] provider", role, i), fallback.Provider); err != nil {
				return err
			}
			if err := validateConfigText(fmt.Sprintf("role %q fallback[%d] model", role, i), fallback.Model); err != nil {
				return err
			}
			if err := c.validateModelRef(
				fmt.Sprintf("role %q fallback[%d]", role, i),
				fallback,
			); err != nil {
				return err
			}
		}
	}

	// kiểm tra chính sách budget
	if c.Budget.BookUSD < 0 {
		return fmt.Errorf("budget.book_usd must be >= 0: %w", errs.ErrConfig)
	}
	if c.Budget.Enabled() && (c.Budget.WarnRatio <= 0 || c.Budget.WarnRatio >= 1) {
		return fmt.Errorf("budget.warn_ratio must be in (0, 1): %w", errs.ErrConfig)
	}

	// kiểm tra cấu hình cảnh báo
	if err := validateConfigText("notify.command", c.Notify.Command); err != nil {
		return err
	}
	for _, ev := range c.Notify.Events {
		if !knownNotifyEvents[ev] {
			return fmt.Errorf("unknown notify event %q (valid: run_end/repeat/budget): %w", ev, errs.ErrConfig)
		}
	}

	// Kiểm tra ngôn ngữ giao diện / output (rỗng = dùng mặc định, hợp lệ).
	if c.UILang != "" && !knownUILangs[c.UILang] {
		return fmt.Errorf("unknown ui_lang %q (valid: vi/en): %w", c.UILang, errs.ErrConfig)
	}
	if c.OutputLang != "" && !knownOutputLangs[c.OutputLang] {
		return fmt.Errorf("unknown output_lang %q (valid: vi/en/original): %w", c.OutputLang, errs.ErrConfig)
	}

	return nil
}

var knownNotifyEvents = map[string]bool{"run_end": true, "repeat": true, "budget": true}

func validateProviderConfigText(name string, pc ProviderConfig) error {
	fields := []struct {
		label string
		value string
	}{
		{label: fmt.Sprintf("provider %q type", name), value: pc.Type},
		{label: fmt.Sprintf("provider %q api_key", name), value: pc.APIKey},
		{label: fmt.Sprintf("provider %q base_url", name), value: pc.BaseURL},
	}
	for _, field := range fields {
		if err := validateConfigText(field.label, field.value); err != nil {
			return err
		}
	}
	for i, model := range pc.Models {
		if err := validateConfigText(fmt.Sprintf("provider %q models[%d]", name, i), model); err != nil {
			return err
		}
	}
	return nil
}

func validateConfigText(name, value string) error {
	if utils.ContainsControl(value) {
		return fmt.Errorf("%s contains control character: %w", name, errs.ErrConfig)
	}
	return nil
}

// DefaultProviderConfig trả về cấu hình credential của provider mặc định.
func (c *Config) DefaultProviderConfig() ProviderConfig {
	if c.Providers == nil {
		return ProviderConfig{}
	}
	return c.Providers[c.Provider]
}

// FillDefaults điền các giá trị mặc định.
func (c *Config) FillDefaults() {
	if c.OutputDir == "" {
		c.OutputDir = filepath.Join("output", "novel")
	}
	if c.Providers == nil {
		c.Providers = make(map[string]ProviderConfig)
	}
	if c.Roles == nil {
		c.Roles = make(map[string]RoleConfig)
	}
	if c.Style == "" {
		c.Style = "default"
	}
	if c.UILang == "" {
		c.UILang = defaultUILang
	}
	if c.OutputLang == "" {
		c.OutputLang = defaultOutputLang
	}
	if c.Budget.Enabled() && c.Budget.WarnRatio == 0 {
		c.Budget.WarnRatio = 0.8
	}
}

// ResolveOutputLang trả về ngôn ngữ output hiệu lực, mặc định vi khi để trống.
// Dùng cho đường truyền prompt (assets.Load) ở Wave 5; tách riêng để FillDefaults
// chưa chạy vẫn có giá trị đúng.
func (c Config) ResolveOutputLang() string {
	if c.OutputLang == "" {
		return defaultOutputLang
	}
	return c.OutputLang
}

// ContextWindowSource đánh dấu nguồn của giá trị window, dùng cho log/chẩn đoán.
type ContextWindowSource string

const (
	CtxWindowConfig   ContextWindowSource = "config"   // context_window trong file cấu hình chỉ định tường minh
	CtxWindowRegistry ContextWindowSource = "registry" // Trúng baseline OpenRouter
	CtxWindowDefault  ContextWindowSource = "default"  // Dự phòng (proxy tuỳ biến/model chưa biết)
)

// ResolveContextWindow phân giải window hiệu lực dùng cho nén context window, theo thứ tự ưu tiên:
//  1. ContextWindow trong file cấu hình > 0 → dùng trực tiếp (ưu tiên cao nhất, có thể vượt window thật của model)
//  2. models.DefaultRegistry tra theo tên model (baseline OpenRouter + làm mới mỗi 24h)
//  3. Dự phòng DefaultContextWindow (proxy tuỳ biến / model chưa biết)
//
// Lưu ý: giá trị trả về chỉ dùng để tính ngưỡng nén, không thu nhỏ độ dài request thật mà LLM API có thể gửi.
func (c Config) ResolveContextWindow(modelName string) (int, ContextWindowSource) {
	if c.ContextWindow > 0 {
		return c.ContextWindow, CtxWindowConfig
	}
	if rw := models.DefaultRegistry().ResolveContextWindow(modelName); rw > 0 {
		return rw, CtxWindowRegistry
	}
	return DefaultContextWindow, CtxWindowDefault
}

// ResolveThinking trả về chuỗi gốc cường độ tư duy có hiệu lực cho một role (off/minimal/low/medium/high/xhigh/max hoặc rỗng).
// Thứ tự ưu tiên: mức role Roles[role].Thinking → mặc định tầng đỉnh Thinking → "" (không override, giữ mặc định model/provider).
// role rỗng hoặc "default" thì lấy thẳng mặc định tầng đỉnh. Tính hợp lệ của giá trị do agents.ParseThinkingLevel canh giữ.
func (c Config) ResolveThinking(role string) string {
	if role != "" && role != "default" {
		if rc, ok := c.Roles[role]; ok && rc.Thinking != "" {
			return rc.Thinking
		}
	}
	return c.Thinking
}

// LogContextWindowChoice in ra quyết định window của một role. Khi source=default thì phát Warn nhắc rằng
// model này chưa trúng trong registry (OpenRouter cũng không thu nạp), nén context window về sau sẽ kích hoạt theo window
// dự phòng — nếu window thật của model lớn hơn, có thể chỉ định tường minh bằng context_window trong file cấu hình để tránh bị nén sớm, mất lịch sử.
func LogContextWindowChoice(role, model string, window int, source ContextWindowSource) {
	attrs := []any{"module", "context", "role", role, "model", model, "window", window, "source", source}
	switch source {
	case CtxWindowDefault:
		slog.Warn(i18n.T("log.config.model_unrecognized_fallback"), attrs...)
	case CtxWindowConfig:
		slog.Info(i18n.T("log.config.context_window_from_config"), attrs...)
	default:
		slog.Info(i18n.T("log.config.context_window"), attrs...)
	}
}

// CandidateModels trả về danh sách model có thể chuyển đổi dưới một provider.
// Ưu tiên dùng models do provider khai báo tường minh; đồng thời bổ sung các model của provider đó đã xuất hiện trong cấu hình hiện tại.
func (c Config) CandidateModels(provider string) []string {
	if provider == "" {
		return nil
	}

	seen := make(map[string]bool)
	models := make([]string, 0, 4)
	add := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			return
		}
		seen[model] = true
		models = append(models, model)
	}

	if pc, ok := c.Providers[provider]; ok {
		for _, model := range pc.Models {
			add(model)
		}
	}
	if c.Provider == provider {
		add(c.ModelName)
	}
	for _, rc := range c.Roles {
		if rc.Provider == provider {
			add(rc.Model)
		}
		for _, fallback := range rc.Fallbacks {
			if fallback.Provider == provider {
				add(fallback.Model)
			}
		}
	}
	return models
}

func (c Config) validateModelRef(owner string, ref ModelRef) error {
	if ref.Provider == "" || ref.Model == "" {
		return fmt.Errorf("%s must have both provider and model: %w", owner, errs.ErrConfig)
	}

	pc, ok := c.Providers[ref.Provider]
	if !ok {
		return fmt.Errorf("%s references provider %q which is not configured: %w", owner, ref.Provider, errs.ErrConfig)
	}
	if pc.RequiresAPIKey(ref.Provider) && pc.APIKey == "" {
		return fmt.Errorf("%s references provider %q which has no api_key: %w", owner, ref.Provider, errs.ErrConfig)
	}
	return nil
}
