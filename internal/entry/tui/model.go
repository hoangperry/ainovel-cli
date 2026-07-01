package tui

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/tools"
	"github.com/voocel/ainovel-cli/internal/utils"
)

const maxEvents = 500

// maxStreamRounds giới hạn số vòng panel stream giữ lại. Mỗi LLM call kết thúc kích hoạt
// một streamClear mở vòng mới, một chương writer khoảng 3~5 vòng (agent header / thinking /
// draft / commit), 32 vòng tương đương xem lại output stream của 6~10 chương gần nhất. Nội dung
// chương đã commit lưu xuống đĩa ở store/drafts, vượt thì bỏ để tránh mỗi token delta kích hoạt
// render lại O(toàn văn). Trần bộ nhớ ổn định khoảng 512KB, thấp hơn nhiều ngưỡng giật.
const maxStreamRounds = 32

type focusPane int

const (
	focusEvents focusPane = iota
	focusStream
	focusDetail
	focusState // sidebar trạng thái bên trái (cuộn được)

	focusPaneCount // tổng số focus, dùng cho xoay vòng Tab
)

type appMode int

const (
	modeNew     appMode = iota // chờ người dùng nhập nhu cầu tiểu thuyết
	modeRunning                // đang sáng tác (gồm cả dừng do lỗi, input có thể khôi phục)
	modeDone                   // sáng tác hoàn thành
)

// Chuỗi frame spinner dùng chung cho topbar / hoạt động stream (bubbles.Spinner.MiniDot).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Chuỗi frame spinner riêng cho dòng "đang chạy" của luồng sự kiện (bubbles.Spinner.Dot).
// 7 chấm + 1 khe xoay theo chiều kim đồng hồ trên lưới 3×3, nhìn như vòng tròn loading hoàn chỉnh.
// Dùng chỉ số frame riêng + tick nhanh hơn, không ảnh hưởng nhịp animation của topbar và sao.
var toolSpinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// Model là trạng thái cấp cao nhất của TUI.
type Model struct {
	runtime        *host.Host
	askBridge      *askUserBridge
	askState       *askUserState
	cocreate       *cocreateState
	help           *helpState
	modelSwitch    *modelSwitchState
	report         *reportState
	version        string
	importer       *importState
	importSeq      int
	simulator      *simulationState
	simSeq         int
	compItems      []commandPaletteItem
	compIdx        int
	compActive     bool
	snapshot       host.UISnapshot
	events         []host.Event
	eventIndex     map[string]int   // event.ID → chỉ số trong m.events; cập nhật tại chỗ khi sự kiện loại gọi tới
	viewport       viewport.Model   // viewport luồng sự kiện
	streamVP       viewport.Model   // viewport output stream
	detailVP       viewport.Model   // viewport chi tiết bên phải
	stateVP        viewport.Model   // viewport sidebar trạng thái bên trái (cuộn được)
	streamBuf      *strings.Builder // buffer tích lũy văn bản stream
	streamRounds   []string
	textarea       textarea.Model
	width          int
	height         int
	autoScroll     bool
	streamScroll   bool      // panel stream tự động theo dõi
	streamDirty    bool      // streamRounds có delta chưa làm tươi; được streamFlushTick gộp 60fps
	lastKeyAt      time.Time // thời điểm phím không phải Enter lần trước; KeyEnter tiết lưu chống dòng \n dán kích hoạt submit nhầm
	inputHistory   []string  // lịch sử input đã submit (khử trùng: kề nhau không lặp)
	historyIdx     int       // chỉ số duyệt hiện tại; == len(inputHistory) nghĩa là "chưa duyệt, đang sửa bản nháp"
	historyDraft   string    // bản nháp lưu trước khi vào duyệt lịch sử, khôi phục khi về cuối
	focusPane      focusPane
	hoverPane      focusPane
	hoverActive    bool
	mode           appMode
	startupMode    startupMode
	cocreateSeq    int
	reportSeq      int
	err            error
	spinnerIdx     int
	toolSpinnerIdx int  // chỉ số frame riêng của dòng đang chạy ở luồng sự kiện (150ms tick, không ảnh hưởng topbar/sao)
	cursorIdx      int  // chỉ số frame con trỏ stream (tick riêng)
	streamRound    int  // đếm số vòng output stream
	quitPending    bool // xác nhận thoát bằng hai lần Ctrl+C
	abortPending   bool // tạm dừng thủ công đang chờ Done quay lại
	mouseOff       bool // true thì đã tắt mouse reporting, cho người dùng kéo-chọn-copy gốc; chuyển lại thì khôi phục
}

