package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/utils"
)

// errorKind classifies a runtime error into a stable, short label for log
// filtering and alert routing. Returns "" when no special tag applies.
//
// err is the live error chain (may be nil after JSON serialization); msg is
// the rendered string fallback used when the chain has been flattened
// (e.g. inside sub-agent JSON results).
func errorKind(err error, msg string) string {
	if err != nil && errors.Is(err, agentcore.ErrProviderStreamIdle) {
		return "stream_idle"
	}
	if msg != "" && agentcore.IsStreamIdleMessage(msg) {
		return "stream_idle"
	}
	return ""
}

// Bộ đếm ID sự kiện tăng đơn điệu; kết hợp timestamp để sinh ID ổn định.
var eventIDCounter uint64

func nextEventID() string {
	return fmt.Sprintf("e%d", atomic.AddUint64(&eventIDCounter, 1))
}

// activeCall ghi lại ID, thời điểm bắt đầu và summary của một lần gọi đang diễn ra (TOOL / DISPATCH).
// summary được điền ngược vào finish Event khi hoàn thành, bảo đảm replay (runtime queue) khôi phục được nội dung dòng.
type activeCall struct {
	id      string
	start   time.Time
	summary string
	depth   int
}

// observer đăng ký event stream của coordinator và chiếu sang kênh output của Host.
// Nó là observer thuần túy, không tham gia bất kỳ quyết định điều khiển nào.
type observer struct {
	unsub   func()
	emitEv  func(Event)
	emitD   func(string)
	emitC   func()
	store   *storepkg.Store // dùng để persist runtime queue (ReplayQueue tiêu thụ)
	agents  map[string]*agentState
	agentMu sync.Mutex

	// aborting được Host bật ở lối vào Abort()/Close(), xóa ở Start/Resume/Continue.
	// Trong lúc bật, mọi sự kiện lỗi phát sinh từ context-cancel bị ức chế (vừa đúng kỳ vọng người dùng, vừa tránh
	// trùng với sự kiện "người dùng tạm dừng thủ công"). Ngoại lệ thật (không phải cancel) vẫn báo như thường.
	aborting atomic.Bool

	streamThinking        bool
	lastThinkingByAgent   map[string]string          // agent → văn bản thinking tích lũy gần nhất (dùng để trích delta tăng tiến)
	dispatchStarts        map[string]*activeCall     // dispatched agent → lời gọi DISPATCH đang diễn ra
	currentDispatchTarget string                     // tên subagent đang thực thi (lúc handleToolEnd thì Args có thể rỗng)
	toolStarts            map[string]*activeCall     // agent → lời gọi TOOL đang diễn ra
	streamExtractors      map[string]*agentExtractor // agent → bộ trích nội dung từ tham số JSON của lời gọi tool hiện tại
	streamHasContent      bool                       // streamRound hiện tại đã xuất nội dung hay chưa (để xét có cần ngăn đoạn không)
	streamLastByte        byte                       // byte cuối của lần xuất stream gần nhất (dùng để bù xuống dòng chính xác)
}

// agentExtractor ghi lại tên tool đang được trích và instance bộ trích của một agent.
// Tên tool dùng để phát hiện "một lời gọi tool mới đã bắt đầu", tránh cache bị tàn dư lượt trước làm ô nhiễm.
type agentExtractor struct {
	tool       string
	ext        *jsonFieldExtractor
	emittedAny bool // extractor này đã sinh nội dung hay chưa; dùng để bù ngăn đoạn trước lần xuất đầu tiên
}

type agentState struct {
	name    string
	state   string
	tool    string
	summary string
	turn    int
	context AgentContextSnapshot
	updated time.Time
}

func newObserver(coordinator *agentcore.Agent, s *storepkg.Store, emitEv func(Event), emitD func(string), emitC func()) *observer {
	o := &observer{
		emitEv:              emitEv,
		emitD:               emitD,
		emitC:               emitC,
		store:               s,
		agents:              make(map[string]*agentState),
		lastThinkingByAgent: make(map[string]string),
		dispatchStarts:      make(map[string]*activeCall),
		toolStarts:          make(map[string]*activeCall),
		streamExtractors:    make(map[string]*agentExtractor),
	}
	o.unsub = coordinator.Subscribe(o.handle)
	return o
}

func (o *observer) finalize() {
	o.agentMu.Lock()
	defer o.agentMu.Unlock()
	for _, a := range o.agents {
		a.state = "idle"
		a.tool = ""
	}
}

// setAborting được Host gọi ở các điểm chuyển vòng đời như Abort/Close/Start, điều khiển
// việc các sự kiện phát sinh loại "context canceled" có cần ức chế hay không (tránh trùng với "người dùng tạm dừng thủ công").
func (o *observer) setAborting(v bool) { o.aborting.Store(v) }

// isCancellationNoise xét một lỗi có phải nhiễu phát sinh từ abort hay không.
// Chỉ khi Host ở trạng thái aborting thì trả về true mới có ý nghĩa — context.Canceled ngoài
// thời gian abort có thể phản ánh vấn đề thật (như ctx bên ngoài bị hủy), vẫn nên báo.
func (o *observer) isCancellationNoise(err error, msg string) bool {
	if !o.aborting.Load() {
		return false
	}
	if err != nil && errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(strings.ToLower(msg), "context canceled")
}

