package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/voocel/ainovel-cli/internal/i18n"
)

const configDirName = ".ainovel"

// DefaultConfigPath trả về đường dẫn file cấu hình toàn cục ~/.ainovel/config.json.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDirName, "config.json")
}

// DefaultConfigDir trả về đường dẫn thư mục ~/.ainovel; không lấy được home dir thì trả về chuỗi rỗng.
// Chỉ dùng để đọc/ghi file không bắt buộc tồn tại (như cache model), không tự tạo thư mục.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDirName)
}

// configDir trả về đường dẫn thư mục ~/.ainovel, tạo mới nếu chưa tồn tại.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, configDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return dir, nil
}

// projectConfigPath trả về đường dẫn tương đối của file cấu hình mức project ./.ainovel/config.json.
// dotdir mức project soi gương thư mục toàn cục ~/.ainovel/, tái dùng cùng configDirName; phân giải tương đối cwd.
func projectConfigPath() string {
	return filepath.Join(configDirName, "config.json")
}

// LoadConfig tải và hợp nhất cấu hình theo thứ tự ưu tiên:
//  1. ~/.ainovel/config.json (toàn cục)
//  2. ./.ainovel/config.json (override mức project)
//  3. đường dẫn do flagPath chỉ định (ưu tiên cao nhất)
func LoadConfig(flagPath string) (Config, error) {
	var cfg Config

	// 1. Cấu hình toàn cục. Đây là nền ưu tiên thấp nhất, file hỏng hạ cấp thành cảnh báo chứ không chặn — có thể bị
	//    mức project / --config override; thất bại cứng sẽ chặn người dùng có "global hỏng + --config hợp lệ" ở ngoài cửa,
	//    vi phạm ngữ nghĩa "tôi chỉ định rõ cái này" của --config.
	if p := DefaultConfigPath(); p != "" {
		global, found, err := loadOptionalJSON(p)
		switch {
		case err != nil:
			slog.Warn(i18n.T("log.config.global_parse_failed"), "module", "config", "path", p, "err", err)
		case found:
			cfg = global
		}
	}

	// 2. Override mức project. File hỏng thì fail loud: cấu hình người dùng chủ động đặt ở thư mục hiện tại, nuốt im lặng sẽ khiến
	//    "đã cấu hình mà không có hiệu lực" không thể truy vết (issue #37).
	project, found, err := loadOptionalJSON(projectConfigPath())
	if err != nil {
		return cfg, fmt.Errorf(i18n.T("cli.config.project_parse_failed"), err)
	}
	if found {
		cfg = mergeConfig(cfg, project)
	}

	// 3. Override bằng CLI flag
	if flagPath != "" {
		override, err := loadJSONFile(flagPath)
		if err != nil {
			return cfg, fmt.Errorf("load config %s: %w", flagPath, err)
		}
		cfg = mergeConfig(cfg, override)
	}

	return cfg, nil
}

// loadOptionalJSON đọc một file cấu hình tuỳ chọn:
//   - File không tồn tại → (zero, false, nil), bên gọi quyết định dùng giá trị mặc định/tầng trên
//   - File tồn tại nhưng phân giải thất bại → trả về lỗi (không nuốt im lặng nữa — nếu không cấu hình của người dùng
//     "đã cấu hình mà không có hiệu lực" lại không thể truy vết, đúng là căn nguyên của issue #37)
func loadOptionalJSON(path string) (Config, bool, error) {
	cfg, err := loadJSONFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, false, nil
		}
		return Config{}, false, err
	}
	return cfg, true, nil
}

// LoadConfigFile đọc một file cấu hình JSON, hỗ trợ comment dòng //.
// Không hợp nhất gì, chỉ trả về cấu hình của riêng file đó. File không tồn tại thì trả về lỗi.
func LoadConfigFile(path string) (Config, error) {
	return loadJSONFile(path)
}

