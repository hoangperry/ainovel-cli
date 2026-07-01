package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/entry/startup"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/host/imp"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/utils"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeTextarea()
		m.updateViewportSize()
		m.refreshDetailViewport()
		m.refreshStateViewport()
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	default:
		if next, cmd, handled := m.handleRuntimeMsg(msg); handled {
			return next, cmd
		}
		return m.handleTextareaMsg(msg)
	}
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.handleOverlayKeyMsg(msg); handled {
		return next, cmd
	}

	if msg.Type == tea.KeyCtrlC {
		if m.quitPending {
			return m, tea.Quit
		}
		m.quitPending = true
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return quitResetMsg{} })
	}
	m.quitPending = false

	if next, cmd, handled := m.handleCommandPaletteKey(msg); handled {
		return next, cmd
	}

	return m.handleBaseKeyMsg(msg)
}

func (m Model) handleOverlayKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case m.askState != nil:
		return m.handleBlockingModalKey(msg, m.handleAskUserKey)
	case m.cocreate != nil:
		return m.handleBlockingModalKey(msg, m.handleCoCreateKey)
	case m.help != nil:
		return m.handleBlockingModalKey(msg, m.handleHelpKey)
	case m.modelSwitch != nil:
		return m.handleBlockingModalKey(msg, m.handleModelSwitchKey)
	case m.report != nil:
		return m.handleBlockingModalKey(msg, m.handleReportKey)
	case m.importer != nil:
		return m.handleBlockingModalKey(msg, m.handleImportKey)
	case m.simulator != nil:
		return m.handleBlockingModalKey(msg, m.handleSimulationKey)
	default:
		return m, nil, false
	}
}

func (m Model) handleBlockingModalKey(msg tea.KeyMsg, next func(tea.KeyMsg) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd, bool) {
	if msg.Type == tea.KeyCtrlC {
		if m.quitPending {
			return m, tea.Quit, true
		}
		m.quitPending = true
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return quitResetMsg{} }), true
	}
	m.quitPending = false
	// Phím tắt toàn cục xuyên modal: trong lúc modal mở cũng phải chuyển được mouse reporting,
	// nếu không ở các modal kiểu khóa màn như đồng sáng tác/help/report người dùng không thể kéo-chọn-copy gốc.
	if msg.Type == tea.KeyCtrlR {
		next, cmd := m.toggleMouseReporting()
		return next, cmd, true
	}
	model, cmd := next(msg)
	return model, cmd, true
}

// toggleMouseReporting bật/tắt mouse reporting. Bật → tắt cho người dùng kéo-chọn-copy gốc;
// tắt → bật khôi phục click chuyển focus / cuộn. Dùng chung cho đường base và đường blocking modal.
func (m Model) toggleMouseReporting() (Model, tea.Cmd) {
	// Trang chào (modeNew) vốn không bật mouse reporting, kéo gốc là copy được; ở đây bỏ qua Ctrl+R,
	// tránh bật reporting nhầm lại phá copy gốc. Mouse reporting do enterRunning bật khi vào bàn làm việc.
	if m.mode == modeNew {
		return m, nil
	}
	m.mouseOff = !m.mouseOff
	if m.mouseOff {
		return m, tea.DisableMouse
	}
	return m, tea.EnableMouseCellMotion
}

// enterRunning vào bàn làm việc sáng tác: bật mouse reporting (bàn làm việc cần click chuyển panel /
// cuộn / kéo sidebar). Lệnh trả về cần được caller Batch vào giá trị trả về cuối.
func (m *Model) enterRunning() tea.Cmd {
	m.mode = modeRunning
	m.mouseOff = false
	return tea.EnableMouseCellMotion
}