// emitAndLog dùng cho trạng thái "bắt đầu" của sự kiện loại gọi: gửi cho TUI nhưng không ghi vào runtime queue,
// tránh trùng "một dòng bắt đầu, một dòng hoàn thành" khi replay. slog do host.emitEvent ghi thống nhất.
func (o *observer) emitAndLog(ev Event) {
	o.emitEv(ev)
}

// persistEvent ghi sự kiện vào runtime queue (slog do host.emitEvent ghi thống nhất).
func (o *observer) persistEvent(ev Event) {
	if o.store == nil || o.store.Runtime == nil {
		return
	}
	priority := domain.RuntimePriorityBackground
	switch ev.Category {
	case "SYSTEM", "ERROR":
		priority = domain.RuntimePriorityControl
	}
	_, _ = o.store.Runtime.AppendQueue(domain.RuntimeQueueItem{
		Time:     ev.Time,
		Kind:     domain.RuntimeQueueUIEvent,
		Priority: priority,
		Category: ev.Category,
		Summary:  ev.Summary,
		Payload:  ev,
	})
}

func (o *observer) handle(ev agentcore.Event) {
	switch ev.Type {
	case agentcore.EventToolExecStart:
		o.handleToolStart(ev)
	case agentcore.EventToolExecUpdate:
		o.handleToolUpdate(ev)
	case agentcore.EventToolExecEnd:
		o.handleToolEnd(ev)
	case agentcore.EventMessageUpdate:
		o.handleMessageUpdate(ev)
	case agentcore.EventMessageEnd:
		o.streamClear()
	case agentcore.EventTurnStart:
		if ev.Progress != nil && ev.Progress.Kind == agentcore.ProgressTurnCounter {
			o.updateAgent(ev.Progress.Agent, func(a *agentState) {
				a.turn = ev.Progress.Turn
			})
		}
	case agentcore.EventRetry:
		if ev.RetryInfo != nil {
			msg := ""
			if ev.RetryInfo.Err != nil {
				msg = ev.RetryInfo.Err.Error()
			}
			prefix := fmt.Sprintf(contentlang.Pick("重试 (%d/%d): ", "Thử lại (%d/%d): "), ev.RetryInfo.Attempt, ev.RetryInfo.MaxRetries)
			retryEv := Event{
				Time:     time.Now(),
				Category: "SYSTEM",
				Summary:  prefix + truncate(msg, 80),
				Detail:   prefix + msg,
				Kind:     errorKind(ev.RetryInfo.Err, msg),
				Level:    "warn",
			}
			o.emitEv(retryEv)
			o.persistEvent(retryEv)
		}
	case agentcore.EventError:
		if ev.Err != nil {
			fullMsg := ev.Err.Error()
			if o.isCancellationNoise(ev.Err, fullMsg) {
				// Lỗi ctx-cancel phát sinh từ abort chủ động của người dùng; đã có sự kiện "người dùng tạm dừng thủ công", không lặp lại làm ngập màn hình.
				slog.Debug("suppressed cancel-derived error", "module", "agent", "msg", fullMsg)
				return
			}
			errEv := Event{
				Time:     time.Now(),
				Category: "ERROR",
				Summary:  truncate(fullMsg, 120),
				Detail:   fullMsg,
				Kind:     errorKind(ev.Err, fullMsg),
				Level:    "error",
			}
			o.emitEv(errEv)
			o.persistEvent(errEv)
		}
	}
}

func (o *observer) handleMessageUpdate(ev agentcore.Event) {
	if ev.Delta == "" {
		return
	}
	// Tham số tool-call của Coordinator là JSON nhiệm vụ cho subagent, không có nội dung đọc được, loại bỏ thẳng.
	if ev.DeltaKind == agentcore.DeltaToolCall {
		return
	}
	o.emitStreamDelta(ev.Delta, ev.DeltaKind == agentcore.DeltaThinking)
}

