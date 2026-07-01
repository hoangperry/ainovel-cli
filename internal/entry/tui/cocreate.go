package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/entry/startup"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

type startupMode int

const (
	startupModeQuick startupMode = iota
	startupModeCoCreate
)

func (m startupMode) label() string {
	switch m {
	case startupModeCoCreate:
		return i18n.T("ui.cocreate.mode.cocreate")
	default:
		return i18n.T("ui.cocreate.mode.quick")
	}
}

func (m startupMode) subtitle() string {
	switch m {
	case startupModeCoCreate:
		return i18n.T("ui.cocreate.sub.cocreate")
	default:
		return i18n.T("ui.cocreate.sub.quick")
	}
}

func placeholderForNewMode(mode startupMode) string {
	switch mode {
	case startupModeCoCreate:
		return i18n.T("ui.cocreate.placeholder.new_cocreate")
	default:
		return i18n.T("ui.cocreate.placeholder.new_quick")
	}
}

func placeholderForCoCreate(state *cocreateState) string {
	if state == nil {
		return placeholderForNewMode(startupModeCoCreate)
	}
	switch {
	case state.awaiting:
		return i18n.T("ui.cocreate.placeholder.organizing")
	case state.canStart():
		if state.stage {
			return i18n.T("ui.cocreate.placeholder.refine_stage")
		}
		return i18n.T("ui.cocreate.placeholder.refine")
	default:
		return i18n.T("ui.cocreate.placeholder.add_more")
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

type cocreateState struct {
	session    *startup.CoCreateSession
	stage      bool // true=đồng sáng tác theo giai đoạn (lập kế hoạch hướng đi tiếp theo khi đang chạy); false=đồng sáng tác khởi động lạnh (làm rõ nhu cầu trước khi khởi động)
	awaiting   bool
	reqID      int
	cancel     context.CancelFunc // hủy request LLM hiện tại
	deltaCh    chan cocreateStreamItem
	doneCh     chan cocreateDoneMsg
	convVP     viewport.Model
	promptVP   viewport.Model
	convFollow bool // true: nội dung stream mới tự cuộn xuống đáy; người dùng cuộn lên thì đặt false dừng theo dõi
	// focusPrompt quyết định ↑↓/PgUp/PgDn/Home/End cuộn cột nào: false=cột hội thoại trái (mặc định),
	// true=cột chỉ thị sáng tác phải. Trang chào đã tắt mouse reporting (giữ copy gốc), cột phải tràn thì dựa Tab chuyển focus rồi cuộn bằng bàn phím.
	focusPrompt bool
}

func newCoCreateState(initial string) *cocreateState {
	makeVP := func() viewport.Model {
		vp := viewport.New(0, 0)
		vp.MouseWheelEnabled = true
		vp.MouseWheelDelta = 3
		return vp
	}
	return &cocreateState{
		session:    startup.NewCoCreateSession(strings.TrimSpace(initial)),
		awaiting:   true,
		convVP:     makeVP(),
		promptVP:   makeVP(),
		convFollow: true,
	}
}

// stageCoCreateOpener là câu mở đầu người dùng tổng hợp cho đồng sáng tác theo giai đoạn, gửi cho LLM
// làm vòng user của kickoff, để trợ lý dựa "trạng thái truyện hiện tại" chủ động mở màn, thay vì hội thoại trống chờ người dùng nói trước.
func stageCoCreateOpener() string {
	return contentlang.Pick("我先暂停一下，想和你一起规划接下来的走向。", "Mình tạm dừng một chút, muốn cùng bạn lên kế hoạch cho hướng đi sắp tới.")
}

// stageCoCreateSystemLine là cách trình bày trung tính của câu mở đầu này trong UI: câu mở đầu bản chất
// do hệ thống tổng hợp, người dùng không thật sự gõ, nên không giả làm phát ngôn của "bạn", mà dùng dòng
// hệ thống để giải thích context (nó vẫn gửi cho LLM dưới dạng stageCoCreateOpener, xem phán riêng i==0 trong renderCoCreateConversationPanel).
// Nội dung bản địa hoá lấy lúc render qua i18n key ui.cocreate.system_line.
func stageCoCreateSystemLine() string { return i18n.T("ui.cocreate.system_line") }

// newStageCoCreateState tạo trạng thái đồng sáng tác theo giai đoạn: seed câu mở đầu và đánh dấu stage,
// để runCoCreate đi StageCoCreateStream, Ctrl+S đi ResumeFromCoCreate.
func newStageCoCreateState() *cocreateState {
	s := newCoCreateState(stageCoCreateOpener())
	s.stage = true
	return s
}

func (s *cocreateState) appendUser(text string) {
	s.session.AppendUser(text)
}

func (s *cocreateState) apply(reply host.CoCreateReply) {
	s.awaiting = false
	s.session.ApplyReply(reply)
}

func (s *cocreateState) applyDelta(kind, text string) {
	s.session.ApplyDelta(kind, text)
}

func (s *cocreateState) canStart() bool {
	return s.session.CanStart()
}

func (s *cocreateState) initialInput() string {
	return s.session.InitialInput()
}

func (s *cocreateState) streamReply() string {
	return s.session.StreamReply()
}

func (s *cocreateState) draftPrompt() string {
	return s.session.DraftPrompt()
}

func (s *cocreateState) ready() bool {
	return s.session.Ready()
}

func (s *cocreateState) suggestions() []string {
	return s.session.Suggestions()
}

func (s *cocreateState) buildPlan() (startup.Plan, error) {
	return s.session.BuildPlan()
}

func renderStartupModeBar(width int, mode startupMode) string {
	quick := renderStartupModePill(mode == startupModeQuick, i18n.T("ui.cocreate.mode.quick"))
	cocreate := renderStartupModePill(mode == startupModeCoCreate, i18n.T("ui.cocreate.mode.cocreate"))
	title := lipgloss.NewStyle().
		Foreground(colorAccent).
		Bold(true).
		Render(i18n.T("ui.cocreate.startup_mode"))
	divider := lipgloss.NewStyle().
		Foreground(colorDim).
		Render("·")
	line := title + " " + divider + " " + quick + "  " + cocreate
	return lipgloss.NewStyle().
		Width(width).
		Padding(0, 1).
		Render(line)
}

func renderStartupModePill(active bool, label string) string {
	style := lipgloss.NewStyle().Padding(0, 1)
	if active {
		style = style.Foreground(lipgloss.Color("#1c1a14")).Background(colorAccent).Bold(true)
	} else {
		style = style.Foreground(colorMuted)
	}
	return style.Render(label)
}

// coCreateColumns cắt vùng nội dung modal thành chiều rộng hai cột trái phải.
// Cột trái chứa hội thoại và ô nhập (xếp trên dưới), cột phải chứa bản nháp chỉ thị sáng tác; tổng bằng chiều rộng nội dung modal.
func coCreateColumns(bodyW int) (leftW, rightW int) {
	leftW = bodyW * 58 / 100
	if leftW < 42 {
		leftW = bodyW / 2
	}
	rightW = bodyW - leftW
	if rightW < 28 {
		rightW = 28
		leftW = bodyW - rightW
	}
	return leftW, rightW
}

func renderCoCreateBody(width, height int, state *cocreateState, errMsg, inputView string, spinnerFrame int) string {
	if state == nil {
		return ""
	}
	leftW, rightW := coCreateColumns(width)

	// Border phải do container leftCol lớp ngoài vẽ, xuyên body từ đỉnh tới đáy; conversation /
	// suggestions / input đều không vẽ border phải của riêng mình. input vẫn là khung bo góc đầy đủ,
	// trái phải mỗi bên 1 cột margin căn với padding của conversation, trông cách hai đường viền đều nhau.
	// Ở chế độ đồng sáng tác textarea cố định 1 dòng (xem nhánh model.refitTextareaHeight),
	// chiều cao input = 1 (textarea) + 2 (border trên/dưới) = 3 dòng, không bao giờ trôi.
	innerW := leftW - 1 // chừa 1 cột cho đường dọc phải lớp ngoài

	inputBox := lipgloss.NewStyle().
		Width(innerW-6). // -2 margin -2 padding -2 border
		Border(baseBorder).
		BorderForeground(colorDim).
		Padding(0, 1).
		Margin(0, 1).
		Render(inputView)

	suggestionsBox := renderCoCreateSuggestions(innerW, state)
	suggestionsH := 0
	if suggestionsBox != "" {
		suggestionsH = lipgloss.Height(suggestionsBox)
	}

	convH := height - lipgloss.Height(inputBox) - suggestionsH
	if convH < 4 {
		convH = 4
	}

	convPanel := renderCoCreateConversationPanel(innerW, convH, state, errMsg, spinnerFrame)

	var stack string
	if suggestionsBox == "" {
		stack = lipgloss.JoinVertical(lipgloss.Left, convPanel, inputBox)
	} else {
		stack = lipgloss.JoinVertical(lipgloss.Left, convPanel, suggestionsBox, inputBox)
	}

	leftCol := lipgloss.NewStyle().
		Border(baseBorder, false, true, false, false).
		BorderForeground(colorDim).
		Render(stack)

	rightPanel := renderCoCreatePromptPanel(rightW, height, state)
	return lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightPanel)
}

// extractReplyForDisplay cắt đoạn <reply>...</reply> từ nội dung lịch sử assistant.
// Các tag khác (<draft>/<ready>/<suggestions>) là trường giao thức cho model vòng sau xem, không nên phơi trần cho người dùng.
// Khi model tuân thủ nửa vời (thiếu tag mở <reply>), từ đầu tới </reply> hoặc tag mở kế tiếp đều tính là reply.
// Khi hoàn toàn không chứa tag nào (đường suy giảm) thì trả nguyên trạng.
func extractReplyForDisplay(content string) string {
	rest := content
	if rIdx := strings.Index(content, "<reply>"); rIdx >= 0 {
		rest = content[rIdx+len("<reply>"):]
	}
	if cIdx := strings.Index(rest, "</reply>"); cIdx >= 0 {
		return strings.TrimSpace(rest[:cIdx])
	}
	cut := len(rest)
	for _, mark := range []string{"<draft>", "<ready>", "<suggestions>"} {
		if idx := strings.Index(rest, mark); idx >= 0 && idx < cut {
			cut = idx
		}
	}
	if cut == len(rest) && !strings.Contains(content, "<") {
		return content
	}
	return strings.TrimSpace(rest[:cut])
}

// renderCoCreateSuggestions render dòng gợi ý AI phía trên input. Khi awaiting hoặc không có gợi ý
// thì trả về chuỗi rỗng, để layout tự co lại không chừa dòng trống. Số gợi ý tối đa 3, chọn bằng phím số 1/2/3.
func renderCoCreateSuggestions(width int, state *cocreateState) string {
	if state == nil || state.awaiting {
		return ""
	}
	sugs := state.suggestions()
	if len(sugs) == 0 {
		return ""
	}
	if len(sugs) > 3 {
		sugs = sugs[:3]
	}

	digits := []string{"❶", "❷", "❸"}
	digitStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	bodyStyle := lipgloss.NewStyle().Foreground(colorMuted)
	hintStyle := lipgloss.NewStyle().Foreground(colorDim).Italic(true)

	lines := []string{hintStyle.Render(i18n.T("ui.cocreate.suggestions_header"))}
	for i, s := range sugs {
		lines = append(lines, digitStyle.Render(digits[i]+" ")+bodyStyle.Render(strings.TrimSpace(s)))
	}

	// Căn margin/padding trái phải với inputBox: trái 2 cột (margin1+padding1), phải tương tự.
	return lipgloss.NewStyle().
		Width(width-2).
		Padding(0, 2).
		Render(strings.Join(lines, "\n"))
}

func coCreateModalSize(width, height int) (boxW, boxH int) {
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 24
	}
	boxW = minInt(maxInt(width*76/100, 88), width-4)
	boxH = minInt(maxInt(height*72/100, 22), height-4)
	if boxW < 64 {
		boxW = maxInt(width-2, 42)
	}
	if boxH < 14 {
		boxH = maxInt(height-2, 12)
	}
	return boxW, boxH
}