func (m Model) handleCommandPaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.compActive {
		return m, nil, false
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.clearCommandPalette()
		return m, nil, true
	case tea.KeyUp:
		if m.compIdx > 0 {
			m.compIdx--
		}
		return m, nil, true
	case tea.KeyDown:
		if m.compIdx < len(m.compItems)-1 {
			m.compIdx++
		}
		return m, nil, true
	case tea.KeyTab:
		m.acceptCommandCompletion()
		return m, nil, true
	case tea.KeyEnter:
		item, ok := m.acceptCommandCompletion()
		if !ok {
			return m, nil, true
		}
		if item.AutoExecute {
			m.textarea.Reset()
			next, cmd := m.handleSlashCommand(slashCommand{name: item.Name})
			return next, cmd, true
		}
		return m, nil, true
	default:
		return m, nil, false
	}
}

func (m Model) handleBaseKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Phòng vệ tiết lưu: dán \n trên terminal không hỗ trợ bracketed paste sẽ thoái hóa thành chuỗi KeyEnter;
	// người thật bấm Enter cách ký tự trước thường > 100ms, <50ms rất có thể là mảnh dòng dán.
	// Chỉ ghi KeyRunes (dòng ký tự) —— phím chức năng (↑↓/Tab/Ctrl-x) không nên làm bẩn tiết lưu,
	// nếu không người dùng vừa duyệt lịch sử chọn xong bấm Enter ngay sẽ bị nuốt nhầm.
	if msg.Type == tea.KeyRunes {
		m.lastKeyAt = time.Now()
	}
	switch msg.Type {
	case tea.KeyEscape:
		if m.mode == modeRunning && m.snapshot.IsRunning {
			return m, abortRuntime(m.runtime)
		}
		m.textarea.Reset()
		m.historyIdx = len(m.inputHistory)
		m.historyDraft = ""
		m.refitTextareaHeight()
		m.clearCommandPalette()
		return m, nil
	case tea.KeyCtrlL:
		m.resetOutputPanels()
		return m, nil
	case tea.KeyCtrlU:
		// Xóa input hiện tại; đồng thời thoát trạng thái duyệt lịch sử.
		m.textarea.Reset()
		m.historyIdx = len(m.inputHistory)
		m.historyDraft = ""
		m.refitTextareaHeight()
		m.clearCommandPalette()
		return m, nil
	case tea.KeyCtrlR:
		return m.toggleMouseReporting()
	case tea.KeyTab:
		if m.mode == modeNew {
			if m.cocreate != nil {
				return m, nil
			}
			if m.startupMode == startupModeQuick {
				m.startupMode = startupModeCoCreate
			} else {
				m.startupMode = startupModeQuick
			}
			m.textarea.Placeholder = placeholderForNewMode(m.startupMode)
			return m, nil
		}
		m.focusPane = (m.focusPane + 1) % focusPaneCount
		return m, nil
	case tea.KeyEnter:
		// Alt+Enter là xuống dòng chủ động, để textarea.Update tiếp quản (KeyMap.InsertNewline đã bind vào phím này).
		if msg.Alt {
			break
		}
		// Khoảng cách với phím không phải Enter lần trước quá ngắn → coi là mảnh \n của dòng dán:
		// thay bằng dấu cách để giữ khoảng cách thị giác, nhất quán ngữ nghĩa với đường cleanHumanKeyRunes ("abc\ndef" → "abc def").
		// Phòng vệ môi trường terminal mất bracketed paste (SSH cũ/một số cấu hình tmux).
		if !m.lastKeyAt.IsZero() && time.Since(m.lastKeyAt) < 50*time.Millisecond {
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
			m.refitTextareaHeight()
			return m, cmd
		}
		return m.handleEnterKey()
	case tea.KeyUp:
		// Input nhiều dòng: để textarea tiếp quản di chuyển con trỏ trong dòng (rơi vào textarea.Update sau switch)
		if m.textareaIsMultiline() {
			break
		}
		// Một dòng: ưu tiên duyệt lịch sử, khi không có lịch sử khả dụng thì lùi về cuộn luồng sự kiện
		if m.tryHistoryUp() {
			return m, nil
		}
		return m.handleVerticalScrollKey(msg, true)
	case tea.KeyDown:
		if m.textareaIsMultiline() {
			break
		}
		if m.tryHistoryDown() {
			return m, nil
		}
		return m.handleVerticalScrollKey(msg, false)
	case tea.KeyPgUp:
		return m.handleVerticalScrollKey(msg, true)
	case tea.KeyPgDown:
		return m.handleVerticalScrollKey(msg, false)
	case tea.KeyEnd:
		switch m.focusPane {
		case focusStream:
			m.streamScroll = true
			m.streamVP.GotoBottom()
		case focusDetail:
			m.detailVP.GotoBottom()
		case focusState:
			m.stateVP.GotoBottom()
		default:
			m.autoScroll = true
			m.viewport.GotoBottom()
		}
		return m, nil
	}

	if msg.Type == tea.KeyRunes && (containsSGRFragment(string(msg.Runes)) || isCSILeak(msg.Runes)) {
		return m, nil
	}
	var ok bool
	if msg, ok = cleanHumanKeyRunes(msg); !ok {
		return m, nil
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.refitTextareaHeight()
	m.updateCommandPalette()
	return m, cmd
}