// NewModel tạo TUI Model.
func NewModel(rt *host.Host, bridge *askUserBridge, version string) Model {
	ta := textarea.New()
	ta.Placeholder = placeholderForNewMode(startupModeQuick)
	ta.CharLimit = 2000
	ta.SetHeight(1)
	// MaxHeight=6 để input siêu dài tự wrap theo chiều rộng hiển thị thành nhiều dòng (trần thị giác 6 dòng).
	ta.MaxHeight = 6
	ta.ShowLineNumbers = false
	ta.Focus()

	// Mặc định Enter không xuống dòng (do handleEnterKey submit);
	// xuống dòng chủ động rebind sang ctrl+j (unix \n) và alt+enter (thói quen GUI).
	// Lớp giao thức terminal không phân biệt được Shift+Enter với Enter, nên không hỗ trợ Shift+Enter.
	ta.KeyMap.InsertNewline.SetKeys("ctrl+j", "alt+enter")

	vp := viewport.New(80, 20)
	vp.SetContent("")

	svp := viewport.New(80, 10)
	svp.SetContent("")

	dvp := viewport.New(40, 20)
	dvp.SetContent("")

	stvp := viewport.New(32, 20)
	stvp.SetContent("")

	return Model{
		runtime:      rt,
		askBridge:    bridge,
		version:      strings.TrimSpace(version),
		autoScroll:   true,
		streamScroll: true,
		mode:         modeNew,
		startupMode:  startupModeQuick,
		textarea:     ta,
		viewport:     vp,
		streamVP:     svp,
		detailVP:     dvp,
		stateVP:      stvp,
		streamBuf:    &strings.Builder{},
		eventIndex:   make(map[string]int),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		listenEvents(m.runtime),
		listenAskUser(m.askBridge),
		listenDone(m.runtime),
		listenStream(m.runtime),
		tickSnapshot(m.runtime),
		bootstrapRuntime(m.runtime),
		tickSpinner(),
		tickToolSpinner(),
		tickCursor(),
		tickStreamFlush(),
	)
}

func (m *Model) paneAtMouse(x, y int) (focusPane, bool) {
	if m.width == 0 || m.height == 0 {
		return focusEvents, false
	}

	topH, _, bodyH := m.layoutHeights()
	if bodyH < 1 {
		return focusEvents, false
	}

	bodyStartY := topH
	bodyEndY := topH + bodyH
	if y < bodyStartY || y >= bodyEndY {
		return focusEvents, false
	}

	leftW := m.sidebarWidth()
	rightW := m.detailWidth()
	centerStartX := leftW
	rightStartX := m.width - rightW

	if x >= rightStartX {
		return focusDetail, true
	}
	if x < centerStartX {
		return focusState, true
	}

	eventH, _ := m.splitHeights(bodyH)
	if y-bodyStartY < eventH {
		return focusEvents, true
	}
	return focusStream, true
}

func (m *Model) paneHighlighted(pane focusPane) bool {
	if m.focusPane == pane {
		return true
	}
	return m.hoverActive && m.hoverPane == pane
}

// hasRunningEvent có tồn tại sự kiện loại gọi chưa xong (spinner vẫn quay) hay không.
// toolSpinnerTick dùng cái này để phán có đáng render lại không: khi không có sự kiện running thì
// frame spinner không ảnh hưởng output, cả refreshEventViewport là việc vô ích chắc chắn.
func (m *Model) hasRunningEvent() bool {
	for i := range m.events {
		if m.events[i].Running() {
			return true
		}
	}
	return false
}

// flushStreamIfDirty render streamRounds tích lũy vào viewport; mark là đã làm tươi.
// Trả về có thật sự làm tươi không, tiện cho caller quyết định có GotoBottom không.
func (m *Model) flushStreamIfDirty() bool {
	if !m.streamDirty {
		return false
	}
	m.refreshStreamViewport()
	m.streamDirty = false
	return true
}

// refreshEventViewport render lại nội dung luồng sự kiện và đặt viewport.
func (m *Model) refreshEventViewport() {
	centerW := m.eventFlowWidth()
	content := renderEventContent(m.events, centerW, m.toolSpinnerIdx)
	if activity := renderEventActivity(m.snapshot, m.spinnerIdx, centerW); activity != "" {
		if strings.TrimSpace(content) != "" {
			content += "\n" + activity
		} else {
			content = activity
		}
	}
	m.viewport.SetContent(content)
	if m.autoScroll {
		m.viewport.GotoBottom()
	}
}