// coCreateInputWidth tính chiều rộng ký tự thực tế textarea nhập được.
// Trang trí cột trái: đường dọc phải lớp ngoài 1 + margin trái phải input 2 + border 2 + padding 2 = 7 cột;
// prompt+cursor của textarea chiếm 2 cột; nên textareaW = leftW - 9.
func coCreateInputWidth(width, height int) int {
	boxW, _ := coCreateModalSize(width, height)
	bodyW := boxW - 4
	leftW, _ := coCreateColumns(bodyW)
	inputW := leftW - 9
	if inputW < 20 {
		inputW = 20
	}
	return inputW
}

func renderCoCreateModal(width, height int, state *cocreateState, errMsg, inputView string, spinnerFrame int, quitPending bool) string {
	if state == nil {
		return ""
	}

	boxW, boxH := coCreateModalSize(width, height)

	// title / subtitle / hint đặt ngoài modal (căn giữa trên và dưới), để bên trong modal
	// hoàn toàn giao cho body —— đường dọc phải cột trái và cột phải xuyên từ đỉnh modal tới đáy.
	// modal chiếm thực = boxH (content) + 2 (padding 1*2) + 2 (border) = boxH+4 dòng;
	// stack tổng = title(1) + subtitle(1) + trống(1) + modal(boxH+4) + trống(1) + hint(1) = boxH+9.
	// Vì vậy trừ boxH 5 dòng dành ngân sách cho trang trí ngoài modal, tránh tràn terminal.
	contentH := boxH - 5
	if contentH < 10 {
		contentH = 10
	}

	titleText, subtitleText := i18n.T("ui.cocreate.title"), i18n.T("ui.cocreate.subtitle")
	if state.stage {
		titleText, subtitleText = i18n.T("ui.cocreate.title_stage"), i18n.T("ui.cocreate.subtitle_stage")
	}
	headerStyle := lipgloss.NewStyle().Width(boxW).AlignHorizontal(lipgloss.Center)
	title := headerStyle.Foreground(colorMuted).Bold(true).Render(titleText)
	subtitle := headerStyle.Foreground(colorDim).Italic(true).Render(subtitleText)

	var hintLine string
	hintStyle := lipgloss.NewStyle().Width(boxW).AlignHorizontal(lipgloss.Center)
	if quitPending {
		// quitPending nhất quán với inputHints(); nếu không modal đồng sáng tác che thanh đáy, người dùng không cảm nhận được "bấm Ctrl+C lần nữa".
		hintLine = hintStyle.Foreground(lipgloss.Color("243")).Bold(true).Render(i18n.T("ui.misc.quit_again"))
	} else {
		hintLine = hintStyle.Foreground(colorDim).Italic(true).Render(coCreateHint(state))
	}

	body := renderCoCreateBody(boxW-4, contentH, state, errMsg, inputView, spinnerFrame)
	box := lipgloss.NewStyle().
		Width(boxW).
		Height(contentH).
		Border(baseBorder).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Render(body)

	stack := lipgloss.JoinVertical(lipgloss.Center, title, subtitle, "", box, "", hintLine)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, stack)
}

