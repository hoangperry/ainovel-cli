package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/voocel/agentcore"
	corecontext "github.com/voocel/agentcore/context"
	"github.com/voocel/agentcore/llm"
	"github.com/voocel/agentcore/subagent"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/agents/ctxpack"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/host/reminder"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// logRulesLoaded in tình hình nạp rules ở giai đoạn lắp ráp: thư mục rules của sách, nguồn thực sự đọc được, giá trị kiểm tra số chữ có hiệu lực.
// Đặt file rules sai đường dẫn sẽ bị loader bỏ qua lặng lẽ, nguồn lại không vào LLM (chỉ thấy ở panel /diag), đặt sai mà không có phản hồi là
// trở ngại lớn nhất khi người dùng dò lỗi. Dòng log khởi động này cho "sai đường dẫn / số chữ chưa viết vào front matter" lộ ra ngay tức thì.
func logRulesLoaded(opts rules.LoadOptions) {
	b := rules.Merge(rules.Load(opts))
	words := contentlang.Pick("未设置（不做字数检查）", "Chưa đặt (không kiểm tra số chữ)")
	if w := b.Structured.ChapterWords; w != nil {
		words = fmt.Sprintf("%d-%d", w.Min, w.Max)
	}
	slog.Info(i18n.T("log.agent.rules_loaded"),
		contentlang.Pick("本书规则目录", "Thư mục rules của sách"), opts.ProjectRulesDir,
		contentlang.Pick("已加载来源", "Nguồn đã nạp"), b.Sources,
		contentlang.Pick("章节字数", "Số chữ chương"), words)
}

// agentToRole quy tên subagent về tên role mà ModelSet nhận biết.
// architect_short / architect_long đều dùng chung một cấu hình role architect.
// Đồng nghĩa với host.agentRoleName, vì build và host không phụ thuộc lẫn nhau nên mỗi bên giữ một bản.
func agentToRole(name string) string {
	if strings.HasPrefix(name, "architect_") {
		return "architect"
	}
	return name
}

// subagentMaxRetries là giới hạn retry LLM thống nhất cho mọi SubAgentConfig và Coordinator.
// Chiến lược backoff: lũy thừa 1s/2s/4s/8s/16s (bị ràng buộc bởi giới hạn maxDelay), ưu tiên tuân theo server Retry-After.
// Kết hợp ToolsAreIdempotent=true để các lỗi retryable như stream-idle / 503 / nhiễu mạng thoáng qua
// được retry ngay tại lớp subagent, thay vì ném cả subagent về coordinator để phái lại.
// Luật sắt số một của dự án đảm bảo các tool dạng ghi đi qua checkpoint+digest idempotent nên retry là an toàn.
const subagentMaxRetries = 5

// UsageRecorder là callback dùng lượng tùy chọn của BuildCoordinator; chữ ký giống OnMessage,
// mỗi message của agent đều gọi một lần, lớp Host lo việc tổng hợp. nil nghĩa là không theo dõi.
type UsageRecorder func(agentName string, msg agentcore.AgentMessage)

// ApplyThinking áp dụng cường độ suy nghĩ của một role cụ thể vào live agent (dùng khi điều chỉnh /model lúc runtime).
// coordinator → Agent.SetThinkingLevel; architect → hai subagent architect_*;
// writer/editor → subagent tương ứng. level rỗng = dùng mặc định của model/provider. Các tên role khác thì bỏ qua.
type ApplyThinking func(role string, level agentcore.ThinkingLevel)

// ParseThinkingLevel đổi chuỗi cấu hình thành agentcore.ThinkingLevel.
// "" là hợp lệ (= không ghi đè/kế thừa); các giá trị khác phải là một trong off/minimal/low/medium/high/xhigh/max,
// nếu không thì trả về error (lúc khởi động hạ cấp thành rỗng và warn, lúc runtime hiển thị error lại cho người dùng).
func ParseThinkingLevel(s string) (agentcore.ThinkingLevel, error) {
	lv := agentcore.NormalizeThinkingLevel(agentcore.ThinkingLevel(s))
	switch lv {
	case "", agentcore.ThinkingOff, agentcore.ThinkingMinimal, agentcore.ThinkingLow,
		agentcore.ThinkingMedium, agentcore.ThinkingHigh, agentcore.ThinkingXHigh,
		agentcore.ThinkingMax:
		return lv, nil
	default:
		return "", fmt.Errorf(contentlang.Pick("无效思考强度 %q（可选：off/minimal/low/medium/high/xhigh/max）", "Cường độ suy nghĩ không hợp lệ %q (chọn: off/minimal/low/medium/high/xhigh/max)"), s)
	}
}