func (m *Model) refreshStreamViewport() {
	cursor := ""
	if m.snapshot.IsRunning {
		cursor = renderStreamCursor(m.cursorIdx)
	}
	m.streamVP.SetContent(renderStreamContent(m.streamRounds, m.streamVP.Width, cursor))
}

func (m *Model) refreshDetailViewport() {
	rightW := m.detailWidth()
	if rightW <= 4 {
		return
	}
	m.detailVP.SetContent(renderDetailContent(m.snapshot, rightW-4))
}

// refreshStateViewport làm tươi nội dung sidebar trạng thái bên trái vào viewport.
// Nội dung sidebar thuần phái sinh từ snapshot, nên khi snapshot hay kích thước đổi đều phải làm tươi lại.
func (m *Model) refreshStateViewport() {
	leftW := m.sidebarWidth()
	if leftW <= 4 {
		return
	}
	m.stateVP.SetContent(renderStateContent(m.snapshot, leftW-4))
}

// updateViewportSize cập nhật kích thước viewport theo kích thước cửa sổ hiện tại.
func (m *Model) updateViewportSize() {
	centerW := m.eventFlowWidth()
	rightW := m.detailWidth()
	bodyH := m.bodyHeight()
	eventH, streamH := m.splitHeights(bodyH)
	m.viewport.Width = centerW - 2
	m.viewport.Height = eventH - 1 // -1 cho dòng header của event panel
	m.streamVP.Width = centerW - 2
	m.streamVP.Height = streamH - 1 // -1 cho dòng header của stream panel
	m.detailVP.Width = rightW - 2
	m.detailVP.Height = bodyH
	leftW := m.sidebarWidth()
	m.stateVP.Width = max(1, leftW-2)
	m.stateVP.Height = max(1, bodyH-2) // -2 cho khoảng trắng trên dưới của Padding(1,1) thanh trạng thái
}

// splitHeights tính phân bổ chiều cao cho luồng sự kiện và output stream.
func (m *Model) splitHeights(bodyH int) (eventH, streamH int) {
	eventH = bodyH * 40 / 100
	if eventH < 3 {
		eventH = 3
	}
	streamH = bodyH - eventH - 1 // -1 cho đường phân cách
	if streamH < 3 {
		streamH = 3
	}
	return
}

func (m *Model) inputWidth() int {
	if m.width == 0 {
		return 60
	}
	return m.width - 6 // border + padding + ký hiệu prompt "❯ "
}

func (m *Model) currentInputWidth() int {
	if m.cocreate != nil {
		return coCreateInputWidth(m.width, m.height)
	}
	return m.inputWidth()
}

// refitTextareaHeight ước lượng số dòng thị giác theo nội dung hiện tại, SetHeight động.
// Dòng thị giác = tổng số dòng logic (cắt theo \n) mỗi đoạn sau khi wrap theo chiều rộng.
// Kết hợp MaxHeight=6 để đạt "nội dung siêu dài/xuống dòng chủ động tự hiển thị nhiều dòng, tối đa 6 dòng".
func (m *Model) refitTextareaHeight() {
	w := m.textarea.Width()
	if w <= 0 {
		return
	}
	// Ở chế độ đồng sáng tác input cố định 1 dòng: nội dung nhiều dòng của textarea sẽ
	// được chính textarea cuộn theo con trỏ để hiển thị. Nếu không inputBox cao theo nội
	// dung, làm conversation cột trái co lại, input trôi theo phương dọc, phá ổn định bố cục.
	if m.cocreate != nil {
		m.textarea.SetHeight(1)
		return
	}
	text := m.textarea.Value()
	if text == "" {
		m.textarea.SetHeight(1)
		return
	}
	// Trừ 2 cột dư (prompt symbol + cursor bên trong textarea), dư 1 dòng chấp nhận được.
	contentW := w - 2
	if contentW < 1 {
		contentW = 1
	}
	total := 0
	for line := range strings.SplitSeq(text, "\n") {
		lw := lipgloss.Width(line)
		if lw == 0 {
			total++
			continue
		}
		total += (lw + contentW - 1) / contentW
	}
	if total < 1 {
		total = 1
	}
	m.textarea.SetHeight(total) // SetHeight bên trong clamp theo MaxHeight
}

// resizeTextarea đặt đồng bộ chiều rộng và chiều cao dựa trên nội dung.
// Thay cho các lời gọi SetWidth(currentInputWidth()) rải rác, đảm bảo khi chiều rộng đổi thì chiều cao theo.
func (m *Model) resizeTextarea() {
	m.textarea.SetWidth(m.currentInputWidth())
	m.refitTextareaHeight()
}