func (o *observer) handleToolStart(ev agentcore.Event) {
	if ev.Tool == "" {
		return
	}
	agent := agentFromEvent(ev)

	// Lời gọi subagent → sự kiện DISPATCH (đang diễn ra)
	if ev.Tool == "subagent" {
		sub := parseSubagentArgs(ev.Args)
		target := sub.agent
		if target == "" {
			target = "subagent"
		}
		dispatchSummary := target
		if sub.task != "" {
			firstLine := strings.TrimSpace(strings.SplitN(sub.task, "\n", 2)[0])
			if firstLine != "" {
				dispatchSummary += "（" + truncate(firstLine, 30) + "）"
			}
		}
		o.updateAgent(agent, func(a *agentState) {
			a.state = "working"
			a.tool = ev.Tool
			a.summary = fmt.Sprintf("%s → %s", agent, dispatchSummary)
		})
		o.currentDispatchTarget = target
		id := nextEventID()
		o.dispatchStarts[target] = &activeCall{id: id, start: time.Now(), summary: dispatchSummary}
		o.emitAndLog(Event{
			ID:       id,
			Time:     time.Now(),
			Category: "DISPATCH",
			Agent:    agent,
			Summary:  dispatchSummary,
			Level:    "info",
		})
		return
	}

	// Tool của chính coordinator (đang diễn ra)
	toolName := displayToolName(ev.Tool, ev.Args)
	o.updateAgent(agent, func(a *agentState) {
		a.state = "working"
		a.tool = ev.Tool
		a.summary = fmt.Sprintf("%s → %s", agent, toolName)
	})
	id := nextEventID()
	o.toolStarts[agent] = &activeCall{id: id, start: time.Now(), summary: toolName}
	o.emitAndLog(Event{
		ID:       id,
		Time:     time.Now(),
		Category: "TOOL",
		Agent:    agent,
		Summary:  toolName,
		Level:    "info",
	})
	o.emitFallbackStreamHeader(ev.Tool)
}

func (o *observer) handleToolUpdate(ev agentcore.Event) {
	if ev.Progress == nil {
		return
	}
	switch ev.Progress.Kind {
	case agentcore.ProgressToolDelta:
		if ev.Progress.Delta != "" {
			o.handleSubagentDelta(ev.Progress)
		}
	case agentcore.ProgressToolStart:
		// Lời gọi tool bên trong sub-agent (như writer → draft_chapter).
		// Lưu ý: dòng TOOL có thể đã được handleSubagentDelta phát sớm ở giai đoạn nhận diện stream.
		// Ở đây: nếu đã phát → chỉ cập nhật summary (args lúc này đầy đủ, hiển thị được "tool(chương N)"); nếu chưa thì phát bình thường.
		if ev.Progress.Agent == "" || ev.Progress.Tool == "" {
			break
		}
		toolName := displayToolName(ev.Progress.Tool, ev.Progress.Args)
		if call, ok := o.toolStarts[ev.Progress.Agent]; ok {
			if toolName != "" && toolName != call.summary {
				call.summary = toolName
				// Phát sự kiện cập nhật chỉ-summary (cùng ID), TUI applyEvent sẽ gộp
				o.emitEv(Event{
					ID:       call.id,
					Time:     call.start,
					Category: "TOOL",
					Agent:    ev.Progress.Agent,
					Summary:  toolName,
					Level:    "info",
					Depth:    call.depth,
				})
			}
			o.updateAgent(ev.Progress.Agent, func(a *agentState) {
				a.state = "working"
				a.tool = ev.Progress.Tool
				a.summary = fmt.Sprintf("%s → %s", ev.Progress.Agent, toolName)
			})
			break
		}
		// Chưa phát sớm → quy trình bình thường
		// (model không stream tool args sẽ không kích hoạt ensureSubagentToolStarted,
		// fallback header phải bù một lần trên đường này, nếu không thì các tool như read_chapter
		// không có extractor sẽ không có đầu ✻ trên panel stream, dán sát ngay đoạn thinking phía trước.)
		id := nextEventID()
		o.toolStarts[ev.Progress.Agent] = &activeCall{id: id, start: time.Now(), summary: toolName, depth: 1}
		o.emitAndLog(Event{
			ID:       id,
			Time:     time.Now(),
			Category: "TOOL",
			Agent:    ev.Progress.Agent,
			Summary:  toolName,
			Level:    "info",
			Depth:    1,
		})
		o.updateAgent(ev.Progress.Agent, func(a *agentState) {
			a.state = "working"
			a.tool = ev.Progress.Tool
			a.summary = fmt.Sprintf("%s → %s", ev.Progress.Agent, toolName)
		})
		o.emitFallbackStreamHeader(ev.Progress.Tool)
	case agentcore.ProgressToolEnd:
		delete(o.streamExtractors, ev.Progress.Agent)
		if ev.Progress.Agent == "" {
			return
		}
		call, ok := o.toolStarts[ev.Progress.Agent]
		if !ok {
			return
		}
		delete(o.toolStarts, ev.Progress.Agent)
		// Sự kiện cập nhật cùng ID: TUI dựa ID định vị dòng TOOL gốc, điền ngược FinishedAt / Duration.
		// Summary / Depth cũng mang theo, bảo đảm replay runtime queue khôi phục được dòng đầy đủ.
		finishEv := Event{
			ID:         call.id,
			Time:       call.start,
			FinishedAt: time.Now(),
			Category:   "TOOL",
			Agent:      ev.Progress.Agent,
			Summary:    call.summary,
			Level:      "info",
			Depth:      call.depth,
			Duration:   time.Since(call.start),
		}
		o.emitEv(finishEv)
		o.persistEvent(finishEv)
	case agentcore.ProgressThinking:
		o.handleThinkingProgress(ev)
	case agentcore.ProgressRetry:
		prefix := fmt.Sprintf(contentlang.Pick("重试 (%d/%d): ", "Thử lại (%d/%d): "), ev.Progress.Attempt, ev.Progress.MaxRetries)
		retryEv := Event{
			Time:     time.Now(),
			Category: "SYSTEM",
			Agent:    ev.Progress.Agent,
			Summary:  prefix + truncate(ev.Progress.Message, 80),
			Detail:   prefix + ev.Progress.Message,
			Kind:     errorKind(nil, ev.Progress.Message),
			Level:    "warn",
			Depth:    1,
		}
		o.emitEv(retryEv)
		o.persistEvent(retryEv)
	case agentcore.ProgressToolError:
		delete(o.streamExtractors, ev.Progress.Agent)
		msg := ev.Progress.Message
		if msg == "" {
			msg = "unknown error"
		}
		// Nếu có dòng TOOL đang diễn ra thì đánh dấu thất bại tại chỗ; nếu không thì nối thêm một dòng ERROR độc lập.
		if call, ok := o.toolStarts[ev.Progress.Agent]; ok {
			delete(o.toolStarts, ev.Progress.Agent)
			finishEv := Event{
				ID:         call.id,
				Time:       call.start,
				FinishedAt: time.Now(),
				Failed:     true,
				Category:   "TOOL",
				Agent:      ev.Progress.Agent,
				Summary:    call.summary,
				Level:      "error",
				Depth:      call.depth,
				Duration:   time.Since(call.start),
			}
			o.emitEv(finishEv)
			o.persistEvent(finishEv)
		}
		// Nối thêm dòng chi tiết ERROR (bổ sung thông tin lỗi, tiện rà soát)
		errEv := Event{
			Time:     time.Now(),
			Category: "ERROR",
			Agent:    ev.Progress.Agent,
			Summary:  fmt.Sprintf(contentlang.Pick("%s 错误: %s", "%s lỗi: %s"), ev.Progress.Tool, truncate(msg, 100)),
			Detail:   fmt.Sprintf(contentlang.Pick("%s 错误: %s", "%s lỗi: %s"), ev.Progress.Tool, msg),
			Kind:     errorKind(nil, msg),
			Level:    "error",
			Depth:    1,
		}
		o.emitEv(errEv)
		o.persistEvent(errEv)
	case agentcore.ProgressContext:
		o.handleContextProgress(ev)
	}
}