func ResolveThinkingForModel(model agentcore.ChatModel, level agentcore.ThinkingLevel) (agentcore.ThinkingLevel, bool) {
	return llm.ThinkingPolicyFor(model).Resolve(level)
}

func AvailableThinkingForModel(model agentcore.ChatModel) []agentcore.ThinkingLevel {
	return llm.ThinkingPolicyFor(model).Available
}

// roleThinking phân giải cường độ suy nghĩ có hiệu lực cho một role; giá trị không hợp lệ thì hạ cấp thành rỗng (không ghi đè) và warn.
func roleThinking(cfg bootstrap.Config, role string) agentcore.ThinkingLevel {
	lv, err := ParseThinkingLevel(cfg.ResolveThinking(role))
	if err != nil {
		slog.Warn(i18n.T("log.agent.invalid_thinking_config"), "module", "agent", "role", role, "err", err)
		return ""
	}
	return lv
}

func resolvedRoleThinking(model agentcore.ChatModel, cfg bootstrap.Config, role string) agentcore.ThinkingLevel {
	resolved, _ := ResolveThinkingForModel(model, roleThinking(cfg, role))
	return resolved
}

// BuildCoordinator lắp ráp Coordinator Agent và các SubAgent của nó.
// Trả về Agent, AskUserTool, WriterRestorePack, tham chiếu ContextEngine của Coordinator,
// cùng closure ApplyThinking — khi lớp Host chuyển /model cần gọi trực tiếp SetContextWindow +
// SetReserveTokens để liên động cửa sổ của model mới (writer/architect/editor đi qua ContextManagerFactory
// tự dựng lại, không cần ref; chỉ coordinator thường trú mới cần), và liên động cường độ suy nghĩ của từng role
// qua ApplyThinking. Lớp Host lấy luồng sự kiện qua Agent.Subscribe, không còn cần callback emit nữa.
func BuildCoordinator(
	cfg bootstrap.Config,
	store *store.Store,
	models *bootstrap.ModelSet,
	bundle assets.Bundle,
	recordUsage UsageRecorder,
) (*agentcore.Agent, *tools.AskUserTool, *ctxpack.WriterRestorePack, *corecontext.ContextEngine, ApplyThinking) {
	// tool dùng chung
	rulesOpts := rules.DefaultOptions(bundle.RulesFS)
	logRulesLoaded(rulesOpts)
	contextTool := tools.NewContextTool(store, bundle.References, cfg.Style, rulesOpts)
	readChapter := tools.NewReadChapterTool(store)
	askUser := tools.NewAskUserTool()

	architectTools := []agentcore.Tool{
		contextTool,
		tools.NewSaveFoundationTool(store),
	}
	writerTools := []agentcore.Tool{
		contextTool,
		readChapter,
		tools.NewPlanChapterTool(store),
		tools.NewDraftChapterTool(store),
		tools.NewEditChapterTool(store),
		tools.NewCheckConsistencyTool(store),
		tools.NewCommitChapterTool(store).WithRules(rulesOpts).WithOutputLang(cfg.ResolveOutputLang()),
	}
	editorTools := []agentcore.Tool{
		contextTool,
		readChapter,
		tools.NewSaveReviewTool(store),
		tools.NewSaveArcSummaryTool(store),
		tools.NewSaveVolumeSummaryTool(store),
	}

	// Provider failover chỉ ghi log, không thông báo cho host
	reportFailover := func(ev bootstrap.FailoverEvent) {
		slog.Warn(i18n.T("log.agent.provider_switch"),
			"module", "agent",
			"role", ev.Role,
			"reason", ev.Reason,
			"from", fmt.Sprintf("%s/%s", ev.FromProvider, ev.FromModel),
			"to", fmt.Sprintf("%s/%s", ev.ToProvider, ev.ToModel),
			"err", ev.Err,
		)
	}

	architectModel := models.ForRoleWithFailover("architect", reportFailover)
	writerModel := models.ForRoleWithFailover("writer", reportFailover)
	editorModel := models.ForRoleWithFailover("editor", reportFailover)
	coordinatorModel := models.ForRoleWithFailover("coordinator", reportFailover)

	// ContextManager của Coordinator được sinh một lần khi dựng Agent, phân giải theo model khởi động.
	// Khi chạy mà /model chuyển sang model có cửa sổ nhỏ hơn, khuyến nghị người dùng cấu hình context_window tường minh để dự phòng.
	_, coordinatorModelName, _ := models.CurrentSelection("coordinator")
	coordinatorContextWindow, coordinatorSource := cfg.ResolveContextWindow(coordinatorModelName)
	// ContextManager của Writer được factory dựng lại mỗi lần gọi, cửa sổ bám động theo model swap (xem factory bên dưới).
	_, writerModelName, _ := models.CurrentSelection("writer")
	writerContextWindow, writerSource := cfg.ResolveContextWindow(writerModelName)
	bootstrap.LogContextWindowChoice("coordinator", coordinatorModelName, coordinatorContextWindow, coordinatorSource)
	bootstrap.LogContextWindowChoice("writer", writerModelName, writerContextWindow, writerSource)

	// modelLookup khi ghi vào session thì gắn _meta:{provider,model} cho mỗi message assistant,
	// để replay không còn phụ thuộc "ModelSet hiện tại" mà suy ngược cost lịch sử, chuyển model lúc chạy vẫn tính chính xác.
	modelLookup := func(agentName string) (string, string) {
		role := agentToRole(agentName)
		provider, name, _ := models.CurrentSelection(role)
		return provider, name
	}
	baseOnMsg := store.Sessions.SubAgentLogger(modelLookup)
	onMsg := func(agentName, task string, msg agentcore.AgentMessage) {
		baseOnMsg(agentName, task, msg)
		if recordUsage != nil {
			recordUsage(agentName, msg)
		}
	}
	baseCoordinatorLog := store.Sessions.CoordinatorLogger(modelLookup)
	coordinatorOnMessage := func(msg agentcore.AgentMessage) {
		baseCoordinatorLog(msg)
		if recordUsage != nil {
			recordUsage("coordinator", msg)
		}
	}

	architectStopGuardFactory := func(_, _ string) agentcore.StopGuard {
		return reminder.NewArchitectStopGuard(store)
	}
	architectThinking, _ := ResolveThinkingForModel(architectModel, roleThinking(cfg, "architect"))
	architectShort := subagent.Config{
		Name:               "architect_short",
		Description:        contentlang.Pick("短篇规划师：为单卷、单冲突、高密度故事生成紧凑设定与扁平大纲", "Nhà quy hoạch truyện ngắn: sinh thiết lập gọn và dàn ý phẳng cho truyện đơn quyển, đơn xung đột, mật độ cao"),
		Model:              architectModel,
		SystemPrompt:       bundle.Prompts.ArchitectShort,
		Tools:              architectTools,
		MaxTurns:           15,
		MaxRetries:         subagentMaxRetries,
		ThinkingLevel:      architectThinking,
		ToolsAreIdempotent: true,
		OnMessage:          onMsg,
		StopAfterToolResult: func(toolName string, result json.RawMessage) bool {
			r := decodeSaveFoundationResult(toolName, result)
			return r.Type == "outline" && r.FoundationReady
		},
		StopGuardFactory: architectStopGuardFactory,
	}
	architectLong := subagent.Config{
		Name:                "architect_long",
		Description:         contentlang.Pick("长篇规划师：为连载型、可持续升级的故事生成分层设定与卷弧大纲", "Nhà quy hoạch truyện dài: sinh thiết lập phân tầng và dàn ý quyển-cung cho truyện kiểu nhiều kỳ, leo thang bền vững"),
		Model:               architectModel,
		SystemPrompt:        bundle.Prompts.ArchitectLong,
		Tools:               architectTools,
		MaxTurns:            20,
		MaxRetries:          subagentMaxRetries,
		ThinkingLevel:       architectThinking,
		ToolsAreIdempotent:  true,
		OnMessage:           onMsg,
		StopAfterToolResult: architectLongShouldStopAfterToolResult,
		StopGuardFactory:    architectStopGuardFactory,
	}

	writerPrompt := bundle.Prompts.Writer
	if style, ok := bundle.Styles[cfg.Style]; ok {
		writerPrompt += "\n\n" + style
	}

	restore := &ctxpack.WriterRestorePack{}
	restore.Refresh(store)

	writer := subagent.Config{
		Name:               "writer",
		Description:        contentlang.Pick("创作者：自主完成一章的构思、写作、自审和提交", "Người sáng tác: tự chủ hoàn thành cấu tứ, viết, tự rà soát và nộp một chương"),
		Model:              writerModel,
		SystemPrompt:       writerPrompt,
		Tools:              writerTools,
		MaxTurns:           30,
		MaxRetries:         subagentMaxRetries,
		ThinkingLevel:      resolvedRoleThinking(writerModel, cfg, "writer"),
		ToolsAreIdempotent: true,
		StopAfterTools:     []string{"commit_chapter"},
		OnMessage:          onMsg,
		StopGuardFactory: func(_, _ string) agentcore.StopGuard {
			return reminder.NewWriterStopGuard(store)
		},
		ContextManagerFactory: func(model agentcore.ChatModel) agentcore.ContextManager {
			// mỗi lần gọi subagent(writer) đều dựng lại, đọc tên model mới nhất từ runModel hiện tại.
			// sau khi /model chuyển writer thì chương kế tiếp tự dùng cửa sổ mới.
			window, _ := cfg.ResolveContextWindow(bootstrap.ModelName(model))
			return newContextManager(contextManagerConfig{
				Model:            model,
				ContextWindow:    window,
				ReserveTokens:    bootstrap.CompactReserveTokens(window),
				KeepRecentTokens: 20000,
				Agent:            "writer",
				ToolMicrocompact: &corecontext.ToolResultMicrocompactConfig{
					IdleThreshold: 5 * time.Minute,
				},
				ExtraStrategies: []corecontext.Strategy{
					ctxpack.NewStoreSummaryCompact(ctxpack.StoreSummaryCompactConfig{
						Store:            store,
						KeepRecentTokens: 20000,
					}),
				},
				Summary: &corecontext.FullSummaryConfig{
					PostSummaryHooks:    []corecontext.PostSummaryHook{restore.Hook()},
					SystemPrompt:        ctxpack.WriterSummarySystemPrompt(),
					SummaryPrompt:       ctxpack.WriterSummaryPrompt(),
					UpdateSummaryPrompt: ctxpack.WriterUpdateSummaryPrompt(),
					TurnPrefixPrompt:    ctxpack.WriterTurnPrefixPrompt(),
				},
			})
		},
	}

	editor := subagent.Config{
		Name:               "editor",
		Description:        contentlang.Pick("审阅者：阅读原文，从结构和审美两个层面发现问题", "Người rà soát: đọc nguyên văn, phát hiện vấn đề ở hai tầng kết cấu và thẩm mỹ"),
		Model:              editorModel,
		SystemPrompt:       bundle.Prompts.Editor,
		Tools:              editorTools,
		MaxTurns:           20,
		MaxRetries:         subagentMaxRetries,
		ThinkingLevel:      resolvedRoleThinking(editorModel, cfg, "editor"),
		ToolsAreIdempotent: true,
		OnMessage:          onMsg,
		// chỉ các sản phẩm trạng thái cuối dạng tóm tắt trúng là dừng; save_review không còn dừng cứng nữa — thoát qua StopAfterTool sẽ vòng qua
		// StopGuard (agentcore loop.go), nếu save_review dừng cứng thì editor "bị phái sinh tóm tắt cung truyện nhưng lại rà soát trước"
		// sẽ bị chặt đứt tại save_review, không tới được save_arc_summary. Việc kết thúc tác vụ rà soát/tóm tắt
		// chuyển sang cho NewEditorStopGuard nhận biết tác vụ kiểm soát.
		StopAfterToolResult: func(toolName string, _ json.RawMessage) bool {
			return toolName == "save_arc_summary" || toolName == "save_volume_summary"
		},
		StopGuardFactory: func(_, task string) agentcore.StopGuard {
			return reminder.NewEditorStopGuard(store, task)
		},
	}

	subagentTool := subagent.New(architectShort, architectLong, writer, editor)

	coordinatorEngine := newContextManager(contextManagerConfig{
		Model:            coordinatorModel,
		ContextWindow:    coordinatorContextWindow,
		ReserveTokens:    bootstrap.CompactReserveTokens(coordinatorContextWindow),
		KeepRecentTokens: 30000,
		Agent:            "coordinator",
		CommitOnProject:  true,
	})

	agent := agentcore.NewAgent(
		agentcore.WithModel(coordinatorModel),
		agentcore.WithSystemPrompt(bundle.Prompts.Coordinator),
		agentcore.WithTools(subagentTool, contextTool, tools.NewSaveDirectiveTool(store), tools.NewReopenBookTool(store)),
		agentcore.WithMaxTurns(100_000),
		agentcore.WithOnMessage(coordinatorOnMessage),
		agentcore.WithToolsAreIdempotent(true),
		// subagent là kênh chính của quy trình; lỗi thật nên trả về tường minh cho Host, chứ không vô hiệu hóa tool vĩnh viễn trong một lần run.
		agentcore.WithMaxToolErrors(0),
		agentcore.WithMaxRetries(subagentMaxRetries),
		agentcore.WithContextManager(coordinatorEngine),
		agentcore.WithStopGuard(reminder.NewStopGuard(store, nil)),
		// khi phase=complete thì chặn cứng việc phái subagent, tránh Writer rơi vào vòng lặp vô hạn.
		agentcore.WithToolGate(combineToolGates(
			completePhaseGate(store),
			writerExpandedChapterGate(store),
		)),
	)
	// Cường độ suy nghĩ của Coordinator: áp dụng kết quả phân giải vô điều kiện. Khi chưa cấu hình thì rỗng (không gửi thinking, dùng mặc định
	// của provider), nhất quán với các subagent (Config.ThinkingLevel mặc định rỗng) — tránh ghi đè mặc định
	// ThinkingLow của agentcore mà ép gửi low cho mọi provider (gồm cả GLM/Ollama vốn bị ép bật thinking).
	coordinatorThinking, _ := ResolveThinkingForModel(models.ForRole("coordinator"), roleThinking(cfg, "coordinator"))
	agent.SetThinkingLevel(coordinatorThinking)

	// liên động cường độ suy nghĩ của từng role lúc runtime: coordinator đi qua Agent, subagent đi qua override của subagentTool.
	applyThinking := func(role string, level agentcore.ThinkingLevel) {
		switch role {
		case "coordinator":
			level, _ = ResolveThinkingForModel(models.ForRole("coordinator"), level)
			agent.SetThinkingLevel(level)
		case "architect":
			level, _ = ResolveThinkingForModel(models.ForRole("architect"), level)
			subagentTool.SetThinkingLevel("architect_short", level)
			subagentTool.SetThinkingLevel("architect_long", level)
		case "writer", "editor":
			level, _ = ResolveThinkingForModel(models.ForRole(role), level)
			subagentTool.SetThinkingLevel(role, level)
		}
	}

	return agent, askUser, restore, coordinatorEngine, applyThinking
}