// loadJSONFile đọc file cấu hình JSON, hỗ trợ comment dòng //.
// File không tồn tại thì trả về lỗi (bên gọi quyết định có bỏ qua hay không).
func loadJSONFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cleaned := stripJSONComments(data)
	var cfg Config
	if err := json.Unmarshal(cleaned, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// mergeConfig hợp nhất overlay lên base. Trường giá trị khác zero thì override, map hợp nhất theo key.
func mergeConfig(base, overlay Config) Config {
	if overlay.Provider != "" {
		base.Provider = overlay.Provider
	}
	if overlay.ModelName != "" {
		base.ModelName = overlay.ModelName
	}
	if overlay.Thinking != "" {
		base.Thinking = overlay.Thinking
	}
	if overlay.Style != "" {
		base.Style = overlay.Style
	}
	if overlay.UILang != "" {
		base.UILang = overlay.UILang
	}
	if overlay.OutputLang != "" {
		base.OutputLang = overlay.OutputLang
	}
	if overlay.ContextWindow > 0 {
		base.ContextWindow = overlay.ContextWindow
	}

	// Providers: key của overlay override key cùng tên của base
	if len(overlay.Providers) > 0 {
		if base.Providers == nil {
			base.Providers = make(map[string]ProviderConfig)
		}
		for k, v := range overlay.Providers {
			existing := base.Providers[k]
			if v.Type != "" {
				existing.Type = v.Type
			}
			if v.APIKey != "" {
				existing.APIKey = v.APIKey
			}
			if v.BaseURL != "" {
				existing.BaseURL = v.BaseURL
			}
			if len(v.Models) > 0 {
				existing.Models = append([]string(nil), v.Models...)
			}
			if len(v.ExtraBody) > 0 {
				existing.ExtraBody = cloneMap(v.ExtraBody)
			}
			if len(v.Extra) > 0 {
				existing.Extra = cloneMap(v.Extra)
			}
			base.Providers[k] = existing
		}
	}

	// Roles: key của overlay override key cùng tên của base
	if len(overlay.Roles) > 0 {
		if base.Roles == nil {
			base.Roles = make(map[string]RoleConfig)
		}
		for k, v := range overlay.Roles {
			existing := base.Roles[k]
			if v.Provider != "" {
				existing.Provider = v.Provider
			}
			if v.Model != "" {
				existing.Model = v.Model
			}
			if len(v.Fallbacks) > 0 {
				existing.Fallbacks = append([]ModelRef(nil), v.Fallbacks...)
			}
			if v.Thinking != "" {
				existing.Thinking = v.Thinking
			}
			base.Roles[k] = existing
		}
	}

	// Budget / Notify: override nguyên khối (budget/cảnh báo mức project là tuyên bố chính sách độc lập, không ghép từng trường với toàn cục)
	if overlay.Budget != (BudgetConfig{}) {
		base.Budget = overlay.Budget
	}
	if overlay.Notify.Enabled != nil || overlay.Notify.Command != "" || len(overlay.Notify.Events) > 0 {
		base.Notify = overlay.Notify
	}

	return base
}

func cloneMap(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// stripJSONComments loại bỏ comment dòng // trong JSON, theo dõi trạng thái dấu ngoặc kép để tránh xoá nhầm nội dung chuỗi.
func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		b := data[i]

		if escaped {
			out = append(out, b)
			escaped = false
			continue
		}

		if inString {
			out = append(out, b)
			if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}

		// Không nằm trong chuỗi
		if b == '"' {
			inString = true
			out = append(out, b)
			continue
		}

		// Phát hiện comment //
		if b == '/' && i+1 < len(data) && data[i+1] == '/' {
			// Nhảy tới cuối dòng
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
			continue
		}

		out = append(out, b)
	}

	return out
}

// WriteStartupError ghi nối lỗi nghiêm trọng trong giai đoạn khởi động vào ~/.ainovel/last-error.log, và trả về
// đường dẫn file đó (best-effort, thất bại thì trả về chuỗi rỗng). Khi double-click khởi động, cửa sổ console sẽ đóng ngay
// theo tiến trình thoát, lỗi loé qua trong chớp mắt; ghi xuống đĩa là con đường duy nhất để loại người dùng này truy vết về sau.
func WriteStartupError(msg string) string {
	dir := DefaultConfigDir()
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, "last-error.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), msg); err != nil {
		return ""
	}
	return path
}

// SaveConfig ghi cấu hình ra đường dẫn chỉ định (định dạng JSON, thụt lề cho đẹp).
func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