func (m Model) handleEnterKey() (tea.Model, tea.Cmd) {
	text := utils.CleanInputLine(m.textarea.Value())
	if text == "" {
		return m, nil
	}
	m.clearCommandPalette()
	if cmd, ok := parseSlashCommand(text); ok {
		m.pushInputHistory(text)
		m.textarea.Reset()
		m.refitTextareaHeight()
		return m.handleSlashCommand(cmd)
	}

	m.pushInputHistory(text)
	m.textarea.Reset()
	m.refitTextareaHeight()
	switch m.mode {
	case modeNew:
		m.err = nil
		if m.startupMode == startupModeQuick {
			plan, err := startup.PrepareQuick(startup.Request{
				Mode:        startup.ModeQuick,
				UserPrompt:  text,
				OutputDir:   m.runtime.Dir(),
				Interactive: true,
			})
			if err != nil {
				m.err = err
				return m, nil
			}
			return m, startRuntime(m.runtime, plan)
		}
		m.cocreate = newCoCreateState(text)
		return m, m.sendCoCreate()
	case modeRunning:
		// Không echo cục bộ sự kiện USER —— entry Host.Continue/Steer đã emit sự kiện "USER",
		// quay lại TUI qua events channel. Kiến trúc §2.3: lớp quan sát chỉ quan sát, không tạo ra sự thật.
		if !m.snapshot.IsRunning {
			return m, continueRuntime(m.runtime, text)
		}
		return m, steerRuntime(m.runtime, text)
	case modeDone:
		// Người dùng nhập sau khi hoàn tất (yêu cầu làm lại/viết tiếp): đánh thức một vòng run mới. Continue ở
		// trạng thái dừng máy đi Inject tự khôi phục, Coordinator nhận [用户干预] rồi route theo coordinator.md ——
		// yêu cầu làm lại chương đã viết thì gọi reopen_book mở lại sách vào trạng thái làm lại. Chuyển về
		// modeRunning vào lại bàn làm việc; vòng này chạy xong doneMsg(complete) sẽ đặt lại modeDone. Lệnh slash đã xử lý trước ở trên, không qua nhánh này.
		m.mode = modeRunning
		return m, continueRuntime(m.runtime, text)
	default:
		return m, nil
	}
}