// handleSubagentDelta phân luồng văn bản và tham số lời gọi tool của subagent:
// - DeltaText trực tiếp stream ra dưới dạng markdown
// - DeltaToolCall chỉ trích field stream ra với các tool nội dung dài đã biết (như draft_chapter.content); tham số JSON của tool khác thì loại bỏ hết
func (o *observer) handleSubagentDelta(p *agentcore.ProgressPayload) {
	if p.DeltaKind != agentcore.DeltaToolCall {
		o.emitStreamDelta(p.Delta, false)
		return
	}
	if p.Tool == "" {
		return // tên tool chưa sẵn sàng, thử lại ở delta kế tiếp
	}

	// Khi stream nhận diện được tên tool thì phát sớm sự kiện TOOL đang-diễn-ra, cho spinner phủ suốt cả đoạn LLM sinh
	// (nếu không thì "đang diễn ra" của các tool như draft_chapter chỉ hiển thị trong vài chục mili giây Execute thật).
	// Khi ProgressToolStart thật tới, nhận ra toolStarts đã có bản ghi, chỉ bù thêm summary.
	o.ensureSubagentToolStarted(p.Agent, p.Tool)

	cur, ok := o.streamExtractors[p.Agent]
	// Sau khi args của cùng lời gọi tool đã đóng (trúng } cấp đỉnh), vẫn có thể nhận trailing delta:
	// một số provider (deepseek-v4-flash thực nghiệm) chia một lần args thành nhiều chunk,
	// chunk cuối sau `}` còn kèm khoảng trắng hoặc ký tự lặp. Lúc này nếu xử lý theo "khớp tên tool +
	// Done thì dựng lại", extractor mới lại emit một lần ✻ header và parse đoạn token đuôi
	// như args mới. Các delta này là đuôi dư thừa, cứ loại bỏ.
	if ok && cur.tool == p.Tool && cur.ext.Done() {
		return
	}
	// Tên tool đã đổi hoặc chưa dựng bao giờ: dựng mới.
	if !ok || cur.tool != p.Tool {
		ext := newToolExtractor(p.Tool)
		if ext == nil {
			delete(o.streamExtractors, p.Agent)
			return
		}
		cur = &agentExtractor{tool: p.Tool, ext: ext}
		o.streamExtractors[p.Agent] = cur
	}
	if emitted := cur.ext.Feed(p.Delta); emitted != "" {
		if !cur.emittedAny {
			cur.emittedAny = true
			// streamClear cho ✻ header của extractor rơi vào điểm bắt đầu round mới, phối với
			// kiểm tra HasPrefix("✻") của renderStreamContent để đi đường highlight renderAgentBlock;
			// nếu dùng ensureStreamParagraphBreak chỉ chèn dòng trống mà không mở round thì ✻ vẫn bị
			// thinking/chính văn phía trước bao quanh, rơi vào renderChapterBlock bị vẽ mất bằng màu mặc định.
			o.streamClear()
			// streamClear đã phòng thủ xóa sạch streamExtractors. cur hiện tại còn phải tiếp tục Feed
			// các delta về sau của lời gọi tool này, phải lập tức đăng ký lại nó; nếu không thì khi đoạn delta
			// kế tiếp tới sẽ dựng extractor mới, parse từ giữa chừng args (tới `{` của object lồng
			// mới vào psBeforeKey), coi timeline_events.time / foreshadow_updates.id
			// ... như field cấp đỉnh, ✻ header xuất hiện lặp trên TUI.
			o.streamExtractors[p.Agent] = cur
		}
		o.emitStreamDelta(emitted, false)
	}
}

