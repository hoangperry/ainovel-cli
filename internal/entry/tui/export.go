package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/host/exp"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// exportDoneMsg là kết quả cuối cùng của lệnh /export.
//
// Không như /import đi theo luồng sự kiện: export là IO cục bộ đồng bộ, không có
// tiến độ trung gian; chạy xong trong goroutine rồi gửi lại một lần message này.
type exportDoneMsg struct {
	result *exp.Result
	err    error
}

// startExport phân tích tham số và trả về tea.Cmd.
// Việc export thật chạy trong tea.Cmd (tránh chặn UI), xong thì gửi exportDoneMsg.
func startExport(rt *host.Host, args []string) (tea.Cmd, error) {
	opts, err := parseExportArgs(args)
	if err != nil {
		return nil, err
	}
	return func() tea.Msg {
		// 30s đủ để ghi cục bộ một tiểu thuyết trung-dài; timeout chỉ là phương án chống treo.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := rt.Export(ctx, opts)
		return exportDoneMsg{result: res, err: err}
	}, nil
}

// parseExportArgs phân tích `/export [path] [from=N] [to=M] [--overwrite]`.
//
// Tham số vị trí: nhiều nhất một, làm đường dẫn xuất; mặc định do exp.Run quyết định
// ({novelDir}/{NovelName}.txt).
func parseExportArgs(args []string) (exp.Options, error) {
	var opts exp.Options
	for _, a := range args {
		if a == "--overwrite" {
			opts.Overwrite = true
			continue
		}
		if k, v, ok := strings.Cut(a, "="); ok {
			switch strings.ToLower(k) {
			case "from":
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return exp.Options{}, fmt.Errorf(i18n.T("ui.export.from_int"), v)
				}
				opts.From = n
			case "to":
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 {
					return exp.Options{}, fmt.Errorf(i18n.T("ui.export.to_int"), v)
				}
				opts.To = n
			default:
				return exp.Options{}, fmt.Errorf(i18n.T("ui.export.unknown_arg"), k)
			}
			continue
		}
		if strings.HasPrefix(a, "-") {
			return exp.Options{}, fmt.Errorf(i18n.T("ui.export.unknown_flag"), a)
		}
		if opts.OutPath != "" {
			return exp.Options{}, fmt.Errorf(i18n.T("ui.export.single_path"), a)
		}
		opts.OutPath = a
	}
	return opts, nil
}

// formatExportSuccess render Result thành Summary sự kiện.
func formatExportSuccess(res *exp.Result) string {
	var b strings.Builder
	b.WriteString(i18n.Tf("ui.export.success", res.Chapters, humanBytes(res.Bytes), res.Path))
	if n := len(res.Skipped); n > 0 {
		b.WriteString(i18n.Tf("ui.export.skipped", n, briefIntList(res.Skipped, 5)))
	}
	return b.String()
}

func humanBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

func briefIntList(xs []int, max int) string {
	if len(xs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(xs))
	for i, x := range xs {
		if i >= max {
			parts = append(parts, "...")
			break
		}
		parts = append(parts, strconv.Itoa(x))
	}
	return strings.Join(parts, ",")
}