func (m Model) handleVerticalScrollKey(msg tea.KeyMsg, upward bool) (tea.Model, tea.Cmd) {
	if m.focusPane == focusStream {
		if upward {
			m.streamScroll = false
		}
		var cmd tea.Cmd
		m.streamVP, cmd = m.streamVP.Update(msg)
		if !upward && m.streamVP.AtBottom() {
			m.streamScroll = true
		}
		return m, cmd
	}
	if m.focusPane == focusDetail {
		var cmd tea.Cmd
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd
	}
	if m.focusPane == focusState {
		var cmd tea.Cmd
		m.stateVP, cmd = m.stateVP.Update(msg)
		return m, cmd
	}
	if upward {
		m.autoScroll = false
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	if !upward && m.viewport.AtBottom() {
		m.autoScroll = true
	}
	return m, cmd
}

func (m Model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.cocreate != nil {
		// Chuột phân luồng theo tọa độ X: nửa trái màn hình = panel conv, nửa phải = panel prompt.
		// modal căn giữa và conv chiếm ~58% bên trái, dùng đường giữa màn hình để phân biệt là đủ chính xác.
		// Người dùng cuộn ở vùng conv thì tự động dừng follow (để có thể dừng ổn định ở một vị trí lịch sử).
		var cmd tea.Cmd
		if msg.X < m.width/2 {
			m.cocreate.convFollow = false
			m.cocreate.convVP, cmd = m.cocreate.convVP.Update(msg)
			if m.cocreate.convVP.AtBottom() {
				m.cocreate.convFollow = true
			}
		} else {
			m.cocreate.promptVP, cmd = m.cocreate.promptVP.Update(msg)
		}
		return m, cmd
	}
	if m.modelSwitch != nil || m.askState != nil {
		return m, nil
	}
	if pane, ok := m.paneAtMouse(msg.X, msg.Y); ok {
		m.hoverPane = pane
		m.hoverActive = true
		if msg.Action == tea.MouseActionPress {
			m.focusPane = pane
		}
	} else {
		m.hoverActive = false
	}

	var cmd tea.Cmd
	if m.focusPane == focusStream {
		m.streamVP, cmd = m.streamVP.Update(msg)
		if msg.Action == tea.MouseActionPress {
			m.streamScroll = m.streamVP.AtBottom()
		}
		return m, cmd
	}
	if m.focusPane == focusDetail {
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd
	}
	if m.focusPane == focusState {
		m.stateVP, cmd = m.stateVP.Update(msg)
		return m, cmd
	}
	m.viewport, cmd = m.viewport.Update(msg)
	if msg.Action == tea.MouseActionPress {
		m.autoScroll = m.viewport.AtBottom()
	}
	return m, cmd
}

func (m Model) handleRuntimeMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case eventMsg:
		ev := host.Event(msg)
		m.applyEvent(ev)
		m.refreshEventViewport()
		return m, listenEvents(m.runtime), true
	case bootstrapMsg:
		// Replay sự kiện lịch sử trước rồi mới xử lý lỗi: Resume bị từ chối (như trần budget) là đường thường gặp,
		// người dùng cần đọc lý do từ chối khi vẫn thấy được lịch sử, chứ không phải đối mặt luồng sự kiện trống.
		m.applyRuntimeReplay(msg.replay)
		if msg.err != nil {
			m.err = msg.err
			return m, fetchSnapshot(m.runtime), true
		}
		if msg.resumed && m.mode == modeNew {
			enableMouse := m.enterRunning()
			m.resizeTextarea()
			m.textarea.Placeholder = defaultSteerPlaceholder()
			return m, tea.Batch(fetchSnapshot(m.runtime), enableMouse), true
		}
		return m, fetchSnapshot(m.runtime), true
	case askUserMsg:
		m.askState = newAskUserState(askUserRequest(msg))
		m.textarea.Blur()
		m.applyEvent(host.Event{
			Time: time.Now(), Category: "SYSTEM", Summary: i18n.T("ui.event.await_user"), Level: "info",
		})
		m.refreshEventViewport()
		return m, listenAskUser(m.askBridge), true
	case snapshotMsg:
		m.snapshot = host.UISnapshot(msg)
		m.syncRuntimePlaceholder()
		m.refreshEventViewport()
		m.refreshStreamViewport()
		m.refreshDetailViewport()
		m.refreshStateViewport()
		return m, tickSnapshot(m.runtime), true
	case doneMsg:
		m.snapshot.IsRunning = false
		m.refreshEventViewport()
		m.refreshStreamViewport()
		m.refreshStateViewport()
		if msg.complete {
			m.abortPending = false
			m.mode = modeDone
			// Trạng thái hoàn thành không khóa ô nhập: dừng tự động viết tiếp, nhưng người dùng vẫn nhập được yêu cầu
			// làm lại (input modeDone qua Continue đánh thức một vòng run mới, Coordinator route tới reopen_book), các lệnh
			// như /export, /model cũng cần dùng được, ô nhập phải giữ focus (issue #27, #38).
			m.textarea.Placeholder = i18n.T("ui.placeholder.done")
			return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime), m.textarea.Focus()), true
		}
		if m.abortPending {
			m.abortPending = false
			m.snapshot.RuntimeState = "paused"
			m.syncRuntimePlaceholder()
		} else {
			m.textarea.Placeholder = i18n.T("ui.placeholder.interrupted")
		}
		return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime)), true
	case abortResultMsg:
		if msg.stopped {
			m.abortPending = true
			m.textarea.Placeholder = i18n.T("ui.placeholder.pausing")
		}
		return m, nil, true
	case reportLoadedMsg:
		if m.report == nil || msg.reqID != m.report.reqID {
			return m, nil, true
		}
		boxW, _ := reportModalSize(m.width, m.height)
		m.report.load(msg.report, paddedModalContentWidth(boxW), msg.exportPath, msg.finishedAt)
		return m, nil, true
	case importEventMsg:
		if m.importer == nil || msg.reqID != m.importer.reqID {
			return m, nil, true
		}
		boxW, _ := reportModalSize(m.width, m.height)
		m.importer.appendEvent(msg.ev, paddedModalContentWidth(boxW))
		if msg.ev.Stage == imp.StageError {
			return m, nil, true
		}
		if msg.ev.Stage == imp.StageDone {
			// Import thành công → tự động tiếp sức viết tiếp: Resume sẽ bật Router và phái lệnh đầu tiên,
			// đi theo luồng viết tiếp hoàn toàn giống "mở lại dự án khôi phục" (bù khâu nối import→viết tiếp cùng phiên).
			// Việc xử lý bootstrapMsg ngay sau sẽ enterRunning() chuyển sang trạng thái sáng tác.
			return m, bootstrapRuntime(m.runtime), true
		}
		return m, listenImportEvent(msg.reqID, msg.ch), true
	case simEventMsg:
		if m.simulator == nil || msg.reqID != m.simulator.reqID {
			return m, nil, true
		}
		boxW, _ := reportModalSize(m.width, m.height)
		m.simulator.appendEvent(msg.ev, paddedModalContentWidth(boxW))
		if msg.terminal() {
			return m, nil, true
		}
		return m, listenSimulationEvent(msg.reqID, msg.ch), true
	case exportDoneMsg:
		if msg.err != nil {
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "ERROR", Summary: i18n.Tf("ui.event.export_failed", msg.err.Error()), Level: "error",
			})
		} else if msg.result != nil {
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "SYSTEM", Summary: formatExportSuccess(msg.result), Level: "success",
			})
		}
		m.refreshEventViewport()
		return m, nil, true
	case startResultMsg:
		next, cmd := m.handleStartResultMsg(msg)
		return next, cmd, true
	case cocreateDeltaMsg:
		if m.cocreate == nil || msg.reqID != m.cocreate.reqID {
			return m, nil, true
		}
		m.cocreate.applyDelta(msg.kind, msg.text)
		return m, listenCoCreateDelta(m.cocreate), true
	case cocreateDoneMsg:
		next, cmd := m.handleCoCreateDoneMsg(msg)
		return next, cmd, true
	case steerResultMsg:
		return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime)), true
	case continueResultMsg:
		if msg.err != nil {
			m.err = msg.err
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "ERROR", Summary: msg.err.Error(), Level: "error",
			})
			m.refreshEventViewport()
			return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus()), true
		}
		m.err = nil
		m.textarea.Placeholder = defaultSteerPlaceholder()
		return m, tea.Batch(fetchSnapshot(m.runtime), listenDone(m.runtime), m.textarea.Focus()), true
	case spinnerTickMsg:
		m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		if m.snapshot.IsRunning {
			// Làm tươi thị giác của sao / spinner topbar đều đi qua đây (350ms)
			m.refreshEventViewport()
		}
		return m, tickSpinner(), true
	case toolSpinnerTickMsg:
		m.toolSpinnerIdx = (m.toolSpinnerIdx + 1) % len(toolSpinnerFrames)
		// Làm tươi spinner của dòng "đang chạy" trong luồng sự kiện (150ms, nhịp độc lập).
		// Frame spinner chỉ ảnh hưởng dòng sự kiện running, output render của dòng đã xong giống byte-for-byte;
		// khi không có sự kiện running thì render lại toàn bộ là vô nghĩa, bỏ qua.
		if m.snapshot.IsRunning && m.hasRunningEvent() {
			m.refreshEventViewport()
		}
		return m, tickToolSpinner(), true
	case cursorTickMsg:
		m.cursorIdx++
		if m.snapshot.IsRunning {
			// Con trỏ nhấp nháy cần render lại toàn bộ panel stream (con trỏ nằm ở cuối content);
			// tiện thể xóa luôn dirty, flush tick theo ngay sau không cần làm tươi lặp lại.
			m.refreshStreamViewport()
			m.streamDirty = false
		}
		return m, tickCursor(), true
	case streamDeltaMsg:
		if len(m.streamRounds) == 0 {
			m.streamRounds = append(m.streamRounds, "")
		}
		m.streamRounds[len(m.streamRounds)-1] += string(msg)
		// Không refreshStreamViewport ngay, để streamFlushTick gộp làm tươi 60fps.
		// Giai đoạn LLM stream tốc độ cao mỗi giây vài chục token, làm tươi từng cái bằng vài chục lần render lại toàn bộ 32 đoạn mỗi giây.
		m.streamDirty = true
		return m, listenStream(m.runtime), true
	case streamClearMsg:
		// Ranh giới round: làm tươi delta tích lũy ra trước, round mới mới căn được về thị giác
		if m.flushStreamIfDirty() && m.streamScroll {
			m.streamVP.GotoBottom()
		}
		if len(m.streamRounds) == 0 {
			m.streamRounds = append(m.streamRounds, "")
		} else if strings.TrimSpace(m.streamRounds[len(m.streamRounds)-1]) != "" {
			m.streamRounds = append(m.streamRounds, "")
		}
		m.trimStreamRounds()
		m.streamRound = len(m.streamRounds)
		m.refreshStreamViewport()
		if m.streamScroll {
			m.streamVP.GotoBottom()
		}
		return m, listenStream(m.runtime), true
	case streamFlushTickMsg:
		if m.flushStreamIfDirty() && m.streamScroll {
			m.streamVP.GotoBottom()
		}
		return m, tickStreamFlush(), true
	case quitResetMsg:
		m.quitPending = false
		return m, nil, true
	default:
		return m, nil, false
	}
}