func (o *observer) handleThinkingProgress(ev agentcore.Event) {
	agent := ev.Progress.Agent
	thinking := ev.Progress.Thinking
	if agent == "" || thinking == "" {
		return
	}

	prev := o.lastThinkingByAgent[agent]
	delta := thinking
	if strings.HasPrefix(thinking, prev) {
		delta = thinking[len(prev):]
	}
	o.lastThinkingByAgent[agent] = thinking
	if delta == "" {
		return
	}
	o.emitStreamDelta(delta, true)
}

func (o *observer) handleContextProgress(ev agentcore.Event) {
	if ev.Progress == nil || len(ev.Progress.Meta) == 0 {
		return
	}
	var payload struct {
		Tokens        int     `json:"tokens"`
		ContextWindow int     `json:"context_window"`
		Percent       float64 `json:"percent"`
		Scope         string  `json:"scope"`
		Strategy      string  `json:"strategy"`
	}
	if json.Unmarshal(ev.Progress.Meta, &payload) != nil {
		return
	}

	agent := ev.Progress.Agent
	if agent == "" {
		agent = "coordinator"
	}

	// Cập nhật snapshot agent (sidebar TUI luôn hiển thị)
	o.updateAgent(agent, func(a *agentState) {
		a.context = AgentContextSnapshot{
			Tokens:        payload.Tokens,
			ContextWindow: payload.ContextWindow,
			Percent:       payload.Percent,
			Scope:         payload.Scope,
			Strategy:      payload.Strategy,
		}
	})

	level := "info"
	if payload.Percent > 85 {
		level = "warn"
	}
	summary := fmt.Sprintf(contentlang.Pick("%s 上下文 %.0f%% (%d/%d) 策略: %s", "%s ngữ cảnh %.0f%% (%d/%d) chiến lược: %s"), agent, payload.Percent, payload.Tokens, payload.ContextWindow, payload.Strategy)

	depth := 0
	if agent != "coordinator" {
		depth = 1
	}

	if payload.Strategy != "" {
		// Đã kích hoạt nén → event stream + log
		ctxEv := Event{Time: time.Now(), Category: "SYSTEM", Agent: agent, Summary: summary, Level: level, Depth: depth}
		o.emitEv(ctxEv)
		o.persistEvent(ctxEv)
	} else {
		// Báo cáo tỉ lệ dùng thông thường → chỉ log
		slogLevel := slog.LevelInfo
		if level == "warn" {
			slogLevel = slog.LevelWarn
		}
		slog.Log(context.Background(), slogLevel, summary, "module", "context", "agent", agent)
	}
}

