// Package notify cung cấp kênh cảnh báo cho chế độ chạy không người trông coi.
//
// Định vị kiến trúc (architecture.md §2.3): hành động thuần quan sát — cảnh báo không bao giờ can thiệp vào luồng điều khiển
// (không retry, không đổi phái, không dừng), chỉ "hô" các sự kiện đã có sẵn trong TUI ra ngoài màn hình.
// Send chạy bất đồng bộ, không bao giờ chặn Host, thất bại chỉ ghi slog.
package notify

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/i18n"
)

// Notification chứa toàn bộ thông tin của một cảnh báo.
type Notification struct {
	Kind  string `json:"kind"`  // run_end / repeat / budget
	Level string `json:"level"` // info / warn / error
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Notifier phân phối thông báo theo cấu hình. Giá trị zero không dùng được, phải tạo qua New; nil an toàn (Send noop).
type Notifier struct {
	command string          // khi không rỗng sẽ thay thế kênh system (push điện thoại đi qua đây)
	events  map[string]bool // nil = cho qua mọi kind
	timeout time.Duration
}

// New tạo Notifier. command rỗng thì dùng kênh system tích hợp (macOS osascript /
// Linux notify-send, không tìm thấy lệnh thì lặng lẽ hạ cấp xuống chỉ slog); events không rỗng thì chỉ cho qua các kind được liệt kê.
func New(command string, events []string) *Notifier {
	n := &Notifier{command: strings.TrimSpace(command), timeout: 10 * time.Second}
	if len(events) > 0 {
		n.events = make(map[string]bool, len(events))
		for _, ev := range events {
			n.events[ev] = true
		}
	}
	return n
}

// Send gửi bất đồng bộ một thông báo. Lọc, thực thi, xử lý thất bại đều không ảnh hưởng tới caller.
func (n *Notifier) Send(nt Notification) {
	if !n.allows(nt.Kind) {
		return
	}
	go n.deliver(nt)
}

// allows trả về kind đó có được cho qua hay không (nil Notifier / không nằm trong events thì chặn).
func (n *Notifier) allows(kind string) bool {
	if n == nil {
		return false
	}
	return n.events == nil || n.events[kind]
}

// deliver thực thi đồng bộ một lần gửi (chạy trong goroutine; test có thể gọi trực tiếp để assert đồng bộ).
func (n *Notifier) deliver(nt Notification) {
	defer func() { recover() }()
	ctx, cancel := context.WithTimeout(context.Background(), n.timeout)
	defer cancel()

	var err error
	if n.command != "" {
		err = runCommand(ctx, n.command, nt)
	} else {
		err = runSystem(ctx, nt)
	}
	if err != nil {
		slog.Warn(i18n.T("notify.deliver_failed"), "module", "notify", "kind", nt.Kind, "err", err)
	}
}

// runCommand thực thi lệnh do người dùng cấu hình: các trường truyền qua biến môi trường (một dòng curl, zero dependency, không rủi ro
// injection), đồng thời ghi toàn bộ JSON vào stdin (kịch bản phân phối phức tạp tự parse). Timeout do ctx cưỡng chế kết thúc.
func runCommand(ctx context.Context, command string, nt Notification) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(),
		"NOTIFY_KIND="+nt.Kind,
		"NOTIFY_LEVEL="+nt.Level,
		"NOTIFY_TITLE="+nt.Title,
		"NOTIFY_BODY="+nt.Body,
	)
	payload, _ := json.Marshal(nt)
	cmd.Stdin = strings.NewReader(string(payload))
	return cmd.Run()
}

// runSystem thông báo desktop tích hợp: chỉ phục vụ kịch bản "người đang ngồi bên máy", không tìm thấy lệnh thì lặng lẽ hạ cấp.
func runSystem(ctx context.Context, nt Notification) error {
	switch runtime.GOOS {
	case "darwin":
		script := "display notification " + appleScriptString(nt.Body) + " with title " + appleScriptString(nt.Title)
		return exec.CommandContext(ctx, "osascript", "-e", script).Run()
	case "linux":
		if _, err := exec.LookPath("notify-send"); err != nil {
			slog.Info(i18n.T("notify.degraded_no_send"), "module", "notify", "title", nt.Title, "body", nt.Body)
			return nil
		}
		return exec.CommandContext(ctx, "notify-send", nt.Title, nt.Body).Run()
	default:
		slog.Info(i18n.T("notify.degraded_no_channel"), "module", "notify", "title", nt.Title, "body", nt.Body)
		return nil
	}
}

// appleScriptString bọc văn bản bất kỳ thành một string literal AppleScript.
func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