// maxInputHistory giới hạn độ dài lịch sử, tránh bộ nhớ tăng trong phiên dài.
const maxInputHistory = 200

// pushInputHistory nối nội dung submit thành công vào lịch sử, khử trùng kề nhau. Đồng thời reset chỉ số duyệt.
func (m *Model) pushInputHistory(text string) {
	if text == "" {
		return
	}
	if n := len(m.inputHistory); n == 0 || m.inputHistory[n-1] != text {
		m.inputHistory = append(m.inputHistory, text)
		if len(m.inputHistory) > maxInputHistory {
			m.inputHistory = m.inputHistory[len(m.inputHistory)-maxInputHistory:]
		}
	}
	m.historyIdx = len(m.inputHistory)
	m.historyDraft = ""
}

// tryHistoryUp đi tới mục lịch sử sớm hơn; trả về có xử lý phím không.
// Lần đầu vào duyệt lịch sử thì lưu nội dung textarea hiện tại làm draft, về cuối thì khôi phục.
// Caller phải tự phán trong tình huống nhiều dòng có nên né hay không (để textarea xử lý di chuyển con trỏ trong dòng).
func (m *Model) tryHistoryUp() bool {
	if len(m.inputHistory) == 0 || m.historyIdx <= 0 {
		return false
	}
	if m.historyIdx == len(m.inputHistory) {
		m.historyDraft = m.textarea.Value()
	}
	m.historyIdx--
	m.textarea.SetValue(m.inputHistory[m.historyIdx])
	m.textarea.CursorEnd()
	m.refitTextareaHeight()
	return true
}

// tryHistoryDown đi tới mục lịch sử mới hơn; tới cuối thì khôi phục draft.
func (m *Model) tryHistoryDown() bool {
	if m.historyIdx >= len(m.inputHistory) {
		return false
	}
	m.historyIdx++
	if m.historyIdx == len(m.inputHistory) {
		m.textarea.SetValue(m.historyDraft)
		m.historyDraft = ""
	} else {
		m.textarea.SetValue(m.inputHistory[m.historyIdx])
	}
	m.textarea.CursorEnd()
	m.refitTextareaHeight()
	return true
}

// textareaIsMultiline nội dung textarea hiện tại có chứa xuống dòng chủ động không; dùng để quyết định ↑↓ là duyệt lịch sử hay di chuyển trong dòng.
func (m *Model) textareaIsMultiline() bool {
	return strings.Contains(m.textarea.Value(), "\n")
}

// inputHints sinh văn bản gợi ý dưới cùng theo trạng thái hiện tại.
// Cuối cùng nối thêm copySuffix thống nhất, để người dùng ở mọi trạng thái không khẩn cấp đều thấy cách chọn-copy;
// khi chuột đã tắt thì hiển thị gợi ý chữ đỏ nổi bật, nhắc bấm lại để khôi phục tương tác chuột.
func (m *Model) inputHints() string {
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	if m.quitPending {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Bold(true).Render(i18n.T("ui.misc.quit_again"))
	}
	// Trang chào (modeNew) không bật mouse reporting, kéo gốc của terminal là copy được, không cần gợi ý Ctrl+R;
	// chỉ bàn làm việc mới bật reporting, copy cần Ctrl+R tắt tạm thời.
	suffix := i18n.T("ui.hint.copy_suffix")
	if m.mode == modeNew {
		suffix = ""
	}
	if m.mouseOff && m.mode != modeNew {
		// Bàn làm việc chuyển thủ công sang chọn-copy: dùng màu nhấn gợi ý đang ở trạng thái "tự do kéo-chọn", bấm Ctrl+R để khôi phục
		return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).
			Render(i18n.T("ui.hint.copy_mode"))
	}
	if m.cocreate != nil {
		scrollHint := i18n.T("ui.hint.scroll_conv")
		if m.cocreate.focusPrompt {
			scrollHint = i18n.T("ui.hint.scroll_prompt")
		}
		switch {
		case m.cocreate.awaiting:
			return dimStyle.Render(i18n.T("ui.hint.cocreate_await") + scrollHint + suffix)
		case m.cocreate.canStart():
			startLabel := i18n.T("ui.hint.start_create")
			if m.cocreate.stage {
				startLabel = i18n.T("ui.hint.apply_continue")
			}
			return dimStyle.Render(i18n.Tf("ui.hint.cocreate_send_start", startLabel) + scrollHint + suffix)
		default:
			return dimStyle.Render(i18n.T("ui.hint.cocreate_send") + scrollHint + suffix)
		}
	}
	if m.mode == modeNew {
		if m.startupMode == startupModeQuick {
			return dimStyle.Render(i18n.T("ui.hint.new_quick") + suffix)
		}
		return dimStyle.Render(i18n.T("ui.hint.new_cocreate") + suffix)
	}
	switch m.snapshot.RuntimeState {
	case "pausing":
		return dimStyle.Render(i18n.T("ui.hint.pausing") + suffix)
	case "paused":
		return dimStyle.Render(i18n.T("ui.hint.paused") + suffix)
	}
	return dimStyle.Render(i18n.T("ui.hint.running") + suffix)
}