func (m Model) handleStartResultMsg(msg startResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		if m.mode != modeNew {
			m.applyEvent(host.Event{
				Time: time.Now(), Category: "ERROR", Summary: msg.err.Error(), Level: "error",
			})
			m.refreshEventViewport()
		}
		if m.cocreate != nil {
			m.cocreate.awaiting = false
			m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
			return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus())
		}
		if m.mode == modeNew {
			m.textarea.Placeholder = placeholderForNewMode(m.startupMode)
			return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus())
		}
		return m, fetchSnapshot(m.runtime)
	}

	if m.mode == modeNew {
		m.cocreate = nil
		enableMouse := m.enterRunning()
		m.resizeTextarea()
		m.textarea.Placeholder = defaultSteerPlaceholder()
		return m, tea.Batch(fetchSnapshot(m.runtime), m.textarea.Focus(), enableMouse)
	}

	return m, fetchSnapshot(m.runtime)
}

func (m Model) handleCoCreateDoneMsg(msg cocreateDoneMsg) (tea.Model, tea.Cmd) {
	if m.cocreate == nil || msg.reqID != m.cocreate.reqID {
		return m, nil
	}
	if msg.err != nil {
		m.err = msg.err
		m.cocreate.awaiting = false
		m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
		return m, m.textarea.Focus()
	}
	m.err = nil
	m.cocreate.apply(msg.reply)
	m.textarea.Placeholder = placeholderForCoCreate(m.cocreate)
	return m, m.textarea.Focus()
}

