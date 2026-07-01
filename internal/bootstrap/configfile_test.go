package bootstrap

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/voocel/ainovel-cli/internal/errs"
)

const validGlobal = `{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": { "openrouter": { "api_key": "sk-test-123456" } }
}`

// writeGlobal ghi cấu hình toàn cục dưới HOME cô lập, và trả về HOME đó.
func writeGlobal(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".ainovel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o644); err != nil {
			t.Fatalf("write global: %v", err)
		}
	}
	return home
}

// writeProjectConfig ghi cấu hình mức project dưới ./.ainovel/ của thư mục làm việc hiện tại.
// Trước khi gọi cần t.Chdir tới thư mục đích.
func writeProjectConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(".ainovel", 0o755); err != nil {
		t.Fatalf("mkdir .ainovel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(".ainovel", "config.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}
}

// Căn nguyên 3: ./.ainovel/config.json mức project tồn tại nhưng JSON hỏng, phải báo lỗi, không được nuốt im lặng rồi lùi về toàn cục.
func TestLoadConfig_CorruptProjectFailsLoud(t *testing.T) {
	writeGlobal(t, validGlobal)
	proj := t.TempDir()
	t.Chdir(proj)
	// Chép tay ví dụ dư một dấu phẩy đuôi — JSON hỏng phổ biến nhất.
	writeProjectConfig(t, `{ "model": "x", }`)

	if _, err := LoadConfig(""); err == nil {
		t.Fatal("坏的 ./.ainovel/config.json 应当报错，却被静默忽略了")
	}
}

// Toàn cục là nền ưu tiên thấp nhất: file hỏng không được chặn override --config ưu tiên cao hơn (guard hồi quy —
// bản trước lỡ để toàn cục cũng fail-loud, khiến người dùng có "global hỏng + --config hợp lệ" bị file không liên quan chặn lại).
func TestLoadConfig_CorruptGlobalDoesNotBlockOverride(t *testing.T) {
	writeGlobal(t, `{ not json`)
	proj := t.TempDir()
	t.Chdir(proj)
	good := filepath.Join(proj, "good.json")
	if err := os.WriteFile(good, []byte(validGlobal), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}

	cfg, err := LoadConfig(good)
	if err != nil {
		t.Fatalf("坏全局不应阻断有效 --config，得到: %v", err)
	}
	if cfg.Provider != "openrouter" {
		t.Errorf("应使用 --config 的值，得到 provider=%q", cfg.Provider)
	}
}

// File không tồn tại là tình huống bình thường (bản portable/lần đầu), không được báo lỗi.
func TestLoadConfig_MissingFilesNoError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // ~/.ainovel/config.json 不存在
	t.Chdir(t.TempDir())   // cũng không có ./.ainovel/config.json

	if _, err := LoadConfig(""); err != nil {
		t.Fatalf("缺失配置文件不应报错，得到: %v", err)
	}
}

// Đường bình thường: toàn cục + mức project hợp nhất có hiệu lực.
func TestLoadConfig_ValidMergeWorks(t *testing.T) {
	writeGlobal(t, validGlobal)
	proj := t.TempDir()
	t.Chdir(proj)
	writeProjectConfig(t, `{
  "model": "google/gemini-2.5-pro",
  "thinking": "high",
  "roles": {
    "writer": {
      "provider": "openrouter",
      "model": "google/gemini-2.5-flash",
      "thinking": "low"
    }
  }
}`)

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("有效配置不应报错: %v", err)
	}
	if cfg.Provider != "openrouter" {
		t.Errorf("provider 应保留全局值 openrouter，得到 %q", cfg.Provider)
	}
	if cfg.ModelName != "google/gemini-2.5-pro" {
		t.Errorf("model 应被项目级覆盖，得到 %q", cfg.ModelName)
	}
	if cfg.Thinking != "high" {
		t.Errorf("thinking 应被项目级覆盖，得到 %q", cfg.Thinking)
	}
	if got := cfg.Roles["writer"].Thinking; got != "low" {
		t.Errorf("roles.writer.thinking 应被项目级覆盖，得到 %q", got)
	}
}

