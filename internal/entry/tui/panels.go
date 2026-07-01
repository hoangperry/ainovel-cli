package tui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/utils"
)

// renderTopBar render thanh trạng thái trên cùng.
// Bên trái: provider/model, giữa: tên sách, bên phải: viên trạng thái.
func renderTopBar(snap host.UISnapshot, width int, spinnerFrame, version string) string {
	novelName := snap.NovelName
	if novelName == "" {
		novelName = i18n.T("ui.misc.untitled")
	}

	var infoParts []string
	if version != "" {
		infoParts = append(infoParts, "ainovel-cli "+version)
	}
	if snap.Provider != "" {
		infoParts = append(infoParts, snap.Provider)
	}
	if snap.ModelName != "" {
		if w := formatContextWindow(snap.ModelContextWindow); w != "" {
			infoParts = append(infoParts, snap.ModelName+"("+w+")")
		} else {
			infoParts = append(infoParts, snap.ModelName)
		}
	}
	if snap.Style != "" && snap.Style != "default" {
		infoParts = append(infoParts, snap.Style)
	}
	leftText := strings.Join(infoParts, " · ")

	label := snap.StatusLabel
	if label == "" {
		label = "READY"
	}
	color, ok := statusColors[label]
	if !ok {
		color = colorDim
	}
	disp, ok := statusDisplay[label]
	if !ok {
		disp = struct {
			icon     string
			labelKey string
		}{"○", ""}
	}
	dispLabel := strings.ToLower(label)
	if disp.labelKey != "" {
		dispLabel = i18n.T(disp.labelKey)
	}
	icon := disp.icon
	if snap.IsRunning && spinnerFrame != "" {
		icon = spinnerFrame
	}
	var status string
	if icon != "" {
		status = statusIconStyle.Foreground(color).Render(icon) + " " + statusLabelStyle.Render(dispLabel)
	} else {
		status = statusLabelStyle.Render(dispLabel)
	}

	innerW := max(12, width-2)
	titleText := truncate(novelName, max(8, innerW/3))
	centerW := max(16, lipgloss.Width(titleText)+6)
	if centerW > innerW-24 {
		centerW = max(8, innerW-24)
	}
	sideTotal := innerW - centerW
	if sideTotal < 0 {
		sideTotal = 0
		centerW = innerW
	}
	leftW := sideTotal / 2
	rightW := innerW - centerW - leftW

	leftCell := lipgloss.NewStyle().
		Width(leftW).
		AlignHorizontal(lipgloss.Left).
		Foreground(colorDim).
		Render(truncate(leftText, leftW))
	centerCell := lipgloss.NewStyle().
		Width(centerW).
		AlignHorizontal(lipgloss.Center).
		Bold(true).
		Foreground(bodyTextColor).
		Render(titleText)
	rightCell := lipgloss.NewStyle().
		Width(rightW).
		AlignHorizontal(lipgloss.Right).
		Render(status)

	content := leftCell + centerCell + rightCell
	return topBarStyle.Width(width).
		Border(baseBorder, false, false, true, false).
		BorderForeground(colorDim).
		Render(content)
}

// renderStatePanel gói nội dung sidebar trạng thái (đã ở trong stateVP) vào hộp bên trái có viền phải.
// Đối xứng với renderDetailPanel: nội dung do renderStateContent sinh ra và nạp vào viewport, ở đây chỉ lo khung.
// MaxHeight kẹp chiều cao, ngăn khi cửa sổ thu nhỏ thì tràn cao hơn cột phải (xem hợp đồng chiều cao trong panels_test.go).
func renderStatePanel(vp viewport.Model, width, height int, focused bool) string {
	borderColor := colorDim
	if focused {
		borderColor = colorAccent
	}
	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxHeight(height).
		Border(baseBorder, false, true, false, false).
		BorderForeground(borderColor).
		Padding(1, 1)
	return style.Render(vp.View())
}

// renderStateContent sinh nội dung thuần của sidebar trạng thái (không gồm viền/khung ngoài), cho stateVP.SetContent dùng.
func renderStateContent(snap host.UISnapshot, contentW int) string {
	contentW = max(12, contentW)
	agents := sidebarAgents(snap.Agents)
	idleAgents := sidebarIdleAgents(snap.Agents)
	var sections []string

	if snap.RecoveryLabel != "" {
		sections = append(sections, lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
			Render(truncate(snap.RecoveryLabel, contentW)))
	}

	var overview strings.Builder
	overview.WriteString(renderField(i18n.T("ui.sidebar.runtime"), snapshotRuntimeStateLabel(snap.RuntimeState)))
	overview.WriteString(renderField(i18n.T("ui.sidebar.phase"), snapshotPhaseLabel(snap.Phase)))
	overview.WriteString(renderField(i18n.T("ui.sidebar.flow"), snapshotFlowLabel(snap.Flow)))
	if snap.Layered {
		overview.WriteString(renderField(i18n.T("ui.sidebar.completed"), i18n.Tf("ui.sidebar.chapter_unit", snap.CompletedCount)))
		// Kế hoạch động phân lớp: cột phải chỉ hiển thị các chương đã mở rộng của cung
		// truyện hiện tại, "đã lên kế hoạch" cũng dùng cùng một thước đo, nếu không sẽ
		// trộn cả ước tính thô EstimatedChapters của cung khung xương (như 92) vào, không
		// khớp với outline thấy được. Giá trị progress.TotalChapters đó chỉ dùng cho quyết
		// định ContextProfile nội bộ, đừng rò ra UI.
		if planned := len(snap.Outline); planned > 0 {
			overview.WriteString(renderField(i18n.T("ui.sidebar.planned"), i18n.Tf("ui.sidebar.chapter_unit", planned)))
		}
	} else {
		switch {
		case snap.TotalChapters > 0:
			overview.WriteString(renderField(i18n.T("ui.sidebar.progress"), i18n.Tf("ui.sidebar.progress_val", snap.CompletedCount, snap.TotalChapters)))
		default:
			overview.WriteString(renderField(i18n.T("ui.sidebar.completed"), i18n.Tf("ui.sidebar.chapter_unit", snap.CompletedCount)))
		}
	}
	overview.WriteString(renderField(i18n.T("ui.sidebar.words"), formatNumber(snap.TotalWordCount)))
	if label, ch := inProgressDisplay(snap); label != "" {
		overview.WriteString(renderField(label, i18n.Tf("ui.sidebar.chapter_n", ch)))
	}
	if headline := snapshotHeadline(snap); headline != "" {
		label := i18n.T("ui.sidebar.current")
		if !snap.IsRunning {
			label = i18n.T("ui.sidebar.pending_recover")
		}
		overview.WriteString(renderHighlightField(label, truncate(headline, contentW-10)))
	}
	sections = append(sections, renderSidebarSection(i18n.T("ui.sidebar.overview"), overview.String(), contentW))

	if len(agents) > 0 {
		var agentBody strings.Builder
		for _, agent := range agents {
			agentBody.WriteString(renderAgentLine(agent, contentW))
			agentBody.WriteString("\n")
		}
		if len(idleAgents) > 0 {
			agentBody.WriteString(lipgloss.NewStyle().Foreground(colorDim).Render(i18n.T("ui.sidebar.standby") + truncate(strings.Join(idleAgents, " · "), max(8, contentW-2))))
			agentBody.WriteString("\n")
		}
		sections = append(sections, renderSidebarSection(i18n.T("ui.sidebar.active_roles"), agentBody.String(), contentW))
	}

	if len(snap.PendingRewrites) > 0 {
		var rewrite strings.Builder
		rewrite.WriteString(renderHighlightField(i18n.T("ui.sidebar.queue"), fmt.Sprintf("%v", snap.PendingRewrites)))
		if snap.RewriteReason != "" {
			rewrite.WriteString(renderField(i18n.T("ui.sidebar.reason"), truncate(snap.RewriteReason, contentW-10)))
		}
		sections = append(sections, renderSidebarSection(i18n.T("ui.sidebar.rework"), rewrite.String(), contentW))
	}

	if snap.PendingSteer != "" {
		sections = append(sections, renderSidebarSection(i18n.T("ui.sidebar.intervene"),
			renderHighlightField(i18n.T("ui.sidebar.pending"), truncate(snap.PendingSteer, contentW-10)), contentW))
	}

	if body := renderUsageSidebar(snap, contentW); body != "" {
		sections = append(sections, renderSidebarSection(i18n.T("ui.sidebar.usage"), body, contentW))
	}

	if body := renderCacheSidebar(snap, contentW); body != "" {
		sections = append(sections, renderSidebarSection(i18n.T("ui.sidebar.cache"), body, contentW))
	}

	if body := renderContextSidebar(snap, contentW); body != "" {
		sections = append(sections, renderSidebarSection(i18n.T("ui.sidebar.context"), body, contentW))
	}

	return strings.Join(sections, "\n\n")
}