func (o *observer) handleToolEnd(ev agentcore.Event) {
	agent := agentFromEvent(ev)
	// Tool kết thúc: chuyển trạng thái về idle, nếu không sidebar sẽ mãi dừng ở working.
	// Khi phái sub-agent kết thúc thì trạng thái của dispatchTarget được xóa riêng ở bên dưới.
	o.updateAgent(agent, func(a *agentState) {
		a.tool = ""
		a.state = "idle"
	})
	delete(o.lastThinkingByAgent, agent)

	// Lấy bản ghi DISPATCH đang diễn ra (ev.Args của handleToolEnd có thể rỗng, lấy từ currentDispatchTarget)
	var dispatchCall *activeCall
	var dispatchTarget string
	if ev.Tool == "subagent" {
		dispatchTarget = o.currentDispatchTarget
		o.currentDispatchTarget = ""
		if dispatchTarget == "" {
			if sub := parseSubagentArgs(ev.Args); sub.agent != "" {
				dispatchTarget = sub.agent
			}
		}
		if dispatchTarget == "" {
			dispatchTarget = "subagent"
		}
		if call, ok := o.dispatchStarts[dispatchTarget]; ok {
			dispatchCall = call
			delete(o.dispatchStarts, dispatchTarget)
		}
		// Phái kết thúc: reset trạng thái sub-agent về idle (các đường thành công/thất bại/lỗi đều cần dọn này)
		if dispatchTarget != "subagent" {
			o.updateAgent(dispatchTarget, func(a *agentState) {
				a.state = "idle"
				a.tool = ""
			})
		}
	}

	// Lấy bản ghi đang diễn ra của tool trực tiếp của coordinator (không phải subagent) (hiếm, nhưng giữ nhất quán)
	var toolCall *activeCall
	if ev.Tool != "subagent" {
		if call, ok := o.toolStarts[agent]; ok {
			toolCall = call
			delete(o.toolStarts, agent)
		}
	}

	// Trạng thái hoàn thành lời gọi thống nhất (thành công/thất bại), cập nhật dòng gốc qua cùng ID
	emitFinish := func(call *activeCall, category, agentName string, failed bool) {
		if call == nil {
			return
		}
		level := "success"
		if failed {
			level = "error"
		}
		finishEv := Event{
			ID:         call.id,
			Time:       call.start,
			FinishedAt: time.Now(),
			Failed:     failed,
			Category:   category,
			Agent:      agentName,
			Summary:    call.summary,
			Level:      level,
			Depth:      call.depth,
			Duration:   time.Since(call.start),
		}
		o.emitEv(finishEv)
		o.persistEvent(finishEv)
	}
	emitDispatchFinish := func(failed bool) {
		emitFinish(dispatchCall, "DISPATCH", dispatchTarget, failed)
	}
	emitToolFinish := func(failed bool) {
		emitFinish(toolCall, "TOOL", agent, failed)
	}
	// Lưới đỡ: nếu khi subagent kết thúc, bên trong subagent đó vẫn còn lời gọi TOOL chưa hoàn thành (ví dụ ensureSubagentToolStarted
	// đã phát sớm sự kiện đang-diễn-ra, nhưng sau đó abort/context cancel khiến ProgressToolEnd không tới),
	// ở đây ép phát finish, tránh dòng TOOL mãi "đang diễn ra". Trạng thái đồng bộ theo dispatch.
	flushOrphanSubagentTool := func(failed bool) {
		if dispatchTarget == "" {
			return
		}
		call, ok := o.toolStarts[dispatchTarget]
		if !ok {
			return
		}
		delete(o.toolStarts, dispatchTarget)
		delete(o.streamExtractors, dispatchTarget)
		emitFinish(call, "TOOL", dispatchTarget, failed)
	}

	if ev.IsError {
		depth := 0
		if agent != "coordinator" {
			depth = 1
		}
		errText := ""
		if len(ev.Result) > 0 {
			errText = string(ev.Result)
		}
		// ctx-cancel phát sinh từ abort chủ động của người dùng: vẫn phải dọn trạng thái (dòng dispatch / tool phải về trạng thái hoàn thành),
		// nhưng bỏ qua dòng ERROR độc lập + log lỗi, nhất quán với đường EventError.
		if o.isCancellationNoise(nil, errText) {
			slog.Debug("suppressed cancel-derived tool error", "module", "agent", "tool", ev.Tool, "msg", errText)
			flushOrphanSubagentTool(true)
			emitDispatchFinish(true)
			emitToolFinish(true)
			return
		}
		summary := fmt.Sprintf(contentlang.Pick("%s 失败", "%s thất bại"), ev.Tool)
		detail := summary
		kind := ""
		if errText != "" {
			kind = errorKind(nil, errText)
			detail = fmt.Sprintf("%s → %s: %s", agent, ev.Tool, errText)
			summary += ": " + truncate(errText, 120)
		}
		flushOrphanSubagentTool(true)
		emitDispatchFinish(true)
		emitToolFinish(true)
		errEv := Event{
			Time:     time.Now(),
			Category: "ERROR",
			Agent:    agent,
			Summary:  summary,
			Detail:   detail,
			Kind:     kind,
			Level:    "error",
			Depth:    depth,
		}
		o.emitEv(errEv)
		o.persistEvent(errEv)
		return
	}

	if errEv, fullErr := o.subagentResultErrorEvent(ev); errEv != nil {
		if o.isCancellationNoise(nil, fullErr) {
			slog.Debug("suppressed cancel-derived subagent error", "module", "agent", "tool", ev.Tool, "msg", fullErr)
			flushOrphanSubagentTool(true)
			emitDispatchFinish(true)
			return
		}
		if dispatchTarget != "" && dispatchTarget != "subagent" {
			errEv.Agent = dispatchTarget
		}
		flushOrphanSubagentTool(true)
		emitDispatchFinish(true)
		o.emitEv(*errEv)
		o.persistEvent(*errEv)
		return
	}

	// subagent hoàn thành thành công → cập nhật dòng DISPATCH gốc thành trạng thái hoàn thành (kèm thời gian)
	if ev.Tool == "subagent" {
		flushOrphanSubagentTool(false)
		emitDispatchFinish(false)
		return
	}

	// Tool trực tiếp của coordinator hoàn thành thành công
	emitToolFinish(false)
}

func (o *observer) emitStreamDelta(delta string, thinking bool) {
	if delta == "" {
		return
	}
	if thinking != o.streamThinking {
		o.emitD(utils.ThinkingSep)
		o.streamThinking = thinking
	}
	o.emitD(delta)
	o.streamHasContent = true
	o.streamLastByte = delta[len(delta)-1]
}