func TestMergeConfig_ProviderExtraFields(t *testing.T) {
	base := Config{
		Provider:  "openrouter",
		ModelName: "google/gemini-2.5-flash",
		Providers: map[string]ProviderConfig{
			"openrouter": {
				APIKey: "sk-test-123456",
				ExtraBody: map[string]any{
					"temperature": 0.8,
				},
				Extra: map[string]any{
					"user_agent": "base-client/1.0",
				},
			},
		},
	}
	overlay := Config{
		Providers: map[string]ProviderConfig{
			"openrouter": {
				BaseURL: "https://proxy.example.com/v1",
				ExtraBody: map[string]any{
					"min_p": 0.05,
				},
				Extra: map[string]any{
					"user_agent": "override-client/1.0",
					"headers": map[string]any{
						"X-Custom-Client": "ainovel",
					},
				},
			},
		},
	}

	cfg := mergeConfig(base, overlay)
	pc := cfg.Providers["openrouter"]
	if pc.APIKey != "sk-test-123456" {
		t.Fatalf("APIKey = %q, want inherited key", pc.APIKey)
	}
	if pc.BaseURL != "https://proxy.example.com/v1" {
		t.Fatalf("BaseURL = %q, want overlay URL", pc.BaseURL)
	}
	if _, ok := pc.ExtraBody["temperature"]; ok {
		t.Fatalf("ExtraBody should be replaced by overlay, got %#v", pc.ExtraBody)
	}
	if got := pc.ExtraBody["min_p"]; got != 0.05 {
		t.Fatalf("ExtraBody[min_p] = %#v, want 0.05", got)
	}
	if got := pc.Extra["user_agent"]; got != "override-client/1.0" {
		t.Fatalf("Extra[user_agent] = %#v, want override-client/1.0", got)
	}
	headers, ok := pc.Extra["headers"].(map[string]any)
	if !ok {
		t.Fatalf("Extra[headers] missing or invalid: %#v", pc.Extra["headers"])
	}
	if got := headers["X-Custom-Client"]; got != "ainovel" {
		t.Fatalf("Extra.headers[X-Custom-Client] = %#v, want ainovel", got)
	}
}

// mergeConfig phải truyền ui_lang/output_lang từ overlay; thiếu clause là âm thầm
// nuốt override (cùng lớp bug issue #37). Đồng thời giữ giá trị base khi overlay rỗng.
func TestMergeConfig_LangFields(t *testing.T) {
	base := Config{UILang: "vi", OutputLang: "vi"}
	overlay := Config{UILang: "en", OutputLang: "en"}
	if got := mergeConfig(base, overlay); got.UILang != "en" || got.OutputLang != "en" {
		t.Fatalf("overlay lang bị nuốt: ui=%q out=%q, want en/en", got.UILang, got.OutputLang)
	}
	if got := mergeConfig(base, Config{}); got.UILang != "vi" || got.OutputLang != "vi" {
		t.Fatalf("overlay rỗng không được ghi đè base: ui=%q out=%q, want vi/vi", got.UILang, got.OutputLang)
	}
}

// Căn nguyên 2 (tái hiện cốt lõi issue #37): mức project override provider nhưng không khai báo credential providers tương ứng,
// ValidateBase phải báo lỗi config (thay vì cho qua rồi sập ở chỗ sâu hơn).
func TestValidateBase_ProviderOverrideWithoutCredentials(t *testing.T) {
	cfg := Config{
		Provider:  "mimo",
		ModelName: "mimo-v2.5-pro",
		Providers: map[string]ProviderConfig{
			"openrouter": {APIKey: "sk-test-123456"},
		},
	}
	cfg.FillDefaults()
	err := cfg.ValidateBase()
	if err == nil {
		t.Fatal("provider 缺凭证应报错")
	}
	if !errors.Is(err, errs.ErrConfig) {
		t.Errorf("应包装 errs.ErrConfig，得到: %v", err)
	}
}

// Ví dụ tích hợp sẵn (config.example.jsonc qua go:embed) phải tự nhất quán: sau khi bỏ comment là JSON hợp lệ,
// con trỏ provider tầng đỉnh không treo lửng, và phải chỉ rõ tư duy "con trỏ" — nó là khuôn mẫu người dùng chép theo, tự nó hỏng là hại người.
func TestExampleConfigIsValidAndSelfConsistent(t *testing.T) {
	if exampleConfig == "" {
		t.Fatal("go:embed 未生效，exampleConfig 为空")
	}
	var cfg Config
	if err := json.Unmarshal(stripJSONComments([]byte(exampleConfig)), &cfg); err != nil {
		t.Fatalf("内置示例去注释后不是合法 JSON（用户照抄即坑）: %v", err)
	}
	if cfg.Provider == "" || cfg.ModelName == "" {
		t.Fatal("示例应给出默认 provider/model")
	}
	if _, ok := cfg.Providers[cfg.Provider]; !ok {
		t.Errorf("示例顶层 provider %q 未指向 providers 中的条目——指针正面样板自己悬空了", cfg.Provider)
	}
	if !contains(exampleConfig, "指针") {
		t.Error("示例应点破“provider 是指针”——别让 #37 的认知陷阱回潮")
	}
}

func TestWriteStartupError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := WriteStartupError("boom: provider not configured")
	if path == "" {
		t.Fatal("应返回落盘路径")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 last-error.log: %v", err)
	}
	if want := "boom: provider not configured"; !contains(string(data), want) {
		t.Errorf("日志应包含 %q，实际: %s", want, data)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