func renderAgentLine(agent host.AgentSnapshot, width int) string {
	stateColor := taskStatusColor(agent.State)
	icon := lipgloss.NewStyle().Foreground(stateColor).Render(agentStateIcon(agent.State))
	badge := lipgloss.NewStyle().Foreground(stateColor).Render(agentStateLabel(agent.State))
	name := lipgloss.NewStyle().Bold(true).Foreground(bodyTextColor).Render(agentDisplayName(agent.Name))
	line := icon + " " + name + " " + badge

	taskLine := agentTaskLine(agent)
	if taskLine != "" {
		line += "\n" + lipgloss.NewStyle().Foreground(colorDim).Render("  "+truncate(taskLine, max(8, width-2)))
	}

	detail := agent.Summary
	if agent.Tool != "" {
		detail = agent.Tool
	}
	if detail != "" && detail != taskLine {
		line += "\n" + lipgloss.NewStyle().Foreground(colorMuted).Render("  "+truncate(detail, max(8, width-2)))
	}
	if ctx := agentContextLine(agent); ctx != "" {
		line += "\n" + lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render("  "+truncate(ctx, max(8, width-2)))
	}
	return line
}

func renderSidebarSection(title, body string, width int) string {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return ""
	}
	lineW := max(0, width-lipgloss.Width(title)-1)
	header := panelTitleStyle.Render(title) + " " +
		lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("─", lineW))
	card := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colorDim).
		PaddingLeft(1).
		Render(body)
	return header + "\n" + card
}

func sidebarAgents(agents []host.AgentSnapshot) []host.AgentSnapshot {
	var out []host.AgentSnapshot
	for _, agent := range agents {
		if agent.State == "idle" {
			continue
		}
		out = append(out, agent)
	}
	if len(out) == 0 {
		out = append(out, agents...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := out[i], out[j]
		if agentStateRank(li.State) != agentStateRank(lj.State) {
			return agentStateRank(li.State) < agentStateRank(lj.State)
		}
		return agentOrder(li.Name) < agentOrder(lj.Name)
	})
	return out
}

func sidebarIdleAgents(agents []host.AgentSnapshot) []string {
	var names []string
	hasActive := false
	for _, agent := range agents {
		if agent.State != "idle" {
			hasActive = true
			continue
		}
		names = append(names, agentDisplayName(agent.Name))
	}
	if !hasActive {
		return nil
	}
	sort.Strings(names)
	return names
}

// inProgressDisplay tính label và số chương của trường "đang chạy".
// Chọn động từ theo flow (mài giũa/viết lại/viết); khi in_progress_chapter không khớp flow thì coi là stale:
//   - ở chế độ polishing/rewriting nếu chương không nằm trong pending_rewrites → lùi về chương đầu hàng đợi
//   - trường bằng 0 thì không render
func inProgressDisplay(snap host.UISnapshot) (label string, chapter int) {
	ch := snap.InProgressChapter
	switch snap.Flow {
	case "polishing":
		if ch <= 0 || !slices.Contains(snap.PendingRewrites, ch) {
			if len(snap.PendingRewrites) == 0 {
				return "", 0
			}
			ch = snap.PendingRewrites[0]
		}
		return i18n.T("ui.progress.polishing"), ch
	case "rewriting":
		if ch <= 0 || !slices.Contains(snap.PendingRewrites, ch) {
			if len(snap.PendingRewrites) == 0 {
				return "", 0
			}
			ch = snap.PendingRewrites[0]
		}
		return i18n.T("ui.progress.rewriting"), ch
	default:
		if ch <= 0 {
			return "", 0
		}
		return i18n.T("ui.progress.writing"), ch
	}
}

func snapshotHeadline(snap host.UISnapshot) string {
	if snap.PendingSteer != "" {
		if !snap.IsRunning {
			return i18n.T("ui.headline.recover_steer")
		}
		return i18n.T("ui.headline.await_steer")
	}
	if len(snap.PendingRewrites) > 0 {
		if !snap.IsRunning {
			return i18n.T("ui.headline.recover_rework")
		}
		return i18n.T("ui.headline.await_rework")
	}
	return ""
}

func snapshotPhaseLabel(phase string) string {
	switch phase {
	case "premise":
		return i18n.T("ui.phase.premise")
	case "outline":
		return i18n.T("ui.phase.outline")
	case "writing":
		return i18n.T("ui.phase.writing")
	case "complete":
		return i18n.T("ui.phase.complete")
	case "init":
		return i18n.T("ui.phase.init")
	default:
		if phase == "" {
			return "-"
		}
		return phase
	}
}

func snapshotRuntimeStateLabel(state string) string {
	switch state {
	case "running":
		return i18n.T("ui.runtime.running")
	case "pausing":
		return i18n.T("ui.runtime.pausing")
	case "paused":
		return i18n.T("ui.runtime.paused")
	case "completed":
		return i18n.T("ui.runtime.completed")
	default:
		return i18n.T("ui.runtime.idle")
	}
}

func snapshotFlowLabel(flow string) string {
	switch flow {
	case "":
		return "-"
	case "writing":
		return i18n.T("ui.flow.writing")
	case "reviewing":
		return i18n.T("ui.flow.reviewing")
	case "rewriting":
		return i18n.T("ui.flow.rewriting")
	case "polishing":
		return i18n.T("ui.flow.polishing")
	case "steering":
		return i18n.T("ui.flow.steering")
	default:
		return flow
	}
}