// completePhaseGate trả về một ToolGate: khi phase=complete thì từ chối mọi lần phái subagent.
// Tránh việc Coordinator LLM vẫn gọi Writer/Architect sau khi sách hoàn thành dẫn tới vòng lặp vô hạn.
func completePhaseGate(st *store.Store) agentcore.ToolGate {
	return func(_ context.Context, req agentcore.GateRequest) (*agentcore.GateDecision, error) {
		if req.Call.Name != "subagent" {
			return nil, nil
		}
		// fail-open: khi Load lỗi hoặc progress rỗng thì luôn cho qua, không vì lỗi đọc tức thời mà kẹt việc phái bình thường.
		// Cái giá duy nhất là khi giai đoạn complete trùng đúng lúc đọc lỗi thì deadlock có thể tái diễn (xác suất cực thấp, chấp nhận được).
		progress, _ := st.Progress.Load()
		if progress != nil && progress.Phase == domain.PhaseComplete {
			return &agentcore.GateDecision{
				Allowed: false,
				Reason:  contentlang.Pick("全书已完成（phase=complete），不能直接派子代理。若用户要返工已写章节，请先调用 reopen_book(chapters=[...]) 把书重新打开进入返工态（之后会自动派 writer 重写）；若用户要新增剧情，告知需新建项目。", "Cả sách đã hoàn thành (phase=complete), không thể giao thẳng sub-agent. Nếu người dùng muốn làm lại chương đã viết, hãy gọi reopen_book(chapters=[...]) để mở lại sách vào trạng thái làm lại (sau đó sẽ tự động giao writer viết lại); nếu người dùng muốn thêm tình tiết, báo rằng cần tạo dự án mới."),
			}, nil
		}
		return nil, nil
	}
}

