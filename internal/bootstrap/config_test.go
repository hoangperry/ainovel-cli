package bootstrap

import "testing"

func TestConfigResolveThinking(t *testing.T) {
	cfg := Config{
		Thinking: "low", // mặc định tầng đỉnh
		Roles: map[string]RoleConfig{
			"writer":    {Provider: "p", Model: "m", Thinking: "high"}, // override theo role
			"architect": {Provider: "p", Model: "m"},                   // không có thinking, phải rơi về mặc định
		},
	}

	cases := []struct {
		role string
		want string
	}{
		{"writer", "high"},     // override theo role được ưu tiên
		{"architect", "low"},   // role chưa cấu hình → rơi về mặc định tầng đỉnh
		{"editor", "low"},      // role không tồn tại → mặc định tầng đỉnh
		{"", "low"},            // rỗng → mặc định tầng đỉnh
		{"default", "low"},     // default → mặc định tầng đỉnh
		{"coordinator", "low"}, // chưa cấu hình → mặc định tầng đỉnh
	}
	for _, c := range cases {
		if got := cfg.ResolveThinking(c.role); got != c.want {
			t.Errorf("ResolveThinking(%q) = %q, want %q", c.role, got, c.want)
		}
	}

	// Khi mặc định tầng đỉnh cũng rỗng, role không bị override trả về "" (không override).
	empty := Config{Roles: map[string]RoleConfig{"writer": {Thinking: "xhigh"}}}
	if got := empty.ResolveThinking("editor"); got != "" {
		t.Errorf("空默认下 editor 应返回 \"\"，得 %q", got)
	}
	if got := empty.ResolveThinking("writer"); got != "xhigh" {
		t.Errorf("空默认下 writer 覆盖应生效，得 %q", got)
	}
}
