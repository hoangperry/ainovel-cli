package startup

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// PrepareQuick chỉnh input trực tiếp thành kế hoạch khởi động nhanh có thể vào Engine.
func PrepareQuick(req Request) (Plan, error) {
	prompt := strings.TrimSpace(req.UserPrompt)
	if prompt == "" {
		return Plan{}, fmt.Errorf("prompt is required")
	}
	return Plan{
		Mode:        ModeQuick,
		DisplayName: i18n.T("startup.mode.quick"),
		StartPrompt: host.BuildStartPrompt(prompt),
	}, nil
}