func (m *Model) eventFlowWidth() int {
	if m.width == 0 {
		return 80
	}
	leftW := m.sidebarWidth()
	rightW := m.detailWidth()
	return m.width - leftW - rightW
}

func (m *Model) sidebarWidth() int {
	if m.width == 0 {
		return 32
	}
	return m.width * 23 / 100
}

func (m *Model) detailWidth() int {
	if m.width == 0 {
		return 40
	}
	return m.width * 27 / 100
}

func (m *Model) bodyHeight() int {
	_, _, bodyH := m.layoutHeights()
	return bodyH
}

func (m *Model) currentSpinnerFrame() string {
	if !m.snapshot.IsRunning {
		return ""
	}
	return spinnerFrames[m.spinnerIdx%len(spinnerFrames)]
}

func (m *Model) outputDir() string {
	if m.runtime == nil {
		return ""
	}
	return m.runtime.Dir()
}

func defaultSteerPlaceholder() string {
	return i18n.T("ui.placeholder.steer")
}

func (m *Model) syncRuntimePlaceholder() {
	if m.mode != modeRunning || m.cocreate != nil {
		return
	}
	switch m.snapshot.RuntimeState {
	case "completed":
		m.textarea.Placeholder = i18n.T("ui.placeholder.completed")
	case "pausing":
		m.textarea.Placeholder = i18n.T("ui.placeholder.pausing")
	case "paused":
		m.textarea.Placeholder = i18n.T("ui.placeholder.paused_continue")
	default:
		if !m.snapshot.IsRunning {
			m.textarea.Placeholder = i18n.T("ui.placeholder.interrupted")
		} else {
			m.textarea.Placeholder = defaultSteerPlaceholder()
		}
	}
}

func (m *Model) renderBottomBar() string {
	inputBox := renderInputBox(
		m.textarea.View(),
		m.inputHints(),
		m.snapshot,
		m.outputDir(),
		m.width,
	)
	if m.mode != modeNew || m.cocreate != nil {
		return inputBox
	}
	return renderStartupModeBar(m.width, m.startupMode) + "\n" + inputBox
}