// coCreateHint sinh gợi ý phím tắt ngắn theo trạng thái, tránh trùng ngữ nghĩa với placeholder.
func coCreateHint(state *cocreateState) string {
	switch {
	case state == nil:
		return i18n.T("ui.cocreate.hint.default")
	case state.awaiting:
		return i18n.T("ui.cocreate.hint.await")
	case state.canStart():
		action := i18n.T("ui.cocreate.action.start")
		if state.stage {
			action = i18n.T("ui.cocreate.action.apply")
		}
		return i18n.Tf("ui.cocreate.hint.can_start", action)
	default:
		return i18n.T("ui.cocreate.hint.send")
	}
}

func renderCoCreateConversationPanel(width, height int, state *cocreateState, errMsg string, spinnerFrame int) string {
	// Không vẽ border của riêng mình —— đường dọc phải do container leftCol lớp ngoài vẽ thống nhất.
	// Chiều rộng cột tổng = width; style.Width = contentW = width-2; sau Padding(0,1) vùng nội dung = contentW-2.
	// Trong dòng còn phải trừ tiền tố 2 cột kiểu "▌ " / "  ", nếu không sau wrap mỗi dòng + tiền tố sẽ tràn vùng nội dung 2 cột,
	// kích hoạt gập dòng vật lý của terminal —— lipgloss vẫn cho rằng chiều cao modal cố định, nhưng chiều cao render thực của terminal tăng,
	// khi stream thinking cứ kích hoạt liên tục thì biểu hiện thành khung ngoài "rung chiều cao". Nên wrapW = contentW - 4.
	contentW := width - 2
	if contentW < 12 {
		contentW = 12
	}
	wrapW := max(12, contentW-4)

	userRole := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render(i18n.T("ui.cocreate.role.you"))
	aiRole := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(i18n.T("ui.cocreate.role.ai"))
	userBody := lipgloss.NewStyle().Foreground(colorAccent2)
	aiBody := lipgloss.NewStyle().Foreground(bodyTextColor)
	thinkingStyle := lipgloss.NewStyle().Foreground(colorDim).Italic(true)
	thinkingTag := lipgloss.NewStyle().Foreground(colorDim).Bold(true).Render(i18n.T("ui.cocreate.thinking_tag"))

	sysStyle := lipgloss.NewStyle().Foreground(colorDim).Italic(true)

	var lines []string
	for i, item := range state.session.History() {
		isUser := item.Role != "assistant"
		// Câu mở đầu tổng hợp của đồng sáng tác theo giai đoạn (luôn là message user của history[0]) hiển thị dưới dạng dòng hệ thống trung tính,
		// không giả làm input người dùng; nó vẫn được gửi cho LLM làm vòng user của kickoff.
		if isUser && state.stage && i == 0 {
			for j, line := range wrapStreamText(stageCoCreateSystemLine(), wrapW) {
				prefix := "· "
				if j > 0 {
					prefix = "  "
				}
				lines = append(lines, sysStyle.Render(prefix+line))
			}
			lines = append(lines, "")
			continue
		}
		if isUser {
			lines = append(lines, userRole)
			for _, line := range wrapStreamText(strings.TrimSpace(item.Content), wrapW) {
				// Render cả dòng một lần, tránh ANSI control bleed ở chỗ nối màu reset của tiền tố với màu nội dung.
				lines = append(lines, userBody.Render("▌ "+line))
			}
		} else {
			lines = append(lines, aiRole)
			// Trong history assistant lưu đầy đủ bốn đoạn Raw (cho context model), UI chỉ hiển thị đoạn [REPLY].
			display := extractReplyForDisplay(item.Content)
			for _, line := range wrapStreamText(strings.TrimSpace(display), wrapW) {
				lines = append(lines, aiBody.Render("  "+line))
			}
		}
		lines = append(lines, "")
	}

	if state.awaiting {
		if t := state.session.StreamThinking(); t != "" {
			lines = append(lines, thinkingTag)
			for _, line := range wrapStreamText(t, wrapW) {
				lines = append(lines, thinkingStyle.Render("  "+line))
			}
			lines = append(lines, "")
		}
		if state.streamReply() != "" {
			lines = append(lines, aiRole)
			for _, line := range wrapStreamText(state.streamReply(), wrapW) {
				lines = append(lines, aiBody.Render("  "+line))
			}
			lines = append(lines, "")
		}
		// Trang trí sparkle: để người dùng luôn thấy "AI đang làm việc"
		lines = append(lines, strings.TrimLeft(renderEventSparkle(spinnerFrame, contentW), " "))
	}
	if errMsg != "" {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(colorError).Render("! "+errMsg))
	}

	// Dùng viewport thay cho truncate thủ công, để người dùng có thể cuộn xem lại.
	// Chiều cao vp = chiều cao panel - 1 dòng tiêu đề. Sau SetContent nếu người dùng vốn ở đáy,
	// tự cuộn tới mới nhất (theo dõi stream); người dùng cuộn lên thì convFollow tắt là dừng theo dõi.
	vpH := height - 1
	if vpH < 1 {
		vpH = 1
	}
	if state.convVP.Width != contentW || state.convVP.Height != vpH {
		state.convVP.Width = contentW
		state.convVP.Height = vpH
	}
	state.convVP.SetContent(strings.Join(lines, "\n"))
	if state.convFollow {
		state.convVP.GotoBottom()
	}

	style := lipgloss.NewStyle().
		Width(contentW).
		Height(height).
		Padding(0, 1)
	return style.Render(panelTitleStyle.Render(i18n.T("ui.cocreate.conv_title")) + "\n" + state.convVP.View())
}