// ensureSubagentToolStarted khi stream nhận diện tool_call xuất hiện lần đầu thì đăng ký sớm cho agent đó
// một lời gọi TOOL đang-diễn-ra, để spinner của event stream phủ suốt đoạn "LLM stream sinh tham số
// tool_call" (thường chiếm 99% tổng thời gian lời gọi). Lúc này args chưa đầy đủ, tạm dùng tên tool thuần
// làm summary; khi ProgressToolStart thật tới sẽ bù summary kèm tham số.
func (o *observer) ensureSubagentToolStarted(agent, tool string) {
	if agent == "" || tool == "" {
		return
	}
	if _, ok := o.toolStarts[agent]; ok {
		return // đã có lời gọi đang diễn ra, idempotent
	}
	id := nextEventID()
	o.toolStarts[agent] = &activeCall{
		id:      id,
		start:   time.Now(),
		summary: tool, // tạm dùng tên tool thuần, khi ProgressToolStart tới có thể cập nhật thành tool(chương N)
		depth:   1,
	}
	o.emitAndLog(Event{
		ID:       id,
		Time:     time.Now(),
		Category: "TOOL",
		Agent:    agent,
		Summary:  tool,
		Level:    "info",
		Depth:    1,
	})
	o.updateAgent(agent, func(a *agentState) {
		a.state = "working"
		a.tool = tool
	})
	o.emitFallbackStreamHeader(tool)
}

// emitFallbackStreamHeader bù một dòng tiêu đề ✻ vào panel stream cho các tool chưa cấu hình extractor.
// Cả ba đường đều phải gọi để giữ nhất quán:
//  1. ensureSubagentToolStarted —— subagent stream tool args (DeltaToolCall)
//  2. handleToolUpdate ProgressToolStart —— subagent tool args không stream
//  3. handleToolStart —— tool của chính coordinator
//
// Thiếu bất kỳ đường nào thì cùng một tool sẽ thành "writer gọi có ✻, coordinator gọi không ✻" hoặc ngược lại.
func (o *observer) emitFallbackStreamHeader(tool string) {
	if _, has := toolDisplays[tool]; has {
		return // có extractor, header do extractor tự xuất
	}
	o.streamClear()
	o.emitStreamDelta(streamHeaderFallback(tool)+"\n", false)
}

// streamHeaderFallback sinh văn bản header stream cho các tool chưa cấu hình extractor,
// để người dùng dù với tool loại đọc nhẹ cũng thấy được "đang gọi cái gì".
//
// Prefix "✻ " là dấu quy ước "khối điều phối agent" — renderStreamContent của TUI thấy
// prefix này sẽ render theo đường renderAgentBlock (icon + label highlight + đường ngăn),
// nếu không sẽ rơi vào đường khối chính văn dùng màu mặc định của terminal, header trông như chính văn thường không nổi bật.
func streamHeaderFallback(tool string) string {
	label := tool
	switch tool {
	case "ask_user":
		label = contentlang.Pick("向用户提问", "Hỏi người dùng")
	}
	return "✻ " + label
}

// streamClear báo cho TUI mở một streamRound mới, đồng thời reset các trạng thái liên quan đến ngăn đoạn.
// Về mặt logic round mới là "stream rỗng", nếu không thì lần emit đầu tiên của extractor kế tiếp sẽ bù nhầm dòng trống dẫn đầu.
//
// streamThinking phải reset cùng: emitStreamDelta dùng streamThinking để theo dõi xuyên lời gọi xem
// đoạn trước có phải thinking. Trong round mới chưa xuất nội dung nào, lần emit(thinking=false) kế tiếp
// không nên chèn ThinkingSep nữa. Nếu không thì fallback header (như ✻ đọc chương) sẽ bị \x02
// chiếm đầu trước, HasPrefix("✻") của renderStreamContent không khớp, cả đoạn rơi vào đường chính văn
// rồi bị ThinkingSep cắt thành đoạn thinking, màu title bị vẽ thành màu thinking.
func (o *observer) streamClear() {
	o.emitC()
	o.streamHasContent = false
	o.streamLastByte = 0
	o.streamThinking = false
	// ProgressToolEnd của subagent lượt trước đã delete trước khi kết thúc, ở đây xóa phòng thủ.
	if len(o.streamExtractors) > 0 {
		o.streamExtractors = make(map[string]*agentExtractor)
	}
}

func (o *observer) subagentResultErrorEvent(ev agentcore.Event) (*Event, string) {
	if ev.Tool != "subagent" || len(ev.Result) == 0 {
		return nil, ""
	}
	sub := parseSubagentArgs(ev.Args)
	errMsg := parseSubagentResultError(ev.Result)
	if errMsg == "" {
		return nil, ""
	}

	target := "subagent"
	if sub.agent != "" {
		target = sub.agent
	}
	fullErr := fmt.Sprintf(contentlang.Pick("%s 失败: %s", "%s thất bại: %s"), target, errMsg)
	return &Event{
		Time:     time.Now(),
		Category: "ERROR",
		Agent:    "coordinator",
		Summary:  fmt.Sprintf(contentlang.Pick("%s 失败: %s", "%s thất bại: %s"), target, truncate(errMsg, 120)),
		Detail:   fullErr,
		Kind:     errorKind(nil, errMsg),
		Level:    "error",
	}, fullErr
}