func renderUsageSidebar(snap host.UISnapshot, width int) string {
	if snap.TotalInputTokens <= 0 && snap.TotalOutputTokens <= 0 && snap.TotalCostUSD <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(renderField(i18n.T("ui.usage.input"), formatTokensCompact(snap.TotalInputTokens)))
	b.WriteString(renderField(i18n.T("ui.usage.output"), formatTokensCompact(snap.TotalOutputTokens)))
	if cost := formatCostUSD(snap.TotalCostUSD); cost != "" {
		b.WriteString(renderField(i18n.T("ui.usage.cost"), cost))
	}
	if saved := formatCostUSD(snap.TotalSavedUSD); saved != "" {
		b.WriteString(renderField(i18n.T("ui.usage.saved"), saved))
	}
	if snap.BudgetLimitUSD > 0 {
		pct := snap.TotalCostUSD / snap.BudgetLimitUSD * 100
		b.WriteString(renderField(i18n.T("ui.usage.budget"), fmt.Sprintf("$%.2f/$%.2f (%.0f%%)", snap.TotalCostUSD, snap.BudgetLimitUSD, pct)))
	}

	agentStats := usageStatsByCost(snap.CachePerAgent)
	if len(agentStats) > 0 {
		b.WriteString(renderUsageGroupHeader(i18n.T("ui.usage.group.role"), width))
		limit := min(len(agentStats), 4)
		for i := 0; i < limit; i++ {
			a := agentStats[i]
			b.WriteString(renderUsageLine(agentDisplayName(a.Role), eventAgentColor(a.Role), a.Input, a.Output, a.Cost, width))
			b.WriteString("\n")
		}
	}
	modelStats := usageStatsByCost(snap.CachePerModel)
	if len(modelStats) > 0 {
		b.WriteString(renderUsageGroupHeader(i18n.T("ui.usage.group.model"), width))
		limit := min(len(modelStats), 4)
		for i := 0; i < limit; i++ {
			a := modelStats[i]
			b.WriteString(renderUsageLine(modelDisplayName(a.Model), bodyTextColor, a.Input, a.Output, a.Cost, width))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func usageStatsByCost(in []host.AgentCacheStat) []host.AgentCacheStat {
	out := append([]host.AgentCacheStat(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Input+out[i].Output > out[j].Input+out[j].Output
	})
	return out
}

func renderUsageGroupHeader(label string, width int) string {
	line := lipgloss.NewStyle().Foreground(colorDim).
		Render(strings.Repeat("·", max(8, width-lipgloss.Width(label)-3)))
	return lipgloss.NewStyle().Foreground(colorMuted).Render(label+" ") + line + "\n"
}

func renderUsageLine(name string, color lipgloss.TerminalColor, input, output int, cost float64, width int) string {
	nameW := 11
	if width < 24 {
		nameW = 8
	}
	nameCell := lipgloss.NewStyle().Foreground(color).Width(nameW).
		Render(truncate(name, nameW))
	tokens := formatTokensCompact(input + output)
	right := tokens
	if costStr := formatCostUSD(cost); costStr != "" {
		right += " · " + costStr
	}
	return fitInlineLine(nameCell+lipgloss.NewStyle().Foreground(colorDim).Render(right), width)
}

func modelDisplayName(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "unknown"
	}
	parts := strings.Split(model, "/")
	if len(parts) >= 3 {
		return strings.Join(parts[1:], "/")
	}
	if len(parts) == 2 {
		return parts[1]
	}
	return model
}

// renderCacheSidebar render khối "cache" ở cột trái.
//
// Ba trạng thái:
//  1. Hoàn toàn không tiêu thụ token: trả về rỗng, section không render
//  2. Tất cả role trong phiên hiện tại đều chạy model không hỗ trợ prompt cache: chỉ render một dòng gợi ý "chưa bật"
//  3. Đã bật: trên cùng "tỷ lệ hit tích lũy/gần 10 · tiết kiệm · đọc/ghi" + phân cách + dòng per-role
//
// Dòng per-role khi capable hiển thị hai số "tích lũy/gần 10%"; khi không capable hiển thị "chưa bật".
// Qua so sánh tích lũy vs gần N lần có thể nhận ra "kéo lùi giai đoạn đầu" vs "hit thấp ổn định".
func renderCacheSidebar(snap host.UISnapshot, width int) string {
	// Streaming thượng nguồn không gửi final usage chunk của OpenAI —— dữ liệu tích lũy
	// đều bằng 0, nhưng đây không phải "chưa bật cache" cũng không phải "dùng quá ít bị gate
	// giấu đi", phải gợi ý rõ ràng, nếu không người dùng cứ tưởng cột trái có code cache mà
	// không hiển thị ra. Ưu tiên cao nhất.
	if snap.MissingAssistantUsage > 0 && snap.TotalInputTokens <= 0 {
		warn := lipgloss.NewStyle().Foreground(colorError).Bold(true).
			Render(i18n.Tf("ui.cache.missing_usage", snap.MissingAssistantUsage))
		hint := lipgloss.NewStyle().Foreground(colorDim).Italic(true).
			Render(truncate(i18n.T("ui.cache.missing_usage_hint"), max(8, width-2)))
		return warn + "\n" + hint + "\n"
	}

	if snap.TotalInputTokens <= 0 && snap.TotalCacheWriteTokens <= 0 {
		return ""
	}

	// Suốt quá trình chưa bật → hiển thị một dòng giải thích, tránh người dùng hiểu nhầm là "0% hit cần kiểm tra"
	if !snap.OverallCacheCapable && snap.TotalCacheReadTokens == 0 && snap.TotalCacheWriteTokens == 0 {
		return lipgloss.NewStyle().Foreground(colorDim).Italic(true).
			Render(truncate(i18n.T("ui.cache.not_enabled"), max(8, width-2))) + "\n"
	}

	var b strings.Builder

	// Chỉ số tổng hợp trên cùng: tích lũy + gần N mỗi cái một dòng, nhãn ghi rõ, tránh kiểu
	// "X% · gầnN Y%" trộn ba loại phân cách (dấu phần trăm / chấm giữa / chữ) làm ngữ nghĩa không rõ.
	overallHit := cacheHitRate(snap.TotalCacheReadTokens, snap.TotalInputTokens)
	b.WriteString(renderField(i18n.T("ui.cache.overall_hit"), colorPercent(overallHit)))
	if snap.OverallRecentSamples > 0 && snap.OverallRecentInput > 0 {
		recent := cacheHitRate(snap.OverallRecentCacheRead, snap.OverallRecentInput)
		b.WriteString(renderField(i18n.Tf("ui.cache.recent_hit", snap.OverallRecentSamples), colorPercent(recent)))
	}

	if savedStr := formatCostUSD(snap.TotalSavedUSD); savedStr != "" {
		b.WriteString(renderField(i18n.T("ui.cache.saved"), savedStr))
	}

	// Lượng đọc/ghi chia hai dòng. Lượng ghi bằng 0 là chuyện thường ở họ giao thức OpenAI / Gemini ——
	// hai hãng này caching tự động trong suốt, ghi cache hoàn toàn miễn phí (lần đầu không hit tính
	// theo giá input thường, lập cache không thu phụ phí nào), nên bản thân giao thức không phơi
	// trường cache_creation, không cần thiết. Chỉ họ Anthropic / Bedrock mới báo lượng ghi, vì ghi
	// của họ phải cộng phí (5m +25%/1h +100%), buộc phải đưa lượng này cho người dùng để tính tiền.
	b.WriteString(renderField(i18n.T("ui.cache.read_amount"), formatTokensCompact(snap.TotalCacheReadTokens)))
	if snap.TotalCacheWriteTokens > 0 {
		b.WriteString(renderField(i18n.T("ui.cache.write_amount"), formatTokensCompact(snap.TotalCacheWriteTokens)))
	} else if snap.TotalCacheReadTokens > 0 {
		hint := lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render(i18n.T("ui.cache.auto_no_premium"))
		b.WriteString(renderField(i18n.T("ui.cache.write_amount"), "0 "+hint))
	}

	if len(snap.CachePerAgent) > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(colorDim).
			Render(strings.Repeat("·", max(8, width-12))))
		b.WriteString("\n")
		for _, a := range snap.CachePerAgent {
			b.WriteString(renderCacheAgentLine(a, width))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// colorPercent tô màu phần trăm theo phân nấc tỷ lệ hit rồi chuyển thành chuỗi, chỉ dùng cho cột giá trị.
func colorPercent(p float64) string {
	return lipgloss.NewStyle().Foreground(cacheHitColor(p)).Bold(true).
		Render(formatPercent(p))
}

// renderCacheAgentLine render một dòng role đơn: role + tỷ lệ hit + cache đọc / tổng input.
//
// Đặt cả tử số và mẫu số ra (cacheRead / input) để người dùng nhìn một cái là kiểm chứng
// được nguồn của tỷ lệ hit, cũng nhận ra dữ liệu may rủi "phần trăm cao nhưng mẫu nhỏ"
// (ví dụ 100% / 1k độ tin cậy thấp hơn 80% / 300k).
//
// Phần trăm ưu tiên dùng giá trị ổn định của cửa sổ trượt; khi trong cửa sổ không có mẫu thì
// lùi về tích lũy. Cả cột trái chỉ chỗ này dùng "/", ngữ nghĩa chuyên biệt (dấu chia toán học:
// lượng cache hit / tổng lượng input), không lẫn với các phân cách khác.
//
// Ba trạng thái:
//
//	chưa bật   "WRITER        chưa bật"
//	đã bật     "WRITER        85%  · 323k / 394k"
//	không cache  hiển thị rõ "chưa bật", không trộn 0/0 gây nhiễu khi đọc
func renderCacheAgentLine(a host.AgentCacheStat, width int) string {
	// Tên role giữ hoàn toàn nhất quán với vùng "vai trò đang chạy"; Width lấy 12 để COORDINATOR
	// dài nhất vẫn giữ được 1 cột khoảng trắng đuôi làm phân cách, các role khác tự động đệm bên phải.
	roleStyle := lipgloss.NewStyle().Foreground(eventAgentColor(a.Role)).Width(12)
	role := roleStyle.Render(agentDisplayName(a.Role))

	if !a.CacheCapable {
		dim := lipgloss.NewStyle().Foreground(colorDim).Italic(true)
		_ = width
		return role + dim.Render(i18n.T("ui.cache.agent_not_enabled"))
	}

	// Tỷ lệ hit ổn định ưu tiên; khi trong cửa sổ không có mẫu thì lùi về tích lũy.
	hit := cacheHitRate(a.RecentCacheRead, a.RecentInput)
	if a.RecentSamples == 0 || a.RecentInput == 0 {
		hit = cacheHitRate(a.CacheRead, a.Input)
	}
	// Phần trăm cố định 4 cột rộng ("100%"), tránh cột lượng đọc nhảy trái phải giữa "5%" và "85%".
	pctCell := lipgloss.NewStyle().Width(4).
		Render(colorPercent(hit))

	// Đọc tích lũy / input tích lũy — dù phần trăm phía trên là giá trị cửa sổ trượt, tử và mẫu
	// đều dùng tích lũy, vì "nhìn ra quy mô" mới là nhu cầu chính của cột này; phần trăm cung cấp riêng tín hiệu ổn định là đủ.
	tokens := lipgloss.NewStyle().Foreground(colorDim).Render(
		" · " + formatTokensCompact(a.CacheRead) + " / " + formatTokensCompact(a.Input))
	_ = width
	return role + pctCell + tokens
}

// cacheHitRate dưới ngữ nghĩa input đã gồm cacheRead thì chia trực tiếp ra phần trăm.
// input == 0 thì trả về 0, tránh xuất hiện hit giả.
func cacheHitRate(cacheRead, input int) float64 {
	if input <= 0 {
		return 0
	}
	return float64(cacheRead) / float64(input) * 100
}

// cacheHitColor tô màu tỷ lệ hit: ≥50% xanh / 20–50% vàng / <20% đỏ.
// Dùng hướng ngược với tỷ lệ sử dụng context window: tỷ lệ cache hit càng cao càng khỏe.
func cacheHitColor(percent float64) lipgloss.AdaptiveColor {
	switch {
	case percent >= 50:
		return colorSuccess
	case percent >= 20:
		return colorReview
	default:
		return colorError
	}
}

func formatPercent(p float64) string {
	if p <= 0 {
		return "0%"
	}
	if p < 10 {
		return fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.0f%%", p)
}

// formatTokensCompact render số token thành dạng gọn "8.2k" / "1.4M".
// Dùng cho dòng per-role hẹp, tránh bị đẩy ra với phong cách dấu phẩy của formatNumber.
func formatTokensCompact(n int) string {
	if n <= 0 {
		return "0"
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func renderContextSidebar(snap host.UISnapshot, width int) string {
	if snap.ContextWindow <= 0 && snap.ContextStrategy == "" && snap.ContextScope == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(renderContextUsageField(i18n.T("ui.context.main"), snap.ContextPercent, snap.ContextTokens, snap.ContextWindow))
	if strategy := contextStrategyLabel(snap.ContextStrategy); strategy != "" {
		b.WriteString(renderField(i18n.T("ui.context.recent_strategy"), truncate(strategy, max(8, width-12))))
	}
	if scope := contextScopeLabel(snap.ContextScope); scope != "" {
		b.WriteString(renderField(i18n.T("ui.context.current_view"), scope))
	}
	if snap.ContextSummaryCount > 0 {
		b.WriteString(renderField(i18n.T("ui.context.summary"), i18n.Tf("ui.context.summary_count", snap.ContextSummaryCount)))
	}
	if snap.ContextActiveMessages > 0 {
		b.WriteString(renderField(i18n.T("ui.context.message_count"), fmt.Sprintf("%d", snap.ContextActiveMessages)))
	}
	if snap.ContextCompactedCount > 0 || snap.ContextKeptCount > 0 {
		b.WriteString(renderField(i18n.T("ui.context.recent_rewrite"), fmt.Sprintf("%d → %d", snap.ContextCompactedCount, snap.ContextKeptCount)))
	}
	return b.String()
}

func contextScopeLabel(scope string) string {
	switch scope {
	case "baseline":
		return i18n.T("ui.context.scope.baseline")
	case "projected":
		return i18n.T("ui.context.scope.projected")
	case "recovered":
		return i18n.T("ui.context.scope.recovered")
	case "committed":
		return i18n.T("ui.context.scope.committed")
	case "skipped":
		return i18n.T("ui.context.scope.skipped")
	default:
		return scope
	}
}

func contextStrategyLabel(strategy string) string {
	switch strategy {
	case "":
		return ""
	case "tool_result_microcompact":
		return i18n.T("ui.context.strategy.microcompact")
	case "light_trim":
		return i18n.T("ui.context.strategy.light_trim")
	case "full_summary":
		return i18n.T("ui.context.strategy.full_summary")
	default:
		return strategy
	}
}

func agentDisplayName(name string) string {
	return strings.ToUpper(name)
}

func agentTaskLine(agent host.AgentSnapshot) string {
	if agent.TaskKind != "" {
		return taskKindLabel(agent.TaskKind)
	}
	if agent.Summary != "" {
		return agent.Summary
	}
	return ""
}

func agentContextLine(agent host.AgentSnapshot) string {
	ctx := agent.Context
	if ctx.ContextWindow <= 0 || ctx.Tokens <= 0 {
		return ""
	}
	percentColor := contextPercentColor(ctx.Percent)
	percentStr := lipgloss.NewStyle().Foreground(percentColor).Render(fmt.Sprintf("ctx %.0f%%", ctx.Percent))
	parts := []string{percentStr}
	if scope := contextScopeLabel(ctx.Scope); scope != "" {
		parts = append(parts, scope)
	}
	if strategy := contextStrategyLabel(ctx.Strategy); strategy != "" {
		parts = append(parts, strategy)
	}
	return strings.Join(parts, " · ")
}

func agentStateRank(state string) int {
	switch state {
	case "running":
		return 0
	case "failed":
		return 1
	default:
		return 2
	}
}

func agentOrder(name string) int {
	switch {
	case strings.HasPrefix(name, "architect"):
		return 0
	case name == "coordinator":
		return 1
	case name == "editor":
		return 2
	case name == "writer":
		return 3
	default:
		return 9
	}
}

func agentStateLabel(state string) string {
	switch state {
	case "running":
		return i18n.T("ui.agent.state.running")
	case "failed":
		return i18n.T("ui.agent.state.failed")
	case "idle":
		return i18n.T("ui.agent.state.idle")
	default:
		return state
	}
}

func agentStateIcon(state string) string {
	switch state {
	case "running":
		return "●"
	case "failed":
		return "×"
	default:
		return "·"
	}
}

func taskStatusColor(status string) lipgloss.AdaptiveColor {
	switch status {
	case "running":
		return colorSuccess
	case "queued":
		return colorMuted
	case "failed", "canceled":
		return colorError
	case "succeeded":
		return colorSuccess
	default:
		return colorDim
	}
}

func taskKindLabel(kind string) string {
	switch kind {
	case "foundation_plan":
		return i18n.T("ui.task.foundation_plan")
	case "chapter_write":
		return i18n.T("ui.task.chapter_write")
	case "chapter_review":
		return i18n.T("ui.task.chapter_review")
	case "chapter_rewrite":
		return i18n.T("ui.task.chapter_rewrite")
	case "chapter_polish":
		return i18n.T("ui.task.chapter_polish")
	case "arc_expand":
		return i18n.T("ui.task.arc_expand")
	case "volume_append":
		return i18n.T("ui.task.volume_append")
	case "steer_apply":
		return i18n.T("ui.task.steer_apply")
	case "coordinator_decision":
		return i18n.T("ui.task.coordinator_decision")
	default:
		return kind
	}
}

// renderEventContent render danh sách sự kiện thành luồng sự kiện phân cấp.
// DISPATCH làm tiêu đề cấp cao nhất, tool của subagent hiển thị thụt vào, tạo cây điều phối rõ ràng.
// spinnerFrame dùng để render icon động cho dòng "đang chạy" (đồng bộ với spinner topbar).
func renderEventContent(events []host.Event, width, spinnerFrame int) string {
	var b strings.Builder
	for i, ev := range events {
		b.WriteString(renderEventLine(ev, width, spinnerFrame))
		if i < len(events)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Frame spinner dùng cho sự kiện loại gọi đang chạy (bubbles.Spinner.Dot, độc lập với MiniDot ở topbar).
var eventRunningFrames = toolSpinnerFrames

func runningSpinner(frame int) string {
	return eventRunningFrames[frame%len(eventRunningFrames)]
}

func renderEventLine(ev host.Event, width, spinnerFrame int) string {
	tsStr := lipgloss.NewStyle().Foreground(colorDim).Render(ev.Time.Format("15:04:05"))
	indent := ""
	if ev.Depth > 0 {
		indent = "  "
	}
	maxSumW := max(20, width-12-ev.Depth*2)

	running := ev.Running()
	durStr := renderEventDuration(ev.Duration)

	switch {
	case ev.Category == "DISPATCH":
		// Ba trạng thái: đang chạy (accent spinner + đậm) / thất bại (đỏ ✕) / xong (xanh ✓)
		var icon string
		switch {
		case running:
			icon = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(runningSpinner(spinnerFrame))
		case ev.Failed:
			icon = lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("✕")
		default:
			icon = lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
		}
		sum := renderDispatchSummary(ev.Summary, maxSumW)
		if running {
			// Đang chạy giữ nguyên nhưng in đậm
			sum = lipgloss.NewStyle().Bold(true).Render(sum)
		}
		line := tsStr + " " + icon + " " + sum
		if !running {
			line += durStr
		}
		return line

	case ev.Category == "DONE":
		// Tương thích dữ liệu replay cũ; luồng mới không còn sinh sự kiện DONE riêng
		icon := lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
		color := eventAgentColor(ev.Agent)
		name := lipgloss.NewStyle().Foreground(color).Render(agentDisplayName(ev.Agent))
		return tsStr + " " + icon + " " + name + durStr

	case ev.Category == "TOOL" && ev.Depth == 0:
		// tool của chính coordinator
		var icon, sum string
		switch {
		case running:
			icon = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(runningSpinner(spinnerFrame))
			sum = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(truncate(ev.Summary, maxSumW))
		case ev.Failed:
			icon = lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("✕")
			sum = lipgloss.NewStyle().Foreground(colorError).Render(truncate(ev.Summary, maxSumW))
		default:
			icon = lipgloss.NewStyle().Foreground(colorTool).Render("◇")
			sum = lipgloss.NewStyle().Foreground(colorTool).Render(truncate(ev.Summary, maxSumW))
		}
		line := tsStr + " " + icon + " " + sum
		if !running {
			line += durStr
		}
		return line

	case ev.Category == "TOOL":
		// tool nội bộ của subagent (Depth=1)
		var icon, sum string
		switch {
		case running:
			icon = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(runningSpinner(spinnerFrame))
			sum = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(truncate(ev.Summary, maxSumW))
		case ev.Failed:
			icon = lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("✕")
			sum = lipgloss.NewStyle().Foreground(colorError).Render(truncate(ev.Summary, maxSumW))
		default:
			icon = lipgloss.NewStyle().Foreground(colorDim).Render("├")
			sum = lipgloss.NewStyle().Foreground(colorMuted).Render(truncate(ev.Summary, maxSumW))
		}
		line := tsStr + " " + indent + icon + " " + sum
		if !running {
			line += durStr
		}
		return line

	case ev.Category == "ERROR":
		icon := lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("✕")
		errStyle := lipgloss.NewStyle().Foreground(colorError)
		lines := wrapStreamText(ev.Summary, maxSumW)
		first := tsStr + " " + indent + icon + " " + errStyle.Render(lines[0])
		pad := strings.Repeat(" ", 10+len(indent))
		for _, l := range lines[1:] {
			first += "\n" + pad + errStyle.Render(l)
		}
		if durStr != "" {
			first += durStr
		}
		return first

	case ev.Category == "SYSTEM":
		icon := lipgloss.NewStyle().Foreground(colorAccent).Render("⚙")
		sumColor := colorMuted
		if ev.Level == "warn" {
			sumColor = colorAccent
		}
		sum := lipgloss.NewStyle().Foreground(sumColor).Render(truncate(ev.Summary, maxSumW))
		return tsStr + " " + indent + icon + " " + sum

	case ev.Category == "USER":
		// Hiển thị lại văn bản Steer / Continue người dùng gửi từ ô nhập; tách hình thái khỏi ⚙ của SYSTEM, dùng ✎ ngụ ý "nhập".
		// Màu dùng colorAccent2 (xanh lục lam) tách khỏi màu vàng của SYSTEM, tránh đọc nhầm thành message hệ thống.
		icon := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render("✎")
		sum := lipgloss.NewStyle().Foreground(colorAccent2).Render(truncate(ev.Summary, maxSumW))
		return tsStr + " " + indent + icon + " " + sum

	case ev.Category == "CONTEXT" || ev.Category == "COMPACT":
		icon := lipgloss.NewStyle().Foreground(colorContext).Render("⚙")
		sumColor := colorContext
		if ev.Level == "debug" {
			sumColor = colorMuted
		}
		sum := lipgloss.NewStyle().Foreground(sumColor).Render(truncate(ev.Summary, maxSumW))
		return tsStr + " " + indent + icon + " " + sum

	default:
		// category đã biết dùng màu ánh xạ; category chưa biết theo màu nền chữ mặc định của terminal, tránh nhét cứng colorText.
		if color, ok := categoryColors[ev.Category]; ok {
			icon := lipgloss.NewStyle().Foreground(color).Render("·")
			sum := lipgloss.NewStyle().Foreground(color).Render(truncate(ev.Summary, maxSumW))
			return tsStr + " " + indent + icon + " " + sum
		}
		icon := lipgloss.NewStyle().Foreground(colorDim).Render("·")
		return tsStr + " " + indent + icon + " " + truncate(ev.Summary, maxSumW)
	}
}

// renderDispatchSummary render tóm tắt DISPATCH: tên Agent dùng màu vai trò, nhiệm vụ dùng màu nhạt.
func renderDispatchSummary(summary string, maxW int) string {
	agentName := summary
	taskPart := ""
	if idx := strings.Index(summary, "（"); idx > 0 {
		agentName = summary[:idx]
		taskPart = summary[idx:]
	}
	displayName := agentDisplayName(agentName)
	color := eventAgentColor(agentName)
	nameW := lipgloss.Width(displayName)
	if nameW >= maxW {
		return lipgloss.NewStyle().Foreground(color).Bold(true).Render(truncate(displayName, maxW))
	}
	result := lipgloss.NewStyle().Foreground(color).Bold(true).Render(displayName)
	if taskPart != "" {
		remaining := maxW - nameW
		if remaining > 2 {
			result += lipgloss.NewStyle().Foreground(colorMuted).Render(truncate(taskPart, remaining))
		}
	}
	return result
}

// eventAgentColor trả về màu chủ đề ứng với vai trò Agent.
func eventAgentColor(agent string) lipgloss.AdaptiveColor {
	switch {
	case strings.HasPrefix(agent, "architect"):
		return colorAccent2
	case agent == "writer":
		return colorTool
	case agent == "editor":
		return colorReview
	default:
		return colorAccent
	}
}

// renderEventDuration render Duration thành chú thích ngoặc màu nhạt, giá trị 0 trả về rỗng.
func renderEventDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return " " + lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render("("+formatDuration(d)+")")
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", m, s)
}

func renderEventActivity(snap host.UISnapshot, frame, width int) string {
	if !snap.IsRunning {
		return ""
	}
	return renderEventSparkle(frame, width)
}

var sparkleFrames = []string{
	"✦  ·   ✧   ·  ✦",
	"·  ✧   ·  ✦   ·",
	"  ✧   ·  ✦   · ",
	"   ·  ✦   ·  ✧ ",
	"✧   ·  ✦  ·   ✧",
	" ·  ✧   ·  ✦  ·",
	"✦   ·  ✧   ·  ✦",
	" ·  ✦   ·  ✧   ",
}

func renderEventSparkle(frame, width int) string {
	pattern := sparkleFrames[frame%len(sparkleFrames)]

	var b strings.Builder
	for _, ch := range pattern {
		switch ch {
		case '✦':
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#d4a21a")).Bold(true).Render("✦"))
		case '✧':
			b.WriteString(lipgloss.NewStyle().Foreground(colorAccent2).Render("✧"))
		case '·':
			b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Render("·"))
		default:
			b.WriteRune(ch)
		}
	}
	_ = width
	return " " + b.String()
}

// renderEventFlowViewport dùng viewport bọc render panel luồng sự kiện.
func renderEventFlowViewport(vp viewport.Model, width, height int, focused bool) string {
	// Thanh tiêu đề
	titleColor := colorDim
	if focused {
		titleColor = colorAccent
	}
	title := lipgloss.NewStyle().Foreground(titleColor).Render(i18n.T("ui.stream.events_title"))
	lineW := width - lipgloss.Width(title) - 4
	if lineW < 0 {
		lineW = 0
	}
	separator := lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("─", lineW))
	header := " " + title + " " + separator

	vpH := height - 1
	if vpH < 1 {
		vpH = 1
	}
	style := lipgloss.NewStyle().
		Width(width).
		Height(vpH).
		Padding(0, 1)

	return header + "\n" + style.Render(vp.View())
}

// renderStreamPanel render panel output stream (nửa dưới của cột giữa).
func renderStreamPanel(vp viewport.Model, width, height int, focused, running bool, frame int) string {
	// Thanh tiêu đề phân cách (luôn nổi bật): tiền tố thanh dọc đậm + luôn Bold + màu nhấn, tránh đụng màu với chữ nghiêng xám nhạt của phần thinking.
	// Khi focused thêm gạch chân, phân biệt trạng thái focus.
	titleStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Underline(focused)
	title := titleStyle.Render(i18n.T("ui.stream.live_title"))
	if running {
		status := renderStreamActivity(frame)
		title += " " + status
	}
	lineW := width - lipgloss.Width(title) - 4
	if lineW < 0 {
		lineW = 0
	}
	separator := lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("─", lineW))
	header := " " + title + " " + separator

	// Nội dung viewport (height gồm dòng header, chiều cao thực của viewport phải trừ 1).
	// vpStyle lớp ngoài không đặt Foreground —— màu nội dung chương do contentStyle bên trong
	// renderChapterBlock quản (nền sáng nâu đậm / nền tối theo mặc định terminal). Nếu lớp ngoài
	// thêm Foreground, ở chủ đề nền sáng khối điều phối agent (✻ vàng + label xanh) sẽ bị nâu đậm "đè" thành màu nội dung thường.
	vpH := height - 1
	if vpH < 1 {
		vpH = 1
	}
	vpStyle := lipgloss.NewStyle().
		Width(width).
		Height(vpH).
		Padding(0, 1)

	return header + "\n" + vpStyle.Render(vp.View())
}

var streamCursorFrames = []string{"·", "✢", "✳", "✶", "✻", "✽"}

func renderStreamCursor(frame int) string {
	f := frame % len(streamCursorFrames)
	var dots [3]string
	for i := range 3 {
		dots[i] = streamCursorFrames[(f+i)%len(streamCursorFrames)]
	}
	trail := dots[0] + " " + dots[1] + " " + dots[2]
	return "\n" + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(trail)
}

var streamActivityFrames = [][2]string{
	{"✦", "✧"},
	{"✦", "✧"},
	{"✧", "✦"},
	{"✧", "✦"},
	{"✦", "✧"},
	{"✦", "✧"},
	{"✧", "✦"},
	{"✧", "✦"},
}

func renderStreamActivity(frame int) string {
	pair := streamActivityFrames[frame%len(streamActivityFrames)]
	major := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(pair[0])
	minor := lipgloss.NewStyle().Foreground(colorAccent2).Render(pair[1])
	return major + " " + minor
}

// renderStreamContent render output stream theo từng vòng thành các khối ngữ nghĩa.
// Khối điều phối Agent (bắt đầu bằng ▸ hoặc ✻) dùng tiêu đề accent + chỉ thị dim; khối nội dung theo màu mặc định terminal.
// cursor khác rỗng thì nối vào cuối, biểu thị AI đang output.
func renderStreamContent(rounds []string, width int, cursor string) string {
	if width < 24 {
		width = 24
	}

	var blocks []string
	for _, round := range rounds {
		text := strings.TrimSpace(round)
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "▸") || strings.HasPrefix(text, "✻") {
			blocks = append(blocks, renderAgentBlock(text, width))
		} else {
			blocks = append(blocks, renderChapterBlock(text, width))
		}
	}
	result := strings.Join(blocks, "\n\n")
	if cursor != "" {
		result += cursor
	}
	return result
}

// renderAgentBlock render khối điều phối Agent: icon + tiêu đề + đường phân cách + chỉ thị nhiệm vụ.
//
// label dùng colorAccent2 xanh lục lam + Bold + Underline ba lớp nhấn —— trước đây colorAccent
// vàng + Bold ở nền tối quá giống dòng thinking màu xám colorDim về thị giác, không phân được chính phụ.
// Xanh lục lam là màu lạnh, tách hoàn toàn về sắc độ khỏi xám ấm của dòng thinking; Underline có hiệu lực
// ổn định trên mọi terminal, là điểm neo thị giác đáng tin hơn Bold. Icon ✻ ngược lại dùng vàng làm điểm neo, tạo tương phản hai màu với label.
func renderAgentBlock(text string, width int) string {
	headerLine, body, _ := strings.Cut(text, "\n")

	iconStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Underline(true)

	// Tách icon tiền tố (✻ hoặc ▸) và label nội dung, tô màu riêng; định dạng cũ không có icon giữ đơn sắc.
	var headerStyled string
	if first, rest, ok := strings.Cut(headerLine, " "); ok && (first == "✻" || first == "▸") {
		headerStyled = iconStyle.Render(first) + " " + labelStyle.Render(rest)
	} else {
		headerStyled = labelStyle.Render(headerLine)
	}

	// Dòng tiêu đề + đường phân cách (lineW dùng chiều rộng thị giác của headerLine chứ không phải chiều rộng byte sau render)
	titleW := lipgloss.Width(headerLine)
	lineW := max(0, width-titleW-1)
	header := headerStyled +
		" " + lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("─", lineW))

	var b strings.Builder
	b.WriteString(header)

	// Chỉ thị nhiệm vụ: màu dim, thụt 2 ô; chừa một dòng trống với header, tránh dính nhau về thị giác.
	body = strings.TrimSpace(body)
	if body != "" {
		taskStyle := lipgloss.NewStyle().Foreground(colorMuted)
		lines := wrapStreamText(body, max(16, width-6))
		b.WriteString("\n\n")
		for i, line := range lines {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(taskStyle.Render("  " + line))
		}
	}
	return b.String()
}

// renderChapterBlock render khối nội dung, tự động phân biệt nội dung thinking và nội dung chương.
// Nội dung thinking (đoạn được ThinkingSep đánh dấu) dùng colorDim nghiêng; nội dung chương dùng bodyTextColor:
// nền tối kế thừa màu nền chữ mặc định terminal, nền sáng dùng nâu đậm giữ tông ấm.
func renderChapterBlock(text string, width int) string {
	contentStyle := lipgloss.NewStyle().Foreground(bodyTextColor)
	thinkStyle := lipgloss.NewStyle().Foreground(colorDim).Italic(true)
	wrapW := max(16, width-4)

	// Tách theo ThinkingSep: đoạn lẻ là thinking, đoạn chẵn là nội dung
	// Định dạng: [nội dung] \x02 [thinking] [nội dung] \x02 [thinking] ...
	parts := strings.Split(text, utils.ThinkingSep)

	var b strings.Builder
	for i, part := range parts {
		part = strings.TrimRight(part, " \n")
		if part == "" {
			continue
		}
		isThinking := i > 0 && i%2 != 0 // đoạn lẻ sau ThinkingSep là thinking

		style := contentStyle
		if isThinking {
			style = thinkStyle
		}

		lines := wrapStreamText(part, wrapW)
		for j, line := range lines {
			if b.Len() > 0 && j == 0 {
				b.WriteString("\n\n") // dòng trống giữa các đoạn: chừa khoảng cách thị giác giữa thinking và nội dung
			} else if j > 0 {
				b.WriteString("\n")
			}
			b.WriteString(style.Render(line))
		}
	}
	return b.String()
}

func wrapStreamText(text string, width int) []string {
	if width < 8 {
		return []string{text}
	}

	var out []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(raw) == "" {
			out = append(out, "")
			continue
		}
		prefix, rest, nextPrefix := parseWrapPrefix(raw)
		wrapped := wrapRunes(rest, max(4, width-lipgloss.Width(prefix)))
		for i, line := range wrapped {
			if i == 0 {
				out = append(out, prefix+line)
				continue
			}
			out = append(out, nextPrefix+line)
		}
	}
	return out
}

func parseWrapPrefix(line string) (prefix, content, nextPrefix string) {
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	trimmed := strings.TrimSpace(line)

	switch {
	case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "), strings.HasPrefix(trimmed, "• "):
		prefix = indent + trimmed[:2]
		content = strings.TrimSpace(trimmed[2:])
		nextPrefix = indent + "  "
		return prefix, content, nextPrefix
	case orderedListPrefix(trimmed) != "":
		marker := orderedListPrefix(trimmed)
		prefix = indent + marker
		content = strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
		nextPrefix = indent + strings.Repeat(" ", lipgloss.Width(marker))
		return prefix, content, nextPrefix
	case strings.HasPrefix(trimmed, "```"):
		return indent, trimmed, indent
	default:
		return indent, trimmed, indent
	}
}

func orderedListPrefix(line string) string {
	end := strings.Index(line, ". ")
	if end <= 0 {
		return ""
	}
	for _, r := range line[:end] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return line[:end+2]
}

func wrapRunes(text string, width int) []string {
	if text == "" {
		return []string{""}
	}
	if width < 2 {
		return []string{text}
	}

	var lines []string
	var current strings.Builder
	currentWidth := 0

	for _, r := range text {
		rw := lipgloss.Width(string(r))
		if currentWidth > 0 && currentWidth+rw > width {
			lines = append(lines, strings.TrimRight(current.String(), " "))
			current.Reset()
			currentWidth = 0
			if r == ' ' {
				continue
			}
		}
		current.WriteRune(r)
		currentWidth += rw
	}
	if current.Len() > 0 {
		lines = append(lines, strings.TrimRight(current.String(), " "))
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// outlineGridThreshold ngưỡng số chương để outline chuyển sang nhiều cột.
// short tier trần 25 chương, dưới 20 một cột vừa một màn, lại giữ được huy hiệu "đang chạy";
// chế độ layered truyện dài sau khi cuộn mở rộng thì n tự nhiên vượt 20, chuyển mượt sang nhiều cột.
const outlineGridThreshold = 20

// renderOutlineSection chọn bố cục theo số chương: ít thì một cột (có huy hiệu "đang chạy"), nhiều thì lưới nhiều cột.
func renderOutlineSection(snap host.UISnapshot, contentW int) string {
	if len(snap.Outline) < outlineGridThreshold {
		return renderOutlineList(snap, contentW)
	}
	return renderOutlineGrid(snap, contentW)
}

// renderOutlineList danh sách chương một cột (cho truyện ngắn). Cuối mỗi dòng có huy hiệu "đang chạy", nhịp đọc dọc gần với mục lục hơn.
func renderOutlineList(snap host.UISnapshot, contentW int) string {
	var b strings.Builder
	for _, e := range snap.Outline {
		ch := fmt.Sprintf("%2d", e.Chapter)
		var marker, chStyle string
		titleStyle := cardContentStyle
		switch {
		case snap.CompletedCount >= e.Chapter:
			marker = lipgloss.NewStyle().Foreground(colorSuccess).Render("●")
			chStyle = lipgloss.NewStyle().Foreground(colorDim).Render(ch)
		case snap.InProgressChapter == e.Chapter:
			marker = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▸")
			chStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(ch)
			titleStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		default:
			marker = lipgloss.NewStyle().Foreground(colorDim).Render("○")
			chStyle = lipgloss.NewStyle().Foreground(colorDim).Render(ch)
			titleStyle = lipgloss.NewStyle().Foreground(colorMuted)
		}
		title := truncate(e.Title, contentW-6)
		line := marker + chStyle + " " + titleStyle.Render(title)
		if snap.InProgressChapter == e.Chapter {
			line += lipgloss.NewStyle().Foreground(colorAccent).Italic(true).Render(i18n.T("ui.detail.in_progress"))
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// renderOutlineGrid lấp các chương outline thành lưới nhiều cột theo "ưu tiên cột", tránh để màn rộng một cột chừa nhiều khoảng trống.
// Số cột tự thích ứng theo contentW (1-4), chương trong cột tăng liên tục ("đọc xong một cột rồi đọc cột kế").
// Đánh đổi so với bố cục một cột: bỏ huy hiệu "đang chạy" ở đuôi —— nhiều cột thì huy hiệu sẽ phá căn lề cột,
// và dấu ▸ + màu vàng + "đang viết chương N" ở thanh tổng quan bên trái đã nói rõ thông tin đang chạy.
func renderOutlineGrid(snap host.UISnapshot, contentW int) string {
	n := len(snap.Outline)
	if n == 0 {
		return ""
	}
	chNumW := 2
	titleW := 0
	for _, e := range snap.Outline {
		if w := len(strconv.Itoa(e.Chapter)); w > chNumW {
			chNumW = w
		}
		if w := lipgloss.Width(e.Title); w > titleW {
			titleW = w
		}
	}
	// Chiều rộng tiêu đề trần 14 (khoảng 7 chữ Hán); tiêu đề dài thi thoảng xuất hiện thì cắt, tránh một hai tiêu đề dài làm phình toàn bộ cell
	if titleW > 14 {
		titleW = 14
	} else if titleW < 4 {
		titleW = 4
	}
	cellW := 3 + chNumW + titleW // marker(1) + dấu cách(1) + số chương + dấu cách(1) + tiêu đề
	gutter := 4
	cols := (contentW + gutter) / (cellW + gutter)
	if cols < 1 {
		cols = 1
	} else if cols > 4 {
		cols = 4
	}
	rows := (n + cols - 1) / cols

	var b strings.Builder
	cellStyle := lipgloss.NewStyle().Width(cellW)
	gutterStr := strings.Repeat(" ", gutter)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			idx := c*rows + r
			if idx >= n {
				break
			}
			cell := renderOutlineCell(snap.Outline[idx], snap, chNumW, titleW)
			// Khi cột sau còn cell thì đệm theo cellW + gutter; ngược lại cell hiện tại là cuối dòng thì không đệm
			if c < cols-1 && (c+1)*rows+r < n {
				b.WriteString(cellStyle.Render(cell))
				b.WriteString(gutterStr)
			} else {
				b.WriteString(cell)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderOutlineCell render cell một chương: hoàn thành (xanh ●) / đang chạy (vàng ▸) / chưa bắt đầu (mờ ○).
func renderOutlineCell(e host.OutlineSnapshot, snap host.UISnapshot, chNumW, titleW int) string {
	chStr := fmt.Sprintf("%*d", chNumW, e.Chapter)
	title := truncateWidth(e.Title, titleW)
	var marker, chRendered, titleRendered string
	switch {
	case snap.CompletedCount >= e.Chapter:
		marker = lipgloss.NewStyle().Foreground(colorSuccess).Render("●")
		chRendered = lipgloss.NewStyle().Foreground(colorDim).Render(chStr)
		titleRendered = cardContentStyle.Render(title)
	case snap.InProgressChapter == e.Chapter:
		marker = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▸")
		chRendered = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(chStr)
		titleRendered = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
	default:
		marker = lipgloss.NewStyle().Foreground(colorDim).Render("○")
		chRendered = lipgloss.NewStyle().Foreground(colorDim).Render(chStr)
		titleRendered = lipgloss.NewStyle().Foreground(colorMuted).Render(title)
	}
	return marker + " " + chRendered + " " + titleRendered
}

// truncateWidth cắt theo "chiều rộng thị giác" (ký tự Trung tính 2 cột), cùng nguồn với lipgloss.Width.
// truncate thường tính theo số rune, với tiếng Trung sẽ cắt tới gấp đôi chiều rộng, không dùng được ở chỗ cần căn lề cột này.
func truncateWidth(s string, maxW int) string {
	if lipgloss.Width(s) <= maxW {
		return s
	}
	var b strings.Builder
	cur := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if cur+rw > maxW {
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	return b.String()
}

// renderDetailContent dựng nội dung panel chi tiết bên phải.
// Ưu tiên hiển thị thiết lập nền tảng (outline, nhân vật), rồi tới thông tin runtime (commit, rà soát...).
func renderDetailContent(snap host.UISnapshot, contentW int) string {
	var b strings.Builder

	// Outline
	if len(snap.Outline) > 0 {
		outlineHeader := i18n.T("ui.detail.outline")
		if snap.Layered {
			outlineHeader = i18n.Tf("ui.detail.outline_layered", snap.CurrentVolumeArc)
		}
		b.WriteString(panelTitleStyle.Render(outlineHeader))
		b.WriteString("\n")
		b.WriteString(renderOutlineSection(snap, contentW))
		// Gợi ý kế hoạch cuộn mở rộng
		compassStyle := lipgloss.NewStyle().Foreground(colorDim).Italic(true)
		if snap.Layered {
			if snap.NextVolumeTitle != "" {
				b.WriteString(compassStyle.Render(i18n.Tf("ui.detail.next_volume", snap.NextVolumeTitle)))
				b.WriteString("\n")
			}
			b.WriteString(compassStyle.Render(i18n.T("ui.detail.auto_generate")))
			b.WriteString("\n")
			if snap.CompassDirection != "" {
				direction := i18n.Tf("ui.detail.compass_end", snap.CompassDirection)
				if snap.CompassScale != "" {
					direction += "（" + snap.CompassScale + "）"
				}
				b.WriteString(compassStyle.Render(truncate(direction, contentW)))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	// Nhân vật
	if len(snap.Characters) > 0 {
		b.WriteString(panelTitleStyle.Render(i18n.T("ui.detail.characters")))
		b.WriteString("\n")
		for _, c := range snap.Characters {
			b.WriteString(cardContentStyle.Render("· " + truncate(c, contentW-2)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Hệ sinh thái nhân vật phụ: tổng số nhân vật phụ đã xuất hiện tích lũy + top 5 hoạt động gần đây
	if snap.SupportingCount > 0 {
		b.WriteString(panelTitleStyle.Render(i18n.T("ui.detail.supporting")))
		b.WriteString("\n")
		b.WriteString(cardContentStyle.Render(truncate(i18n.Tf("ui.detail.supporting_count", snap.SupportingCount), contentW)))
		b.WriteString("\n")
		for _, name := range snap.RecentSupporting {
			b.WriteString(cardContentStyle.Render("· " + truncate(name, contentW-2)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Tiền đề
	if snap.Premise != "" {
		b.WriteString(panelTitleStyle.Render(i18n.T("ui.detail.premise")))
		b.WriteString("\n")
		for _, line := range wrapStreamText(snap.Premise, contentW) {
			b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Render(line))
			b.WriteString("\n")
		}
		b.WriteString("\n\n")
	}

	if snap.LastCommitSummary != "" {
		b.WriteString(cardTitleStyle.Render(i18n.T("ui.detail.last_commit")))
		b.WriteString("\n")
		b.WriteString(cardContentStyle.Render(snap.LastCommitSummary))
		b.WriteString("\n\n")
	}

	if snap.LastReviewSummary != "" {
		b.WriteString(cardTitleStyle.Render(i18n.T("ui.detail.last_review")))
		b.WriteString("\n")
		b.WriteString(cardContentStyle.Render(snap.LastReviewSummary))
		b.WriteString("\n\n")
	}

	if len(snap.RecentSummaries) > 0 {
		b.WriteString(cardTitleStyle.Render(i18n.T("ui.detail.summaries")))
		b.WriteString("\n")
		for _, s := range snap.RecentSummaries {
			b.WriteString(cardContentStyle.Render(truncate(s, contentW)))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// renderDetailPanel render panel chi tiết cuộn được bên phải.
func renderDetailPanel(vp viewport.Model, width, height int, focused bool) string {
	borderColor := colorDim
	if focused {
		borderColor = colorAccent
	}
	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxHeight(height).
		Border(baseBorder, false, false, false, true).
		BorderForeground(borderColor).
		Padding(0, 1)

	return style.Render(vp.View())
}

// renderWelcome render màn hình đầu của trạng thái tạo mới.
func renderWelcome(width, height int, errMsg string, mode startupMode) string {
	// Tiêu đề gọn
	title := lipgloss.NewStyle().
		Foreground(colorAccent).
		Bold(true).
		Render("A I N O V E L")

	// Phụ đề
	subtitle := lipgloss.NewStyle().
		Foreground(colorMuted).
		Italic(true).
		Render(i18n.T("ui.welcome.subtitle"))

	// Đường phân cách
	divW := 44
	if divW > width-8 {
		divW = width - 8
	}
	divider := lipgloss.NewStyle().Foreground(colorDim).
		Render(strings.Repeat("~", divW))

	// Điểm nổi bật tính năng
	features := []struct{ icon, label, desc string }{
		{">>", i18n.T("ui.welcome.feat.collab"), i18n.T("ui.welcome.feat.collab_desc")},
		{"::", i18n.T("ui.welcome.feat.resume"), i18n.T("ui.welcome.feat.resume_desc")},
		{"<>", i18n.T("ui.welcome.feat.steer"), i18n.T("ui.welcome.feat.steer_desc")},
		{"##", i18n.T("ui.welcome.feat.layered"), i18n.T("ui.welcome.feat.layered_desc")},
	}
	iconStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	featLabelStyle := lipgloss.NewStyle().Foreground(bodyTextColor)
	descStyle := lipgloss.NewStyle().Foreground(colorDim)
	var featLines []string
	for _, f := range features {
		line := iconStyle.Render(f.icon) + " " +
			featLabelStyle.Render(f.label) + "  " +
			descStyle.Render(f.desc)
		featLines = append(featLines, line)
	}
	feats := strings.Join(featLines, "\n")

	// Gợi ý nhập
	prompt := lipgloss.NewStyle().Foreground(bodyTextColor).Render(i18n.T("ui.welcome.prompt"))

	modeLine := lipgloss.NewStyle().
		Foreground(colorMuted).
		Render(i18n.T("ui.welcome.current_mode") + mode.label() + " · " + mode.subtitle())

	// Ví dụ
	examples := []string{
		i18n.T("ui.welcome.example1"),
		i18n.T("ui.welcome.example2"),
		i18n.T("ui.welcome.example3"),
	}
	exStyle := lipgloss.NewStyle().Foreground(colorAccent)
	dotStyle := lipgloss.NewStyle().Foreground(colorDim)
	var exLines []string
	for _, ex := range examples {
		exLines = append(exLines, dotStyle.Render("  . ")+exStyle.Render(ex))
	}
	exBlock := strings.Join(exLines, "\n")

	// Lắp ghép
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(subtitle)
	b.WriteString("\n\n")
	b.WriteString(divider)
	b.WriteString("\n\n")
	b.WriteString(feats)
	b.WriteString("\n\n")
	b.WriteString(divider)
	b.WriteString("\n\n")
	b.WriteString(modeLine)
	b.WriteString("\n\n")
	b.WriteString(prompt)
	b.WriteString("\n\n")
	b.WriteString(exBlock)
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(colorDim).Italic(true).
		Render(i18n.T("ui.welcome.bottom_hint")))

	if errMsg != "" {
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("! " + errMsg))
	}

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		AlignHorizontal(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Render(b.String())
}