func (m *Model) layoutHeights() (topH, inputH, bodyH int) {
	if m.width == 0 || m.height == 0 {
		return 1, 4, 20
	}
	topH = lipgloss.Height(renderTopBar(m.snapshot, m.width, m.currentSpinnerFrame(), m.version))
	inputH = lipgloss.Height(m.renderBottomBar())
	bodyH = m.height - topH - inputH
	if bodyH < 3 {
		bodyH = 3
	}
	return
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return i18n.T("ui.misc.loading")
	}
	if m.width < 100 {
		return lipgloss.NewStyle().
			Width(m.width).Height(m.height).
			AlignHorizontal(lipgloss.Center).
			AlignVertical(lipgloss.Center).
			Render(i18n.T("ui.misc.width_too_small"))
	}
	if m.askState != nil {
		return renderAskUserModal(m.width, m.height, m.askState)
	}
	if m.cocreate != nil {
		return renderCoCreateModal(m.width, m.height, m.cocreate, errorText(m.err), m.textarea.View(), m.spinnerIdx, m.quitPending)
	}
	if m.help != nil {
		return renderHelpModal(m.width, m.height, m.help)
	}
	if m.report != nil {
		return renderReportModal(m.width, m.height, m.report)
	}
	if m.importer != nil {
		return renderImportModal(m.width, m.height, m.importer)
	}
	if m.simulator != nil {
		return renderSimulationModal(m.width, m.height, m.simulator)
	}

	topBar := renderTopBar(m.snapshot, m.width, m.currentSpinnerFrame(), m.version)
	inputBox := m.renderBottomBar()
	_, inputH, bodyH := m.layoutHeights()

	var body string
	if m.mode == modeNew {
		errMsg := ""
		if m.err != nil {
			errMsg = m.err.Error()
		}
		body = renderWelcome(m.width, bodyH, errMsg, m.startupMode)
	} else {
		leftW := m.sidebarWidth()
		rightW := m.detailWidth()
		centerW := m.width - leftW - rightW
		eventH, streamH := m.splitHeights(bodyH)

		if m.viewport.Width != centerW-2 || m.viewport.Height != eventH-1 {
			m.viewport.Width = centerW - 2
			m.viewport.Height = eventH - 1 // -1 cho dòng header của event panel
		}
		if m.streamVP.Width != centerW-2 || m.streamVP.Height != streamH-1 {
			m.streamVP.Width = centerW - 2
			m.streamVP.Height = streamH - 1 // -1 cho dòng header của stream panel
		}

		eventFlow := renderEventFlowViewport(m.viewport, centerW, eventH, m.paneHighlighted(focusEvents))
		streamPanel := renderStreamPanel(m.streamVP, centerW, streamH, m.paneHighlighted(focusStream), m.snapshot.IsRunning, m.spinnerIdx)
		center := lipgloss.JoinVertical(lipgloss.Left, eventFlow, streamPanel)

		left := renderStatePanel(m.stateVP, leftW, bodyH, m.paneHighlighted(focusState))
		right := renderDetailPanel(m.detailVP, rightW, bodyH, m.paneHighlighted(focusDetail))
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, topBar, body, inputBox)

	// Lớp phủ popup: nổi trên đáy body, không ảnh hưởng bố cục
	if m.modelSwitch != nil {
		commandBar := renderModelSwitchBar(m.width, m.modelSwitch)
		view = overlayAboveInput(view, commandBar, inputH)
	} else if m.compActive {
		commandBar := renderCommandPalette(m.width, m.compItems, m.compIdx)
		view = overlayAboveInput(view, commandBar, inputH)
	}
	return view
}

// sendCoCreate phát một vòng yêu cầu đồng sáng tác, xử lý thống nhất reqID, textarea, placeholder.
func (m *Model) sendCoCreate() tea.Cmd {
	m.cocreateSeq++
	m.cocreate.reqID = m.cocreateSeq
	m.cocreate.awaiting = true
	m.resizeTextarea()
	m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
	m.textarea.Blur()
	return runCoCreate(m.runtime, m.cocreate)
}