func renderCoCreatePromptPanel(width, height int, state *cocreateState) string {
	readyLabel := i18n.T("ui.cocreate.ready")
	if state.stage {
		readyLabel = i18n.T("ui.cocreate.ready_stage")
	}
	status := lipgloss.NewStyle().Foreground(colorDim).Render(i18n.T("ui.cocreate.status_continuing"))
	if state.ready() {
		status = lipgloss.NewStyle().Foreground(colorAccent).Render(readyLabel)
	}
	if state.awaiting {
		status = lipgloss.NewStyle().Foreground(colorMuted).Italic(true).Render(i18n.T("ui.cocreate.status_organizing"))
	}

	// Chiều rộng nội dung = chiều rộng cột tổng - 2 (padding 0,1 chiếm 2 cột, không border).
	contentW := width - 2
	if contentW < 8 {
		contentW = 8
	}

	emptyHint := i18n.T("ui.cocreate.empty_hint")
	panelTitle := i18n.T("ui.cocreate.prompt_title")
	if state.stage {
		emptyHint = i18n.T("ui.cocreate.empty_hint_stage")
		panelTitle = i18n.T("ui.cocreate.prompt_title_stage")
	}
	text := strings.TrimSpace(state.draftPrompt())
	if text == "" {
		text = lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render(emptyHint)
	} else {
		text = renderMarkdownPreview(text, max(12, contentW-2))
	}
	vpHeight := height - 5
	if vpHeight < 3 {
		vpHeight = 3
	}
	if state.promptVP.Width != contentW || state.promptVP.Height != vpHeight {
		state.promptVP.Width = contentW
		state.promptVP.Height = vpHeight
	}
	state.promptVP.MouseWheelEnabled = true
	state.promptVP.SetContent(text)

	hint := ""
	if state.promptVP.TotalLineCount() > state.promptVP.VisibleLineCount() {
		switch {
		case state.promptVP.AtTop():
			hint = i18n.T("ui.cocreate.scroll_more_down")
		case state.promptVP.AtBottom():
			hint = i18n.T("ui.cocreate.scroll_more_up")
		default:
			hint = i18n.T("ui.cocreate.scroll_more")
		}
	}

	style := lipgloss.NewStyle().
		Width(contentW).
		Height(height).
		Padding(0, 1)

	body := panelTitleStyle.Render(panelTitle) + "\n" + status + "\n\n" + state.promptVP.View()
	if hint != "" {
		body += "\n\n" + lipgloss.NewStyle().
			Width(contentW).
			AlignHorizontal(lipgloss.Center).
			Foreground(colorDim).
			Italic(true).
			Render(hint)
	}
	return style.Render(body)
}