func (m Model) handleTextareaMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.refitTextareaHeight()
	m.updateCommandPalette()
	return m, cmd
}

// applyEvent áp một sự kiện vào m.events:
// - có ID và đã tồn tại → cập nhật tại chỗ (gộp các trường trạng thái xong, giữ Time / Summary lần đầu)
// - sự kiện mới → nối thêm, khi cần ghi vào eventIndex
// - vượt maxEvents thì cắt trượt và dựng lại index
func (m *Model) applyEvent(ev host.Event) {
	if ev.ID != "" {
		if idx, ok := m.eventIndex[ev.ID]; ok && idx >= 0 && idx < len(m.events) {
			existing := &m.events[idx]
			if !ev.FinishedAt.IsZero() {
				existing.FinishedAt = ev.FinishedAt
			}
			if ev.Duration > 0 {
				existing.Duration = ev.Duration
			}
			if ev.Failed {
				existing.Failed = true
			}
			if ev.Level != "" {
				existing.Level = ev.Level
			}
			// Summary khác rỗng thì cho ghi đè (trạng thái kết thúc có thể mang thông tin bổ sung); ngược lại giữ lần đầu
			if ev.Summary != "" {
				existing.Summary = ev.Summary
			}
			return
		}
	}

	m.events = append(m.events, ev)
	if ev.ID != "" {
		m.eventIndex[ev.ID] = len(m.events) - 1
	}
	if len(m.events) > maxEvents {
		drop := len(m.events) - maxEvents
		m.events = m.events[drop:]
		m.rebuildEventIndex()
	}
}