func (m Model) handleCoCreateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.cocreate == nil {
		return m, nil
	}
	state := m.cocreate

	// Bàn phím ↑↓/PgUp/PgDn/Home/End cuộn; Tab chuyển focus cuộn giữa cột hội thoại trái ↔
	// cột chỉ thị sáng tác phải (mặc định cột trái, người dùng xem lại phần chính). Trang chào
	// đã tắt mouse reporting để giữ copy gốc, khi cột phải tràn thì dựa vào Tab chuyển focus rồi
	// cuộn bằng bàn phím. Cột trái: cuộn lên tắt follow, cuộn xuống đáy bật lại follow (theo dõi stream).
	switch msg.Type {
	case tea.KeyTab:
		state.focusPrompt = !state.focusPrompt
		return m, nil
	case tea.KeyUp, tea.KeyPgUp:
		if state.focusPrompt {
			var cmd tea.Cmd
			state.promptVP, cmd = state.promptVP.Update(msg)
			return m, cmd
		}
		state.convFollow = false
		var cmd tea.Cmd
		state.convVP, cmd = state.convVP.Update(msg)
		return m, cmd
	case tea.KeyDown, tea.KeyPgDown:
		if state.focusPrompt {
			var cmd tea.Cmd
			state.promptVP, cmd = state.promptVP.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		state.convVP, cmd = state.convVP.Update(msg)
		if state.convVP.AtBottom() {
			state.convFollow = true
		}
		return m, cmd
	case tea.KeyHome:
		if state.focusPrompt {
			state.promptVP.GotoTop()
			return m, nil
		}
		state.convFollow = false
		state.convVP.GotoTop()
		return m, nil
	case tea.KeyEnd:
		if state.focusPrompt {
			state.promptVP.GotoBottom()
			return m, nil
		}
		state.convFollow = true
		state.convVP.GotoBottom()
		return m, nil
	case tea.KeyEsc:
		return m.exitCoCreate()
	}

	// Khi chờ AI reply thì loại chỉnh sửa (nhập ký tự/backspace/con trỏ/Ctrl+U/xuống dòng nhiều
	// dòng) cho qua —— người dùng có thể nhập trước câu kế trong lúc AI thinking. Việc chặn loại
	// submit hạ xuống bên trong từng case, để Enter tiết lưu đi trước chặn awaiting —— như vậy mảnh \n dán vẫn bù được dấu cách.

	switch msg.Type {
	case tea.KeyCtrlS:
		if state.awaiting {
			return m, nil
		}
		if !state.canStart() {
			return m, nil
		}
		// Đồng sáng tác theo giai đoạn: tiêm "brief định hướng tiếp theo" vào và khôi phục sáng tác, về bàn chạy.
		if state.stage {
			draft := state.draftPrompt()
			m.cocreate = nil
			m.err = nil
			m.resizeTextarea()
			m.textarea.Placeholder = defaultSteerPlaceholder()
			return m, tea.Batch(resumeFromCoCreate(m.runtime, draft), m.textarea.Focus())
		}
		// Đồng sáng tác khởi động lạnh: dùng chỉ thị sáng tác đã chỉnh để bắt đầu sáng tác.
		plan, err := state.buildPlan()
		if err != nil {
			m.err = err
			return m, nil
		}
		state.awaiting = true
		m.textarea.Blur()
		return m, startRuntime(m.runtime, plan)
	case tea.KeyEnter:
		// Alt+Enter → xuống dòng chủ động, để textarea.Update tiếp quản (KeyMap.InsertNewline đã bind phím này)
		if msg.Alt {
			break
		}
		// Khoảng cách với phím ký tự lần trước quá ngắn → coi là mảnh \n của dòng dán: bù dấu cách thay cho submit.
		// Phải phán trước khi chặn awaiting —— nếu không trong lúc awaiting mảnh \n dán sẽ bị chặn,
		// làm "abc\ndef" bị nuốt thành "abcdef", không nhất quán ngữ nghĩa với đường base.
		if !m.lastKeyAt.IsZero() && time.Since(m.lastKeyAt) < 50*time.Millisecond {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
			m.refitTextareaHeight()
			return m, cmd
		}
		// Ý định submit thật sự: chặn trong lúc awaiting (không thể phát request đồng thời)
		if state.awaiting {
			return m, nil
		}
		text := utils.CleanInputLine(m.textarea.Value())
		if text == "" {
			return m, nil
		}
		m.err = nil
		state.appendUser(text)
		m.textarea.Reset()
		m.refitTextareaHeight()
		cmd := m.sendCoCreate()
		return m, cmd
	case tea.KeyCtrlU:
		m.textarea.Reset()
		m.refitTextareaHeight()
		return m, nil
	}

	// Phím số 1/2/3 khi textarea rỗng và có gợi ý → điền gợi ý tương ứng (không gửi, sửa được).
	// Chỉ chặn khi ô nhập rỗng, tránh ảnh hưởng người dùng chủ động gõ số. Khi awaiting gợi ý
	// không hiển thị, ở đây cũng không cần phán thêm (state.suggestions rỗng là bỏ qua).
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && !state.awaiting {
		if r := msg.Runes[0]; r >= '1' && r <= '3' {
			if strings.TrimSpace(m.textarea.Value()) == "" {
				if sugs := state.suggestions(); int(r-'0') <= len(sugs) {
					m.textarea.SetValue(sugs[r-'1'])
					m.refitTextareaHeight()
					return m, nil
				}
			}
		}
	}

	// Input thông thường chuyển tiếp cho textarea
	if msg.Type == tea.KeyRunes && (containsSGRFragment(string(msg.Runes)) || isCSILeak(msg.Runes)) {
		return m, nil
	}
	var ok bool
	if msg, ok = cleanHumanKeyRunes(msg); !ok {
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.lastKeyAt = time.Now()
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.refitTextareaHeight()
	return m, cmd
}

// exitCoCreate thoát chế độ đồng sáng tác, hủy request LLM đang chạy, khôi phục trạng thái ô nhập.
func (m Model) exitCoCreate() (tea.Model, tea.Cmd) {
	if m.cocreate.cancel != nil {
		m.cocreate.cancel()
	}
	stage := m.cocreate.stage
	initial := m.cocreate.initialInput()
	m.cocreate = nil
	m.resizeTextarea()
	// Hủy đồng sáng tác theo giai đoạn: xóa cờ chiếm dụng, giữ tạm dừng, về trạng thái nhập của bàn chạy (không điền lại câu mở đầu tổng hợp).
	if stage {
		m.textarea.SetValue("")
		m.textarea.Placeholder = defaultSteerPlaceholder()
		return m, tea.Batch(cancelCoCreate(m.runtime), fetchSnapshot(m.runtime), m.textarea.Focus())
	}
	m.textarea.SetValue(initial)
	m.textarea.Placeholder = placeholderForNewMode(m.startupMode)
	return m, m.textarea.Focus()
}

func (m Model) handleAskUserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.askState == nil {
		return m, nil
	}
	state := m.askState
	q := state.currentQuestion()

	if state.typing {
		switch msg.Type {
		case tea.KeyEsc:
			state.cancelCurrentTyping()
			return m, nil
		case tea.KeyEnter:
			if state.finishCurrentAnswer() {
				state.submit()
				m.askState = nil
				return m, m.textarea.Focus()
			}
			return m, nil
		case tea.KeyBackspace, tea.KeyCtrlH:
			if state.input != "" {
				_, size := utf8.DecodeLastRuneInString(state.input)
				state.input = state.input[:len(state.input)-size]
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes {
				state.input += utils.CleanInputRunes(msg.Runes)
			}
			return m, nil
		}
	}

	switch msg.Type {
	case tea.KeyEsc:
		// Đóng popup, trả về câu trả lời rỗng
		state.request.resultCh <- askUserResult{
			resp: &tools.AskUserResponse{
				Answers: make(map[string]string),
				Notes:   make(map[string]string),
			},
		}
		m.askState = nil
		return m, m.textarea.Focus()
	case tea.KeyUp:
		state.moveCursor(-1)
	case tea.KeyDown:
		state.moveCursor(1)
	case tea.KeySpace:
		if q.MultiSelect {
			state.toggleSelection()
			if state.cursor == len(q.Options) && !state.selected[state.cursor] {
				state.input = ""
			}
		}
	case tea.KeyEnter:
		if q.MultiSelect {
			if state.cursor == len(q.Options) {
				state.toggleSelection()
				if state.selected[state.cursor] {
					state.typing = true
				}
				return m, nil
			}
			if len(state.selected) == 0 {
				state.toggleSelection()
			}
		}
		if state.finishCurrentAnswer() {
			state.submit()
			m.askState = nil
			return m, m.textarea.Focus()
		}
	}
	return m, nil
}

// overlayAboveInput phủ nổi overlay lên đáy của view base (trên inputBox),
// không đổi chiều cao bố cục tổng. Chỉ phủ đúng chiều rộng của thẻ overlay, bên phải lộ nội dung lớp dưới.
func overlayAboveInput(base, overlay string, inputLineCount int) string {
	baseLines := strings.Split(base, "\n")
	overLines := strings.Split(strings.TrimRight(overlay, "\n"), "\n")

	endY := len(baseLines) - inputLineCount
	startY := endY - len(overLines)
	if startY < 0 {
		startY = 0
	}

	for i, ol := range overLines {
		y := startY + i
		if y >= 0 && y < endY {
			olW := lipgloss.Width(ol)
			// Cắt olW ký tự hiển thị bên trái dòng nền, ghép overlay + nội dung bên phải còn lại
			right := ansi.TruncateLeft(baseLines[y], olW, "")
			baseLines[y] = ol + right
		}
	}
	return strings.Join(baseLines, "\n")
}

// isCSILeak phát hiện KeyRunes có phải mảnh rò của chuỗi escape CSI không.
// Khi terminal gửi phím mũi tên \x1b[A, bấm phím nhanh có thể làm chuỗi bị tách:
// \x1b bị parse thành Escape, "[" hoặc "[A" rò vào textarea dưới dạng KeyRunes.
func isCSILeak(runes []rune) bool {
	if len(runes) == 0 || runes[0] != '[' {
		return false
	}
	for _, r := range runes[1:] {
		if (r >= '0' && r <= '9') || r == ';' ||
			(r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '~' {
			continue
		}
		return false
	}
	return true
}

// containsSGRFragment phát hiện văn bản có chứa mảnh chuỗi chuột SGR không (mẫu "<số;số;").
func containsSGRFragment(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		j := i + 1
		if j >= len(s) || s[j] < '0' || s[j] > '9' {
			continue
		}
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j < len(s) && s[j] == ';' {
			return true
		}
	}
	return false
}

func cleanHumanKeyRunes(msg tea.KeyMsg) (tea.KeyMsg, bool) {
	if msg.Type != tea.KeyRunes {
		return msg, true
	}
	cleaned := utils.CleanInputRunes(msg.Runes)
	if cleaned == "" {
		return msg, false
	}
	msg.Runes = []rune(cleaned)
	return msg, true
}