func renderMarkdownPreview(text string, width int) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	h1Style := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	h2Style := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	h3Style := lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	bulletStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	codeStyle := lipgloss.NewStyle().Foreground(colorMuted).Italic(true)

	var out []string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			out = append(out, "")
			continue
		}

		switch {
		case strings.HasPrefix(line, "# "):
			title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
			out = append(out, h1Style.Render(title))
		case strings.HasPrefix(line, "## "):
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			out = append(out, h2Style.Render(title))
		case strings.HasPrefix(line, "### "):
			title := strings.TrimSpace(strings.TrimPrefix(line, "### "))
			out = append(out, h3Style.Render(title))
		case strings.HasPrefix(line, "- "), strings.HasPrefix(line, "* "):
			body := strings.TrimSpace(line[2:])
			wrapped := wrapStreamText(body, max(8, width-4))
			for i, item := range wrapped {
				if i == 0 {
					out = append(out, bulletStyle.Render("• ")+cardContentStyle.Render(item))
				} else {
					out = append(out, "  "+cardContentStyle.Render(item))
				}
			}
		case isOrderedMarkdownItem(line):
			prefix, body := splitOrderedMarkdownItem(line)
			wrapped := wrapStreamText(body, max(8, width-len(prefix)-2))
			for i, item := range wrapped {
				if i == 0 {
					out = append(out, bulletStyle.Render(prefix+" ")+cardContentStyle.Render(item))
				} else {
					out = append(out, strings.Repeat(" ", len(prefix)+1)+cardContentStyle.Render(item))
				}
			}
		case strings.HasPrefix(line, "> "):
			body := strings.TrimSpace(strings.TrimPrefix(line, "> "))
			for _, item := range wrapStreamText(body, max(8, width-4)) {
				out = append(out, codeStyle.Render("│ "+item))
			}
		default:
			for _, item := range wrapStreamText(line, width) {
				out = append(out, cardContentStyle.Render(item))
			}
		}
	}
	return strings.Join(out, "\n")
}

func isOrderedMarkdownItem(line string) bool {
	if len(line) < 3 {
		return false
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(line) && line[i] == '.' && line[i+1] == ' '
}

func splitOrderedMarkdownItem(line string) (prefix, body string) {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(line) {
		return "", strings.TrimSpace(line)
	}
	return line[:i+1], strings.TrimSpace(line[i+2:])
}