// trimStreamRounds cắt streamRounds về maxStreamRounds đoạn; vượt thì bỏ từ đầu.
// Thời điểm gọi: sau mỗi lần streamClear mở vòng mới, sau khi replay nạp xong tất cả mục lịch sử.
func (m *Model) trimStreamRounds() {
	if len(m.streamRounds) <= maxStreamRounds {
		return
	}
	drop := len(m.streamRounds) - maxStreamRounds
	m.streamRounds = m.streamRounds[drop:]
}

func (m *Model) rebuildEventIndex() {
	m.eventIndex = make(map[string]int, len(m.events))
	for i, e := range m.events {
		if e.ID != "" {
			m.eventIndex[e.ID] = i
		}
	}
}

func (m *Model) resetOutputPanels() {
	m.events = nil
	m.eventIndex = make(map[string]int)
	m.viewport.SetContent("")
	m.viewport.GotoTop()
	m.streamBuf.Reset()
	m.streamRounds = nil
	m.streamVP.SetContent("")
	m.streamVP.GotoTop()
	m.streamRound = 0
}

func (m *Model) applyRuntimeReplay(items []domain.RuntimeQueueItem) {
	for _, item := range items {
		switch item.Kind {
		case domain.RuntimeQueueUIEvent:
			// Luồng sự kiện không replay: hàng đợi chỉ có sự kiện trạng thái xong, và các trường render cần như
			// Agent/Depth/Duration/Level không được khôi phục theo replay, dòng ra thiếu sót lộn xộn. Thà panel trống còn hơn dữ liệu nửa vời.
			continue
		case domain.RuntimeQueueStreamClear:
			if len(m.streamRounds) == 0 {
				m.streamRounds = append(m.streamRounds, "")
			} else if strings.TrimSpace(m.streamRounds[len(m.streamRounds)-1]) != "" {
				m.streamRounds = append(m.streamRounds, "")
			}
		case domain.RuntimeQueueStreamDelta:
			text := host.ReplayDeltaText(item)
			if text == "" {
				continue
			}
			if len(m.streamRounds) == 0 {
				m.streamRounds = append(m.streamRounds, "")
			}
			m.streamRounds[len(m.streamRounds)-1] += text
		}
	}
	m.trimStreamRounds()
	m.streamRound = len(m.streamRounds)
	m.refreshEventViewport()
	m.refreshStreamViewport()
}
