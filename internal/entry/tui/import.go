package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/host/imp"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// importState là trạng thái modal trong thời gian chạy lệnh /import.
//
// Modal tạo lúc bắt đầu import, đi theo luồng sự kiện; xong hoặc lỗi thì giữ trên màn
// chờ người dùng Esc đóng. Esc khi đang chạy kích hoạt hủy (ctx.Cancel), giao cho
// runner thu dọn ở điểm sự kiện kế tiếp.
type importState struct {
	reqID      int
	source     string
	stage      imp.Stage
	current    int
	total      int
	startedAt  time.Time
	finishedAt time.Time
	history    []importLine
	err        error
	done       bool
	cancel     context.CancelFunc
	viewport   viewport.Model
}

type importLine struct {
	at      time.Time
	stage   imp.Stage
	current int
	total   int
	message string
	err     error
}

func newImportState(reqID int, source string, width, height int, cancel context.CancelFunc) *importState {
	boxW, boxH := reportModalSize(width, height)
	contentW := paddedModalContentWidth(boxW)
	vp := viewport.New(contentW, boxH-4)
	s := &importState{
		reqID:     reqID,
		source:    source,
		startedAt: time.Now(),
		stage:     imp.StageSplitting,
		cancel:    cancel,
		viewport:  vp,
	}
	s.refresh(contentW)
	return s
}

func (s *importState) appendEvent(ev imp.Event, contentW int) {
	s.stage = ev.Stage
	s.current = ev.Current
	s.total = ev.Total
	if ev.Err != nil {
		s.err = ev.Err
	}
	s.history = append(s.history, importLine{
		at: ev.Time, stage: ev.Stage, current: ev.Current, total: ev.Total,
		message: ev.Message, err: ev.Err,
	})
	if ev.Stage == imp.StageDone || ev.Stage == imp.StageError {
		s.done = true
		s.finishedAt = ev.Time
	}
	s.refresh(contentW)
}

func (s *importState) refresh(contentW int) {
	titleStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	mutedStyle := lipgloss.NewStyle().Foreground(colorMuted)
	okStyle := lipgloss.NewStyle().Foreground(colorSuccess)
	errStyle := lipgloss.NewStyle().Foreground(colorError)
	stageStyle := lipgloss.NewStyle().Foreground(colorAccent2)

	var b strings.Builder
	b.WriteString(titleStyle.Render(i18n.T("ui.import.title")))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render(i18n.T("ui.import.source")))
	b.WriteString(s.source)
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(i18n.T("ui.import.started")))
	b.WriteString(formatReportTime(s.startedAt))
	if !s.finishedAt.IsZero() {
		b.WriteString(dimStyle.Render(i18n.T("ui.import.finished")))
		b.WriteString(formatReportTime(s.finishedAt))
	}
	b.WriteString("\n\n")

	// Dòng giai đoạn hiện tại
	b.WriteString(mutedStyle.Render(i18n.T("ui.import.stage")))
	b.WriteString(stageStyle.Render(string(s.stage)))
	if s.total > 0 {
		b.WriteString(mutedStyle.Render(i18n.T("ui.import.progress")))
		if s.current > 0 {
			b.WriteString(fmt.Sprintf("%d/%d", s.current, s.total))
		} else {
			b.WriteString(fmt.Sprintf("0/%d", s.total))
		}
	}
	b.WriteString("\n\n")

	// Nhật ký lịch sử
	b.WriteString(titleStyle.Render(i18n.T("ui.import.flow_log")))
	b.WriteString(" ")
	b.WriteString(dimStyle.Render(i18n.Tf("ui.import.log_count", len(s.history))))
	b.WriteString("\n")
	for _, ln := range s.history {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(ln.at.Format("15:04:05")))
		b.WriteString(" ")
		b.WriteString(stageStyle.Render(string(ln.stage)))
		if ln.total > 0 && ln.current > 0 {
			b.WriteString(mutedStyle.Render(fmt.Sprintf(" %d/%d", ln.current, ln.total)))
		}
		b.WriteString(" ")
		if ln.err != nil {
			b.WriteString(errStyle.Render(ln.message + " — " + ln.err.Error()))
		} else {
			b.WriteString(wrapText(ln.message, contentW))
		}
	}

	// Gợi ý thu dọn
	b.WriteString("\n\n")
	switch {
	case !s.done:
		b.WriteString(dimStyle.Render(i18n.T("ui.import.esc_cancel")))
	case s.err != nil:
		b.WriteString(errStyle.Render(i18n.T("ui.import.failed")))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(i18n.T("ui.import.esc_close")))
	default:
		b.WriteString(okStyle.Render(i18n.T("ui.import.done_autocontinue")))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(i18n.T("ui.import.esc_close_progress")))
	}

	s.viewport.SetContent(b.String())
	if !s.done {
		s.viewport.GotoBottom()
	}
}