func (o *observer) updateAgent(name string, fn func(*agentState)) {
	if name == "" {
		return
	}
	o.agentMu.Lock()
	defer o.agentMu.Unlock()
	a, ok := o.agents[name]
	if !ok {
		a = &agentState{name: name, state: "idle"}
		o.agents[name] = a
	}
	fn(a)
	a.updated = time.Now()
}

func (o *observer) agentSnapshots() []AgentSnapshot {
	o.agentMu.Lock()
	defer o.agentMu.Unlock()
	snaps := make([]AgentSnapshot, 0, len(o.agents))
	for _, a := range o.agents {
		snaps = append(snaps, AgentSnapshot{
			Name:      a.name,
			State:     a.state,
			Summary:   a.summary,
			Tool:      a.tool,
			Turn:      a.turn,
			Context:   a.context,
			UpdatedAt: a.updated,
		})
	}
	return snaps
}

func agentFromEvent(ev agentcore.Event) string {
	if ev.Progress != nil && ev.Progress.Agent != "" {
		return ev.Progress.Agent
	}
	return "coordinator"
}

func displayToolName(tool string, args json.RawMessage) string {
	if len(args) == 0 {
		return tool
	}
	switch tool {
	case "save_foundation":
		var p struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(args, &p) == nil && p.Type != "" {
			return fmt.Sprintf("%s[%s]", tool, p.Type)
		}
	case "commit_chapter", "plan_chapter", "draft_chapter", "check_consistency":
		var p struct {
			Chapter int `json:"chapter"`
		}
		if json.Unmarshal(args, &p) == nil && p.Chapter > 0 {
			return fmt.Sprintf(contentlang.Pick("%s(第%d章)", "%s(chương %d)"), tool, p.Chapter)
		}
	case "save_review":
		var p struct {
			Chapter int    `json:"chapter"`
			Scope   string `json:"scope"`
			Verdict string `json:"verdict"`
		}
		if json.Unmarshal(args, &p) == nil {
			label := ""
			switch p.Scope {
			case "arc":
				label = contentlang.Pick("本弧", "cung này")
			case "global":
				label = contentlang.Pick("全局", "toàn cục")
			default:
				if p.Chapter > 0 {
					label = fmt.Sprintf(contentlang.Pick("第%d章", "chương %d"), p.Chapter)
				}
			}
			if label == "" {
				return tool
			}
			if p.Verdict != "" {
				return fmt.Sprintf("%s(%s·%s)", tool, label, p.Verdict)
			}
			return fmt.Sprintf("%s(%s)", tool, label)
		}
	case "novel_context":
		var p struct {
			Chapter int `json:"chapter"`
		}
		if json.Unmarshal(args, &p) == nil && p.Chapter > 0 {
			return fmt.Sprintf(contentlang.Pick("%s(第%d章)", "%s(chương %d)"), tool, p.Chapter)
		}
	case "read_chapter":
		var p struct {
			Chapter   int    `json:"chapter"`
			Source    string `json:"source"`
			Character string `json:"character"`
		}
		if json.Unmarshal(args, &p) == nil && p.Chapter > 0 {
			suffix := ""
			if p.Character != "" {
				suffix = "·" + p.Character + contentlang.Pick("对话", " thoại")
			} else if p.Source == "draft" {
				suffix = contentlang.Pick("·草稿", "·bản nháp")
			}
			return fmt.Sprintf(contentlang.Pick("%s(第%d章%s)", "%s(chương %d%s)"), tool, p.Chapter, suffix)
		}
	}
	return tool
}

type subagentInvocation struct {
	agent string
	task  string
}

func parseSubagentResultError(result json.RawMessage) string {
	if len(result) == 0 {
		return ""
	}
	// Lỗi phổ biến: object {"error": "..."} (unknown agent / invalid model / sub-agent thực thi thất bại)
	var obj struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(result, &obj); err == nil && obj.Error != "" {
		return obj.Error
	}
	// Tương thích với việc trả lỗi dạng chuỗi trần của agentcore SubAgentTool:
	// "Invalid parameters: ..." / "background mode requires ..." / "Too many parallel tasks ..."
	// Đây là lỗi kiểm tra tham số ở tầng tool, is_error=false nhưng nội dung là mô tả lỗi, cần nhận diện là lỗi để tránh hiểu nhầm là thành công.
	var s string
	if json.Unmarshal(result, &s) == nil && isSubagentErrorString(s) {
		return s
	}
	return ""
}

var subagentErrorPrefixes = []string{
	"Invalid parameters",
	"background mode requires",
	"Too many parallel tasks",
}

func isSubagentErrorString(s string) bool {
	for _, p := range subagentErrorPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func parseSubagentArgs(args json.RawMessage) subagentInvocation {
	if len(args) == 0 {
		return subagentInvocation{}
	}
	var p struct {
		Agent string `json:"agent"`
		Task  string `json:"task"`
	}
	if json.Unmarshal(args, &p) == nil && p.Agent != "" {
		return subagentInvocation{agent: p.Agent, task: p.Task}
	}
	return subagentInvocation{}
}