func combineToolGates(gates ...agentcore.ToolGate) agentcore.ToolGate {
	return func(ctx context.Context, req agentcore.GateRequest) (*agentcore.GateDecision, error) {
		for _, gate := range gates {
			if gate == nil {
				continue
			}
			decision, err := gate(ctx, req)
			if err != nil {
				return nil, err
			}
			if decision != nil && !decision.Allowed {
				return decision, nil
			}
		}
		return nil, nil
	}
}

func writerExpandedChapterGate(st *store.Store) agentcore.ToolGate {
	return func(_ context.Context, req agentcore.GateRequest) (*agentcore.GateDecision, error) {
		if req.Call.Name != "subagent" {
			return nil, nil
		}
		var args struct {
			Agent string `json:"agent"`
			Task  string `json:"task"`
		}
		if err := json.Unmarshal(req.Call.Args, &args); err != nil || args.Agent != "writer" {
			return nil, nil
		}
		chapter := chapterFromTask(args.Task)
		if chapter <= 0 {
			chapter = writerFallbackChapter(st)
		}
		if chapter <= 0 {
			return nil, nil
		}
		if err := tools.EnsureChapterExpanded(st, chapter); err != nil {
			return &agentcore.GateDecision{
				Allowed: false,
				Reason:  err.Error() + contentlang.Pick("。请改派 architect_long，调用 save_foundation(type=expand_arc) 展开下一弧，或 type=append_volume 追加并展开下一卷后再派 writer。", ". Hãy chuyển giao architect_long, gọi save_foundation(type=expand_arc) để triển khai cung tiếp theo, hoặc type=append_volume để thêm rồi triển khai quyển tiếp theo trước khi giao writer."),
			}, nil
		}
		return nil, nil
	}
}

func writerFallbackChapter(st *store.Store) int {
	if st == nil {
		return 0
	}
	progress, err := st.Progress.Load()
	if err != nil || progress == nil {
		return 0
	}
	if len(progress.PendingRewrites) > 0 {
		return progress.PendingRewrites[0]
	}
	return progress.NextChapter()
}

var chapterTaskRe = regexp.MustCompile(`第\s*(\d+)\s*章`)

func chapterFromTask(task string) int {
	m := chapterTaskRe.FindStringSubmatch(task)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

type saveFoundationResult struct {
	Type            string `json:"type"`
	FoundationReady bool   `json:"foundation_ready"`
}

func decodeSaveFoundationResult(toolName string, result json.RawMessage) saveFoundationResult {
	if toolName != "save_foundation" {
		return saveFoundationResult{}
	}
	var r saveFoundationResult
	_ = json.Unmarshal(result, &r)
	return r
}

func architectLongShouldStopAfterToolResult(toolName string, result json.RawMessage) bool {
	r := decodeSaveFoundationResult(toolName, result)
	switch r.Type {
	case "expand_arc", "complete_book":
		return true
	default:
		return false
	}
}