func renderImportModal(width, height int, s *importState) string {
	if s == nil {
		return ""
	}
	boxW, boxH := reportModalSize(width, height)
	contentW := paddedModalContentWidth(boxW)
	if s.viewport.Width != contentW {
		s.viewport.Width = contentW
		s.refresh(contentW)
	}
	if s.viewport.Height != boxH-4 {
		s.viewport.Height = boxH - 4
	}

	hint := i18n.T("ui.import.modal_hint")
	modal := renderPaddedModalFrame(boxW, boxH, i18n.T("ui.import.modal_title"), hint,
		strings.Split(s.viewport.View(), "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

func (m Model) handleImportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.importer == nil {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		if !m.importer.done && m.importer.cancel != nil {
			m.importer.cancel()
			return m, nil
		}
		m.importer = nil
		return m, m.textarea.Focus()
	case tea.KeyUp:
		m.importer.viewport.ScrollUp(1)
	case tea.KeyDown:
		m.importer.viewport.ScrollDown(1)
	case tea.KeyPgUp:
		m.importer.viewport.HalfPageUp()
	case tea.KeyPgDown:
		m.importer.viewport.HalfPageDown()
	}
	return m, nil
}

// importEventMsg gửi một imp.Event đơn lẻ.
type importEventMsg struct {
	reqID int
	ev    imp.Event
	ch    <-chan imp.Event // cùng channel tiếp tục lắng nghe bản kế tiếp
}

// startImport khởi động một lần import tiểu thuyết ngoài: phân tích tham số → tạo
// modal state → lắng nghe luồng sự kiện. width/height dùng để khởi tạo viewport;
// hàm cancel gắn trên state cho Esc hủy.
func startImport(rt *host.Host, reqID int, args []string, width, height int) (*importState, tea.Cmd, error) {
	opts, err := parseImportArgs(args)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := rt.ImportFrom(ctx, opts)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	state := newImportState(reqID, opts.SourcePath, width, height, cancel)
	return state, listenImportEvent(reqID, ch), nil
}

func listenImportEvent(reqID int, ch <-chan imp.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return importEventMsg{reqID: reqID, ev: ev, ch: ch}
	}
}

// parseImportArgs phân tích tham số dạng `/import <path> [from=N]`.
func parseImportArgs(args []string) (imp.Options, error) {
	if len(args) == 0 {
		return imp.Options{}, errors.New(i18n.T("ui.import.usage"))
	}
	opts := imp.Options{SourcePath: args[0]}
	for _, a := range args[1:] {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			return imp.Options{}, fmt.Errorf(i18n.T("ui.import.arg_kv"), a)
		}
		switch strings.ToLower(k) {
		case "from":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return imp.Options{}, fmt.Errorf(i18n.T("ui.import.from_int"), v)
			}
			opts.ResumeFrom = n
		default:
			return imp.Options{}, fmt.Errorf(i18n.T("ui.import.unknown_arg"), k)
		}
	}
	return opts, nil
}
