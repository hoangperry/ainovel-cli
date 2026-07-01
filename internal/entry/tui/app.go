package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/logger"
)

// Run khởi động TUI.
// Quy ước phân lớp chế độ khởi động:
// 1. Chế độ nhanh, chế độ đồng sáng tác thuộc "điều phối khởi động";
// 2. Phiên sáng tác chính thức đi vào host.Host;
// 3. Sau này nếu thêm chế độ dùng chung như "viết tiếp tiểu thuyết có sẵn", đều đưa về internal/entry/startup.
func Run(cfg bootstrap.Config, bundle assets.Bundle, version string) error {
	rt, err := host.New(cfg, bundle)
	if err != nil {
		return err
	}
	bridge := newAskUserBridge()
	rt.AskUser().SetHandler(bridge.handler)
	cleanup := logger.SetupFile(rt.Dir(), "tui.log", false)
	defer cleanup()
	defer rt.Close()

	m := NewModel(rt, bridge, version)
	// Không bật mouse reporting toàn cục lúc khởi động: trang chào không cần chuột, tắt
	// reporting giữ được chọn-kéo-copy gốc của terminal. Khi vào bàn làm việc sáng tác
	// (modeRunning) enterRunning mới bật reporting, để hỗ trợ click chuyển panel / cuộn /
	// kéo sidebar.
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
