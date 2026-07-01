package host

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/voocel/agentcore"
	corecontext "github.com/voocel/agentcore/context"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/agents"
	"github.com/voocel/ainovel-cli/internal/agents/ctxpack"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/host/exp"
	"github.com/voocel/ainovel-cli/internal/host/flow"
	"github.com/voocel/ainovel-cli/internal/host/imp"
	"github.com/voocel/ainovel-cli/internal/host/sim"
	"github.com/voocel/ainovel-cli/internal/i18n"
	modelreg "github.com/voocel/ainovel-cli/internal/models"
	"github.com/voocel/ainovel-cli/internal/notify"
	"github.com/voocel/ainovel-cli/internal/rules"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// Host là lớp vỏ mỏng của runtime.
// Trách nhiệm: khởi động/khôi phục/inject can thiệp/chiếu sự kiện/quản lý model.
// Không đưa ra bất kỳ quyết định điều phối nào, không tự chạy tiếp khi rảnh.
type Host struct {
	cfg               bootstrap.Config
	bundle            assets.Bundle
	store             *storepkg.Store
	models            *bootstrap.ModelSet
	coordinator       *agentcore.Agent
	coordinatorCtxMgr *corecontext.ContextEngine // khi đổi model default/coordinator thì liên động SetContextWindow + SetReserveTokens
	thinkingApplier   agents.ApplyThinking       // khi /model chỉnh cường độ thinking thì liên động live agent (coordinator + sub-agent)
	askUser           *tools.AskUserTool
	writerRestore     *ctxpack.WriterRestorePack
	observer          *observer
	router            *flow.Dispatcher
	routerDetach      func()
	usage             *UsageTracker
	usageCancel       context.CancelFunc // dừng autoSaveLoop và kích hoạt flush lần cuối
	budget            *BudgetSentinel    // chính sách budget; chưa bật thì nil (method nil-safe)
	budgetDetach      func()
	notifier          *notify.Notifier // cảnh báo không người trực; chưa bật thì nil (Send nil-safe)

	events   chan Event
	streamCh chan string
	done     chan struct{}

	mu         sync.Mutex
	lifecycle  lifecycle
	cocreating bool // chiếm dụng đồng sáng tạo theo giai đoạn: trong cửa sổ paused chặn can thiệp đồng thời của import/simulate/continue
	closeOnce  sync.Once
}

type lifecycle string

const (
	lifecycleIdle      lifecycle = "idle"
	lifecycleRunning   lifecycle = "running"
	lifecyclePaused    lifecycle = "paused"
	lifecycleCompleted lifecycle = "completed"
)

// New tạo Host.
func New(cfg bootstrap.Config, bundle assets.Bundle) (*Host, error) {
	cfg.FillDefaults()
	if err := cfg.ValidateBase(); err != nil {
		return nil, err
	}
	slog.Info(i18n.T("log.host.starting"), "module", "boot", "provider", cfg.Provider, "model", cfg.ModelName, "output", cfg.OutputDir)

	// Khởi goroutine nền refresh metadata model từ OpenRouter (window/giá), cache đĩa 24h.
	modelreg.StartPricingRefresh(modelreg.DefaultRegistry(), bootstrap.DefaultConfigDir())

	store := storepkg.NewStore(cfg.OutputDir)
	if err := store.Init(); err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	models, err := bootstrap.NewModelSet(cfg)
	if err != nil {
		return nil, fmt.Errorf("create models: %w", err)
	}
	slog.Info(i18n.T("log.host.models_ready"), "module", "boot", "summary", models.Summary())

	usage := NewUsageTracker(models, store)
	// Ưu tiên đọc meta/usage.json; các trường hợp sau đều dùng sessions/*.jsonl để backfill một lần:
	//   - file không tồn tại (lần đầu nâng cấp lên bản có persistence)
	//   - phiên bản schema không khớp (loại bỏ định dạng cũ sau khi nâng cấp về sau)
	//   - file tồn tại nhưng hỏng / lỗi IO (không thể để dữ liệu hỏng làm tích lũy về zero vĩnh viễn)
	// Backfill xong thì SaveNow ngay, cố định kết quả lại, lần khởi động sau Load là trúng.
	loaded, loadErr := usage.LoadFromStore()
	if loadErr != nil {
		slog.Warn(i18n.T("log.usage.load_failed_replay"), "module", "usage", "err", loadErr)
	}
	if !loaded {
		if n, err := usage.ReplaySessions(cfg.OutputDir); err != nil {
			slog.Warn(i18n.T("log.usage.replay_failed"), "module", "usage", "err", err)
		} else if n > 0 {
			slog.Info(i18n.T("log.usage.replay_done"), "module", "usage", "messages", n)
			if err := usage.SaveNow(); err != nil {
				slog.Warn(i18n.T("log.usage.replay_save_failed"), "module", "usage", "err", err)
			}
		}
	}
	usageCtx, usageCancel := context.WithCancel(context.Background())
	usage.StartAutoSave(usageCtx)

	coordinator, askUser, restore, coordinatorCtxMgr, applyThinking := agents.BuildCoordinator(cfg, store, models, bundle, usage.Record)
	store.Signals.ClearStaleSignals()

	h := &Host{
		cfg:               cfg,
		bundle:            bundle,
		store:             store,
		models:            models,
		coordinator:       coordinator,
		coordinatorCtxMgr: coordinatorCtxMgr,
		thinkingApplier:   applyThinking,
		askUser:           askUser,
		writerRestore:     restore,
		usage:             usage,
		usageCancel:       usageCancel,
		events:            make(chan Event, 100),
		streamCh:          make(chan string, 256),
		done:              make(chan struct{}, 4),
		lifecycle:         lifecycleIdle,
	}
	h.observer = newObserver(coordinator, store, h.emitEvent, h.emitDelta, h.emitClear)
	if cfg.Notify.IsEnabled() {
		h.notifier = notify.New(cfg.Notify.Command, cfg.Notify.Events)
	}
	// Đăng ký sentinel budget phải trước Dispatcher: trên cùng một sự kiện biên sub-agent, Abort và FollowUp
	// tranh chấp, Sentinel bật Abort trước thì việc phái của Dispatcher tự nhiên hụt, tầng route không hay biết budget.
	if sentinel := NewBudgetSentinel(cfg.Budget,
		func() float64 { c, _, _, _, _ := usage.Totals(); return c },
		func(reason string) { h.abortWithEvent(reason, "error") },
		func(level, summary string) {
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: level})
			h.notifier.Send(notify.Notification{Kind: "budget", Level: level, Title: i18n.T("notify.title.budget"), Body: summary})
		},
	); sentinel != nil {
		h.budget = sentinel
		usage.SetOnCost(sentinel.OnCost)
		h.budgetDetach = coordinator.Subscribe(sentinel.HandleEvent)
		// Cảnh báo vùng mù tính phí: khi model không báo usage thì cost luôn 0, budget không bao giờ kích hoạt — cầu chì chưa nối phải gọi người.
		usage.SetOnMissingUsage(func() {
			blind := i18n.T("notify.budget.blind")
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: blind, Level: "warn"})
			h.notifier.Send(notify.Notification{Kind: "budget", Level: "warn", Title: i18n.T("notify.title.budget"), Body: blind})
		})
	}
	h.router = flow.NewDispatcher(coordinator, store)
	// Cảnh báo chỉ thị lặp: thuần telemetry, khi treo máy thì "model có thể đang quẩn tại chỗ" đáng để gọi người liếc qua.
	// Event stream và notify phát thành cặp — notify chỉ là bản sao ngoài màn hình của sự kiện trên màn hình (kiến trúc §2.3).
	h.router.SetOnRepeat(func(agent, task string, n int) {
		body := i18n.Tf("notify.repeat.body", n, agent, task)
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: i18n.T("notify.repeat.event_prefix") + body, Level: "warn"})
		h.notifier.Send(notify.Notification{Kind: "repeat", Level: "warn", Title: i18n.T("notify.title.repeat"), Body: body})
	})
	h.routerDetach = h.router.Attach()

	if err := store.RunMeta.Init(cfg.Style, cfg.Provider, cfg.ModelName); err != nil {
		slog.Error(i18n.T("log.host.run_meta_init_failed"), "module", "boot", "err", err)
	}

	return h, nil
}

// ── vòng đời ──

// Start chế độ tạo mới: khởi tạo tiến độ và khởi động vòng lặp dài của coordinator.
func (h *Host) Start(prompt string) error {
	return h.StartPrepared(BuildStartPrompt(prompt))
}

// StartPrepared dùng prompt khởi động đã được dàn dựng xong để bắt đầu sáng tác.
func (h *Host) StartPrepared(promptText string) error {
	h.mu.Lock()
	if h.lifecycle == lifecycleRunning {
		h.mu.Unlock()
		return fmt.Errorf("already running")
	}
	if h.cocreating {
		h.mu.Unlock()
		return errors.New(contentlang.Pick("阶段共创进行中，请先结束共创", "Đang trong đồng sáng tạo theo giai đoạn, hãy kết thúc đồng sáng tạo trước"))
	}
	h.mu.Unlock()

	promptText = strings.TrimSpace(promptText)
	if promptText == "" {
		return fmt.Errorf("prompt is required")
	}
	if err := h.budget.Refuse(); err != nil {
		return err
	}
	if err := h.store.Checkpoints.Reset(); err != nil {
		return fmt.Errorf("reset checkpoints: %w", err)
	}
	if err := h.store.Progress.Init("", 0); err != nil {
		return fmt.Errorf("init progress: %w", err)
	}

	slog.Info(i18n.T("log.host.create_start"), "module", "host", "prompt_len", len(promptText))
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: contentlang.Pick("开始创作", "Bắt đầu sáng tác"), Level: "info"})
	h.observer.setAborting(false)
	// Reset theo dõi lặp và bật route trước, rồi mới khởi động Prompt, tránh sự kiện lượt đầu tới trước Enable
	h.router.ResetRepeat()
	h.router.Enable()
	if err := h.coordinator.Prompt(context.Background(), promptText); err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	// Chủ động phái chỉ thị đầu tiên một lần: nếu đã vào giai đoạn viết (Phase=Writing), Host hạ lệnh ngay;
	// ở giai đoạn lập kế hoạch Route trả về nil, không side effect.
	h.router.Dispatch()

	h.mu.Lock()
	h.lifecycle = lifecycleRunning
	h.mu.Unlock()
	go h.waitDone()
	return nil
}

// Resume chế độ khôi phục: từ checkpoint + progress sinh resume prompt rồi khởi động.
func (h *Host) Resume() (string, error) {
	h.mu.Lock()
	if h.lifecycle == lifecycleRunning {
		h.mu.Unlock()
		return "", fmt.Errorf("already running")
	}
	if h.cocreating {
		h.mu.Unlock()
		return "", errors.New(contentlang.Pick("阶段共创进行中，请先结束共创", "Đang trong đồng sáng tạo theo giai đoạn, hãy kết thúc đồng sáng tạo trước"))
	}
	h.mu.Unlock()

	prompt, label, err := buildResumePrompt(h.store)
	if err != nil {
		return "", err
	}
	if label == "" {
		return "", nil // chế độ tạo mới, không có gì để khôi phục
	}
	if err := h.budget.Refuse(); err != nil {
		return "", err
	}

	slog.Info(i18n.T("log.host.create_resume"), "module", "host", "label", label)
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: contentlang.Pick("恢复创作: ", "Khôi phục sáng tác: ") + label, Level: "info"})
	for _, w := range h.store.CheckConsistency() {
		slog.Warn(i18n.T("log.host.consistency_warning"), "module", "host", "detail", w)
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: contentlang.Pick("一致性告警: ", "Cảnh báo nhất quán: ") + w, Level: "warn"})
	}
	h.refreshWriterRestore()
	h.observer.setAborting(false)
	h.router.ResetRepeat()
	h.router.Enable()
	if err := h.coordinator.Prompt(context.Background(), prompt); err != nil {
		return "", fmt.Errorf("resume prompt: %w", err)
	}
	// Chủ động phái chỉ thị đầu tiên một lần, tránh việc Coordinator chỉ trả lời chữ với resume prompt khiến StopGuard chặn lặp đi lặp lại.
	h.router.Dispatch()

	h.mu.Lock()
	h.lifecycle = lifecycleRunning
	h.mu.Unlock()
	go h.waitDone()
	return label, nil
}

// interventionMsg gói văn bản người dùng thành thông điệp can thiệp mà Coordinator nhận diện được.
// Steer và Continue dùng chung một framing: chỉ thị người dùng ở cả hai lối vào đều mang prefix `[用户干预]`,
// mới kích hoạt ổn định phân loại can thiệp của coordinator.md. Nếu không, văn bản trần của Continue sẽ né luật route,
// Coordinator mất mỏ neo phân loại nên phái nhầm sub-agent (từng khiến "sửa chương đã viết" bị phái cho writer đụng guard edit_chapter).
func interventionMsg(text string) agentcore.Message {
	return agentcore.UserMsg("[用户干预] " + text)
}

// Continue tiếp tục bằng prompt chỉ định. Gọi khi người dùng nhập vào ô nhập sau khi đã dừng máy.
func (h *Host) Continue(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("text is required")
	}
	h.mu.Lock()
	if h.cocreating {
		h.mu.Unlock()
		return errors.New(contentlang.Pick("阶段共创进行中，请先结束共创", "Đang trong đồng sáng tạo theo giai đoạn, hãy kết thúc đồng sáng tạo trước"))
	}
	running := h.lifecycle == lifecycleRunning
	h.mu.Unlock()

	h.emitEvent(Event{Time: time.Now(), Category: "USER", Summary: "[继续] " + text, Level: "info"})

	if running {
		h.coordinator.FollowUp(interventionMsg(text))
		return nil
	}
	// Sau khi dừng máy → inject và tự động khôi phục (run khôi phục cũng chịu ràng buộc tiền điều kiện budget)
	if err := h.budget.Refuse(); err != nil {
		return err
	}
	h.refreshWriterRestore()
	h.observer.setAborting(false)
	_, err := h.coordinator.Inject(interventionMsg(text))
	if err != nil {
		return fmt.Errorf("inject: %w", err)
	}
	h.mu.Lock()
	h.lifecycle = lifecycleRunning
	h.mu.Unlock()
	go h.waitDone()
	return nil
}

// Steer gửi can thiệp của người dùng.
func (h *Host) Steer(text string) {
	h.mu.Lock()
	running := h.lifecycle == lifecycleRunning
	h.mu.Unlock()

	h.emitEvent(Event{Time: time.Now(), Category: "USER", Summary: "[用户干预] " + text, Level: "info"})

	msg := interventionMsg(text)
	if running {
		if _, err := h.coordinator.Inject(msg); err != nil {
			slog.Error(i18n.T("log.host.steer_inject_failed"), "module", "host", "err", err)
		}
		return
	}
	// Đã dừng máy: persist chờ lần khởi động sau + phản hồi trạng thái hệ thống ("đã lưu" là nhắc nhở hệ thống ngoài sự kiện USER)
	_ = h.store.RunMeta.SetPendingSteer(text)
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: contentlang.Pick("干预已保存，下次启动时生效", "Đã lưu can thiệp, có hiệu lực ở lần khởi động sau"), Level: "info"})
}

// Abort tạm dừng coordinator hiện tại.
func (h *Host) Abort() bool {
	return h.abortWithEvent(contentlang.Pick("用户手动暂停当前创作", "Người dùng tạm dừng thủ công phiên sáng tác hiện tại"), "warn")
}

// abortWithEvent thực thi tạm dừng với sự kiện lý do chỉ định. Dừng máy do budget và tạm dừng thủ công dùng chung một cơ chế dừng,
// chỉ khác nội dung sự kiện (dừng do budget = chỉ thị Abort người dùng đã ký trước, ngữ nghĩa tương đương tạm dừng thủ công).
func (h *Host) abortWithEvent(summary, level string) bool {
	h.mu.Lock()
	running := h.lifecycle == lifecycleRunning
	if running {
		h.lifecycle = lifecyclePaused
	}
	h.mu.Unlock()
	if !running {
		return false
	}
	// Bật cờ phải trước coordinator.Abort: lan truyền cancel sẽ lập tức gây ra sự kiện thất bại stream init / subagent,
	// observer dựa vào cờ này nhận diện là nhiễu phát sinh từ abort và ức chế.
	h.observer.setAborting(true)
	h.coordinator.Abort()
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: level})
	return true
}

// Close chấm dứt coordinator và đóng kênh sự kiện.
//
// Ngữ nghĩa persist Usage: trước hết hủy autoSaveLoop (nó tự flush trạng thái dirty lần cuối),
// rồi bù một lần SaveNow đồng bộ để kết thúc. Lỗ hổng đã biết: sau AbortSilent nếu vẫn còn lời gọi LLM
// in-flight quay về, OnMessage → Record được kích hoạt sẽ cập nhật bộ nhớ nhưng **không được persist**. Phần
// "vài trăm token cuối cùng" bị mất này ở lần khởi động sau sẽ được session jsonl replay tự bù lại.
func (h *Host) Close() {
	h.observer.setAborting(true)
	h.coordinator.AbortSilent()
	if h.routerDetach != nil {
		h.routerDetach()
		h.routerDetach = nil
	}
	if h.budgetDetach != nil {
		h.budgetDetach()
		h.budgetDetach = nil
	}
	if h.usageCancel != nil {
		h.usageCancel()
		h.usageCancel = nil
	}
	if err := h.usage.SaveNow(); err != nil {
		slog.Warn(i18n.T("log.usage.flush_before_exit_failed"), "module", "usage", "err", err)
	}
	h.closeOnce.Do(func() {
		close(h.done)
		close(h.events)
		close(h.streamCh)
	})
}

// waitDone chờ coordinator dừng máy và phát sự kiện trạng thái cuối.
//
// Không tự chạy tiếp gì cả. Run kết thúc = Host vào trạng thái cuối:
//   - Phase=Complete  → đánh dấu completed, phát sự kiện "sáng tác hoàn thành"
//   - khác            → đánh dấu idle, phát sự kiện "Coordinator dừng"
//
// Để tiếp tục sáng tác người dùng chỉ có hai đường: Continue thủ công (inject sau khi dừng máy) hoặc khởi động lại tiến trình đi qua Resume.
// Xem docs/architecture.md §13.3, §8.3.
func (h *Host) waitDone() {
	h.coordinator.WaitForIdle()
	h.observer.finalize()

	h.mu.Lock()
	progress, _ := h.store.Progress.Load()
	if progress != nil && progress.Phase == domain.PhaseComplete {
		h.lifecycle = lifecycleCompleted
		summary := i18n.Tf("notify.run_end.done_summary", len(progress.CompletedChapters), progress.TotalWordCount)
		h.mu.Unlock()
		slog.Info(summary, "module", "host")
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: "success"})
		h.notifier.Send(notify.Notification{
			Kind: "run_end", Level: "info", Title: i18n.T("notify.title.run_done"),
			Body: h.runEndBody(progress.NovelName, summary),
		})
	} else {
		wasRunning := h.lifecycle == lifecycleRunning
		if wasRunning {
			h.lifecycle = lifecycleIdle
		}
		completed := 0
		name := ""
		if progress != nil {
			completed = len(progress.CompletedChapters)
			name = progress.NovelName
		}
		h.mu.Unlock()
		if wasRunning {
			summary := i18n.Tf("notify.run_end.stopped_summary", completed)
			slog.Warn(summary, "module", "host")
			h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: summary, Level: "warn"})
			h.notifier.Send(notify.Notification{
				Kind: "run_end", Level: "warn", Title: i18n.T("notify.title.run_stopped"),
				Body: h.runEndBody(name, summary),
			})
		}
	}

	select {
	case h.done <- struct{}{}:
	default:
	}
}

// runEndBody ráp nội dung thông báo run_end: tên sách + tóm tắt tiến độ + chi phí tích lũy.
func (h *Host) runEndBody(novelName, summary string) string {
	if name := strings.TrimSpace(novelName); name != "" {
		summary = i18n.Tf("notify.run_end.novel_prefix", name) + summary
	}
	cost, _, _, _, _ := h.usage.Totals()
	if cost > 0 {
		summary += i18n.Tf("notify.run_end.cost_suffix", cost)
	}
	return summary
}

// ── kênh ──

// StreamClearSentinel gửi đơn lẻ qua streamCh để báo hiệu "xóa stream round hiện tại".
// Không dùng clearCh riêng nữa — hai kênh không có thứ tự khiến ✻ header thường rơi vào cuối round trước.
const StreamClearSentinel = "\x00\x00CLEAR\x00\x00"

func (h *Host) Events() <-chan Event        { return h.events }
func (h *Host) Stream() <-chan string       { return h.streamCh }
func (h *Host) Done() <-chan struct{}       { return h.done }
func (h *Host) Dir() string                 { return h.store.Dir() }
func (h *Host) AskUser() *tools.AskUserTool { return h.askUser }

// ── phát sự kiện ──

func (h *Host) emitEvent(ev Event) {
	defer func() { recover() }()
	// Lối vào slog duy nhất của mọi sự kiện. Các sự kiện agentcore do observer dịch và các sự kiện
	// SYSTEM tự phát của Host (Start/Abort/Resume…) đều ghi log ở đây, tránh việc ESC abort và việc
	// terminate từ bên ngoài không phân biệt được trên tui.log.
	if ev.Summary != "" || ev.Detail != "" {
		level := slog.LevelInfo
		switch ev.Level {
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
		// Log ghi đầy đủ Detail (để rà soát, không cắt bớt); chỉ khi Detail rỗng mới quay về Summary.
		msg := ev.Detail
		if msg == "" {
			msg = ev.Summary
		}
		attrs := []any{"module", "event", "category", ev.Category, "agent", ev.Agent}
		if ev.Kind != "" {
			attrs = append(attrs, "kind", ev.Kind)
		}
		slog.Log(context.Background(), level, msg, attrs...)
	}
	select {
	case h.events <- ev:
	default:
		select {
		case <-h.events:
		default:
		}
		select {
		case h.events <- ev:
		default:
		}
	}
}

func (h *Host) emitDelta(delta string) {
	defer func() { recover() }()
	select {
	case h.streamCh <- delta:
	default:
		select {
		case <-h.streamCh:
		default:
		}
		select {
		case h.streamCh <- delta:
		default:
		}
	}
}

func (h *Host) emitClear() {
	// Đi qua streamCh dưới dạng "sentinel", bảo đảm cùng kênh với emitDelta để tới TUI theo thứ tự.
	h.emitDelta(StreamClearSentinel)
}

// ── Snapshot (tổng hợp trạng thái TUI) ──

func (h *Host) Snapshot() UISnapshot {
	h.mu.Lock()
	state := h.lifecycle
	provider, model, _ := h.models.CurrentSelection("default")
	h.mu.Unlock()

	// Phân giải động context window của model hiện tại, sau khi /model chuyển đổi thì Snapshot lần sau tự phản ánh
	modelWindow, _ := h.cfg.ResolveContextWindow(model)
	cost, tokIn, tokOut, cacheRead, cacheWrite := h.usage.Totals()
	saved := h.usage.SavedUSD()
	overallCapable := h.usage.OverallCacheCapable()
	recentRead, recentInput, recentSamples := h.usage.OverallRecent()
	perAgent := h.usage.PerAgent()
	cacheStats := make([]AgentCacheStat, 0, len(perAgent))
	for _, a := range perAgent {
		cacheStats = append(cacheStats, AgentCacheStat{
			Role:            a.Role,
			Input:           a.Input,
			Output:          a.Output,
			CacheRead:       a.CacheRead,
			CacheWrite:      a.CacheWrite,
			Cost:            a.Cost,
			Saved:           a.Saved,
			CacheCapable:    a.CacheCapable,
			RecentCacheRead: a.RecentCacheRead,
			RecentInput:     a.RecentInput,
			RecentSamples:   a.RecentSamples,
		})
	}
	perModel := h.usage.PerModel()
	modelStats := make([]AgentCacheStat, 0, len(perModel))
	for _, a := range perModel {
		modelStats = append(modelStats, AgentCacheStat{
			Model:        a.Model,
			Input:        a.Input,
			Output:       a.Output,
			CacheRead:    a.CacheRead,
			CacheWrite:   a.CacheWrite,
			Cost:         a.Cost,
			Saved:        a.Saved,
			CacheCapable: a.CacheCapable,
		})
	}

	snap := UISnapshot{
		Provider:               provider,
		ModelName:              model,
		ModelContextWindow:     modelWindow,
		Style:                  h.cfg.Style,
		RuntimeState:           string(state),
		IsRunning:              state == lifecycleRunning,
		TotalInputTokens:       tokIn,
		TotalOutputTokens:      tokOut,
		TotalCacheReadTokens:   cacheRead,
		TotalCacheWriteTokens:  cacheWrite,
		TotalCostUSD:           cost,
		TotalSavedUSD:          saved,
		BudgetLimitUSD:         h.budget.Limit(),
		OverallCacheCapable:    overallCapable,
		OverallRecentCacheRead: recentRead,
		OverallRecentInput:     recentInput,
		OverallRecentSamples:   recentSamples,
		CachePerAgent:          cacheStats,
		CachePerModel:          modelStats,
		MissingAssistantUsage:  h.usage.MissingAssistantUsage(),
	}

	progress, _ := h.store.Progress.Load()
	if progress != nil {
		snap.NovelName = strings.TrimSpace(progress.NovelName)
		snap.Phase = string(progress.Phase)
		snap.Flow = string(progress.Flow)
		snap.CurrentChapter = progress.CurrentChapter
		snap.TotalChapters = progress.TotalChapters
		snap.CompletedCount = len(progress.CompletedChapters)
		snap.TotalWordCount = progress.TotalWordCount
		snap.InProgressChapter = progress.InProgressChapter
		snap.PendingRewrites = progress.PendingRewrites
		snap.RewriteReason = progress.RewriteReason
		snap.Layered = progress.Layered
		if progress.CurrentVolume > 0 {
			snap.CurrentVolumeArc = fmt.Sprintf(contentlang.Pick("第%d卷·第%d弧", "Quyển %d·Cung %d"), progress.CurrentVolume, progress.CurrentArc)
		}
	}
	if snap.NovelName == "" {
		if premise, _ := h.store.Outline.LoadPremise(); premise != "" {
			snap.NovelName = domain.ExtractNovelNameFromPremise(premise)
		}
	}
	if meta, _ := h.store.RunMeta.Load(); meta != nil {
		snap.PendingSteer = meta.PendingSteer
	}

	snap.Agents = h.observer.agentSnapshots()
	h.fillContextStatus(&snap)
	snap.StatusLabel = deriveStatusLabel(snap)

	// Nhãn khôi phục
	if _, label, err := buildResumePrompt(h.store); err == nil && label != "" {
		snap.RecoveryLabel = label
	}

	h.fillDetails(&snap, progress)

	return snap
}

// fillContextStatus điền thông tin sức khỏe context của Coordinator.
func (h *Host) fillContextStatus(snap *UISnapshot) {
	if h.coordinator == nil {
		return
	}
	if usage := h.coordinator.BaselineContextUsage(); usage != nil {
		snap.ContextTokens = usage.Tokens
		snap.ContextWindow = usage.ContextWindow
		snap.ContextPercent = usage.Percent
	}
	if ctx := h.coordinator.ContextSnapshot(); ctx != nil {
		snap.ContextScope = ctx.Scope
		snap.ContextStrategy = ctx.LastStrategy
		snap.ContextActiveMessages = ctx.ActiveMessages
		snap.ContextSummaryCount = ctx.SummaryMessages
		snap.ContextCompactedCount = ctx.LastCompactedCount
		snap.ContextKeptCount = ctx.LastKeptCount
		if snap.ContextTokens == 0 {
			if ctx.BaselineUsage != nil {
				snap.ContextTokens = ctx.BaselineUsage.Tokens
				snap.ContextWindow = ctx.BaselineUsage.ContextWindow
				snap.ContextPercent = ctx.BaselineUsage.Percent
			} else if ctx.Usage != nil {
				snap.ContextTokens = ctx.Usage.Tokens
				snap.ContextWindow = ctx.Usage.ContextWindow
				snap.ContextPercent = ctx.Usage.Percent
			}
		}
	}
}

// fillDetails điền vùng chi tiết: thiết lập, nhân vật, commit/review/tóm tắt gần đây.
func (h *Host) fillDetails(snap *UISnapshot, progress *domain.Progress) {
	if premise, _ := h.store.Outline.LoadPremise(); premise != "" {
		snap.Premise = truncate(premise, 80)
	}
	if outline, _ := h.store.Outline.LoadOutline(); len(outline) > 0 {
		for _, e := range outline {
			snap.Outline = append(snap.Outline, OutlineSnapshot{
				Chapter: e.Chapter, Title: e.Title, CoreEvent: e.CoreEvent,
			})
		}
	}
	if progress != nil && progress.Layered {
		if compass, _ := h.store.Outline.LoadCompass(); compass != nil {
			snap.CompassDirection = compass.EndingDirection
			snap.CompassScale = compass.EstimatedScale
		}
		if volumes, _ := h.store.Outline.LoadLayeredOutline(); len(volumes) > 0 {
			for _, v := range volumes {
				if v.Index > progress.CurrentVolume {
					snap.NextVolumeTitle = v.Title
					break
				}
			}
		}
	}
	if chars, _ := h.store.Characters.Load(); len(chars) > 0 {
		for _, c := range chars {
			label := c.Name
			if c.Role != "" {
				label += "（" + c.Role + "）"
			}
			snap.Characters = append(snap.Characters, label)
		}
	}
	if ledger, _ := h.store.Cast.Load(); len(ledger) > 0 {
		snap.SupportingCount = len(ledger)
		recent, _ := h.store.Cast.RecentActive(5)
		for _, e := range recent {
			label := e.Name
			if e.BriefRole != "" {
				label += "（" + e.BriefRole + "）"
			}
			snap.RecentSupporting = append(snap.RecentSupporting, label)
		}
	}
	if progress != nil && len(progress.CompletedChapters) > 0 {
		lastCh := progress.CompletedChapters[len(progress.CompletedChapters)-1]
		wc := progress.ChapterWordCounts[lastCh]
		snap.LastCommitSummary = fmt.Sprintf(contentlang.Pick("第%d章 %d字", "Chương %d %d chữ"), lastCh, wc)
	}
	currentCh := 1
	if progress != nil && len(progress.CompletedChapters) > 0 {
		currentCh = progress.CompletedChapters[len(progress.CompletedChapters)-1]
	}
	if review, err := h.store.World.LoadLastReview(currentCh); err == nil && review != nil {
		snap.LastReviewSummary = fmt.Sprintf(contentlang.Pick("verdict=%s %d个问题", "verdict=%s %d vấn đề"), review.Verdict, len(review.Issues))
		if len(review.AffectedChapters) > 0 {
			snap.LastReviewSummary += fmt.Sprintf(contentlang.Pick(" 影响%v", " ảnh hưởng %v"), review.AffectedChapters)
		}
	}
	if cp := h.store.Checkpoints.LatestGlobal(); cp != nil {
		snap.LastCheckpointName = fmt.Sprintf("%s.%s", cp.Scope, cp.Step)
	}
	if progress != nil {
		for i := len(progress.CompletedChapters) - 1; i >= 0 && len(snap.RecentSummaries) < 2; i-- {
			ch := progress.CompletedChapters[i]
			if summary, err := h.store.Summaries.LoadSummary(ch); err == nil && summary != nil {
				snap.RecentSummaries = append(snap.RecentSummaries,
					fmt.Sprintf(contentlang.Pick("第%d章: %s", "Chương %d: %s"), ch, truncate(summary.Summary, 50)))
			}
		}
	}
}

func deriveStatusLabel(s UISnapshot) string {
	switch {
	case s.Phase == string(domain.PhaseComplete):
		return "COMPLETE"
	case s.Flow == string(domain.FlowReviewing):
		return "REVIEW"
	case s.Flow == string(domain.FlowRewriting) || s.Flow == string(domain.FlowPolishing):
		return "REWRITE"
	case s.RuntimeState == "running":
		return "RUNNING"
	default:
		return "READY"
	}
}

// ── quản lý model ──

func (h *Host) ConfiguredProviders() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	providers := make([]string, 0, len(h.cfg.Providers))
	for name := range h.cfg.Providers {
		providers = append(providers, name)
	}
	sort.Strings(providers)
	return providers
}

func (h *Host) ConfiguredModels(provider string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.CandidateModels(provider)
}

func (h *Host) CurrentModelSelection(role string) (string, string, bool) {
	return h.models.CurrentSelection(role)
}

func (h *Host) SwitchModel(role, provider, model string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if provider == "" || model == "" {
		return fmt.Errorf("provider and model are required")
	}
	if err := h.models.Swap(role, provider, model); err != nil {
		return err
	}
	if role == "" || role == "default" {
		h.cfg.Provider = provider
		h.cfg.ModelName = model
	} else {
		if h.cfg.Roles == nil {
			h.cfg.Roles = make(map[string]bootstrap.RoleConfig)
		}
		rc := h.cfg.Roles[role]
		rc.Provider = provider
		rc.Model = model
		h.cfg.Roles[role] = rc
	}
	h.normalizeThinkingLocked(role)
	if path := bootstrap.DefaultConfigPath(); path != "" {
		if err := bootstrap.SaveConfig(path, h.cfg); err != nil {
			slog.Warn(i18n.T("log.host.save_config_failed"), "module", "host", "err", err)
		}
	}
	h.applyThinkingLocked(role)
	// Khi chuyển sang model chưa đăng ký thì in một dòng warn, nhắc người dùng đã đi vào lưới đỡ 128k — truyện dài dễ bị nén sớm.
	logRole := role
	if logRole == "" {
		logRole = "default"
	}
	window, source := h.cfg.ResolveContextWindow(model)
	bootstrap.LogContextWindowChoice(logRole, model, window, source)

	// Khi chuyển sang default/coordinator thì liên động window và reserve của coordinator engine.
	// writer/architect/editor đi qua ContextManagerFactory tự dựng lại theo model mới, không cần liên động.
	// Không liên động sẽ dẫn tới: khi chuyển 1M→128k coordinator engine vẫn tính threshold theo 1M,
	// messages tích lũy vượt 128k là API báo lỗi; khi 128k→1M ngưỡng bị ghim ở 96k, lãng phí context dài.
	//
	// Then chốt: phải dùng models.CurrentSelection("coordinator") để lấy model "coordinator thực sự dùng"
	// mà tính window — chứ không dùng thẳng model đích chuyển. Khi người dùng cấu hình roles.coordinator model riêng,
	// chuyển default không ảnh hưởng model thực của coordinator; dùng window của model đích để SetContextWindow sẽ
	// chỉnh nhầm ngưỡng coordinator thành giá trị không liên quan (ví dụ: khi default chuyển model 1M thì kéo ngưỡng
	// coordinator engine 200k lên 891k, viết quá 200k là nổ API ngay).
	if h.coordinatorCtxMgr != nil && (role == "" || role == "default" || role == "coordinator") {
		_, coordinatorModel, _ := h.models.CurrentSelection("coordinator")
		coordinatorWindow, coordSource := h.cfg.ResolveContextWindow(coordinatorModel)
		h.coordinator.SetContextWindow(coordinatorWindow)
		h.coordinatorCtxMgr.SetContextWindow(coordinatorWindow)
		h.coordinatorCtxMgr.SetReserveTokens(bootstrap.CompactReserveTokens(coordinatorWindow))
		// Khi model thực của coordinator khác model đích chuyển (người dùng chuyển default nhưng coordinator có role riêng),
		// LogContextWindowChoice ở trên in window của default, không khớp giá trị thực tế có hiệu lực; bù thêm một dòng.
		if coordinatorModel != model {
			bootstrap.LogContextWindowChoice("coordinator", coordinatorModel, coordinatorWindow, coordSource)
		}
	}

	h.emitEvent(Event{
		Time:     time.Now(),
		Category: "SYSTEM",
		Summary:  fmt.Sprintf(contentlang.Pick("模型已切换：%s → %s/%s", "Đã chuyển model: %s → %s/%s"), role, provider, model),
		Level:    "info",
	})
	return nil
}

// concreteThinkingRoles là các role cụ thể có thể áp cường độ thinking (nhất quán với route của agents.ApplyThinking).
// Khi gọi default thì áp lại từng role theo ResolveThinking.
var concreteThinkingRoles = []string{"coordinator", "architect", "writer", "editor"}

// CurrentThinking trả về chuỗi gốc cường độ thinking đang có hiệu lực của một role (để panel /model đồng bộ giá trị hiện tại).
func (h *Host) CurrentThinking(role string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.ResolveThinking(strings.ToLower(strings.TrimSpace(role)))
}

func (h *Host) AvailableThinking(role string) []agentcore.ThinkingLevel {
	h.mu.Lock()
	model := h.models.ForRole(strings.ToLower(strings.TrimSpace(role)))
	h.mu.Unlock()
	return agents.AvailableThinkingForModel(model)
}

func (h *Host) normalizeThinkingLocked(role string) agentcore.ThinkingLevel {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" || role == "default" {
		parsed, _ := agents.ParseThinkingLevel(h.cfg.Thinking)
		for _, r := range concreteThinkingRoles {
			resolved, ok := agents.ResolveThinkingForModel(h.models.ForRole(r), parsed)
			if !ok || resolved != parsed {
				h.cfg.Thinking = string(resolved)
				return resolved
			}
		}
		h.cfg.Thinking = string(parsed)
		return parsed
	}

	_, hasRoleThinking := h.cfg.Roles[role]
	hasRoleThinking = hasRoleThinking && h.cfg.Roles[role].Thinking != ""
	parsed, _ := agents.ParseThinkingLevel(h.cfg.ResolveThinking(role))
	resolved, _ := agents.ResolveThinkingForModel(h.models.ForRole(role), parsed)
	if !hasRoleThinking {
		if resolved != parsed {
			h.cfg.Thinking = string(resolved)
		}
		return resolved
	}
	if h.cfg.Roles == nil {
		h.cfg.Roles = make(map[string]bootstrap.RoleConfig)
	}
	rc := h.cfg.Roles[role]
	rc.Thinking = string(resolved)
	h.cfg.Roles[role] = rc
	return resolved
}

func (h *Host) applyThinkingLocked(role string) {
	if h.thinkingApplier == nil {
		return
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" || role == "default" {
		for _, r := range concreteThinkingRoles {
			lv, _ := agents.ParseThinkingLevel(h.cfg.ResolveThinking(r))
			h.thinkingApplier(r, lv)
		}
		return
	}
	lv, _ := agents.ParseThinkingLevel(h.cfg.ResolveThinking(role))
	h.thinkingApplier(role, lv)
}

// SetRoleThinking đặt cường độ thinking của một role (hoặc default): kiểm tra→persist→liên động live agent→sự kiện.
// Phản chiếu cấu trúc của SwitchModel; trực giao với việc chọn model, có thể chỉnh riêng. level rỗng = không ghi đè (kế thừa).
func (h *Host) SetRoleThinking(role, level string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	parsed, err := agents.ParseThinkingLevel(level)
	if err != nil {
		return err
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" || role == "default" {
		for _, r := range concreteThinkingRoles {
			if resolved, ok := agents.ResolveThinkingForModel(h.models.ForRole(r), parsed); !ok || resolved != parsed {
				parsed = resolved
				break
			}
		}
	} else {
		parsed, _ = agents.ResolveThinkingForModel(h.models.ForRole(role), parsed)
	}
	// Persist: role cụ thể ghi vào Roles[role].Thinking, default/"" ghi vào Thinking cấp đỉnh.
	if role == "" || role == "default" {
		h.cfg.Thinking = string(parsed)
	} else {
		if h.cfg.Roles == nil {
			h.cfg.Roles = make(map[string]bootstrap.RoleConfig)
		}
		rc := h.cfg.Roles[role]
		rc.Thinking = string(parsed)
		h.cfg.Roles[role] = rc
	}
	if path := bootstrap.DefaultConfigPath(); path != "" {
		if err := bootstrap.SaveConfig(path, h.cfg); err != nil {
			slog.Warn(i18n.T("log.host.save_config_failed"), "module", "host", "err", err)
		}
	}

	// Liên động live: role cụ thể áp trực tiếp; default thì duyệt từng role cụ thể áp lại theo ResolveThinking
	// (cái đã bị ghi đè ở mức role thì giữ riêng, cái chưa ghi đè thì nhận default mới).
	h.applyThinkingLocked(role)

	logRole := role
	if logRole == "" {
		logRole = "default"
	}
	shown := string(parsed)
	if shown == "" {
		shown = contentlang.Pick("默认(继承)", "mặc định (kế thừa)")
	}
	h.emitEvent(Event{
		Time:     time.Now(),
		Category: "SYSTEM",
		Summary:  fmt.Sprintf(contentlang.Pick("思考强度已切换：%s → %s", "Đã chuyển cường độ suy nghĩ: %s → %s"), logRole, shown),
		Level:    "info",
	})
	return nil
}

// ── replay sự kiện ──

func (h *Host) ReplayQueue(afterSeq int64) ([]domain.RuntimeQueueItem, error) {
	if h.store == nil || h.store.Runtime == nil {
		return nil, nil
	}
	return h.store.Runtime.LoadQueueAfter(afterSeq)
}

// ── đồng sáng tạo ──

// CoCreateStream đồng sáng tạo khởi động lạnh: làm rõ nhu cầu từ con số không, sinh ra chỉ thị sáng tác cho cả cuốn sách.
func (h *Host) CoCreateStream(ctx context.Context, history []CoCreateMessage, onProgress func(kind, text string)) (CoCreateReply, error) {
	return coCreateStream(ctx, h.models, h.store.Sessions, coCreateSystemPrompt(), history, onProgress)
}

// StageCoCreateStream đồng sáng tạo theo giai đoạn: lập kế hoạch hướng đi tiếp theo dựa trên nội dung đã viết.
// System prompt = prompt giai đoạn + tóm tắt trạng thái truyện hiện tại, cho trợ lý biết "đã viết những gì".
func (h *Host) StageCoCreateStream(ctx context.Context, history []CoCreateMessage, onProgress func(kind, text string)) (CoCreateReply, error) {
	return coCreateStream(ctx, h.models, h.store.Sessions, stageSystemPrompt(h.store), history, onProgress)
}

// stagePlanPrefix gói "brief hướng đi tiếp theo" do đồng sáng tạo sinh ra thành một can thiệp lập kế hoạch giai đoạn, giao Coordinator phân xử.
// Chỉ dán nhãn sự thật [阶段规划] + phát biểu trung tính, không viết cứng "cách triển khai" — route cụ thể (compass / architect /
// save_directive) giao cho phán định "阶段规划" của coordinator.md, tránh tạo nguồn sự thật thứ hai với prompt,
// cũng không chặn các yêu cầu loại phong cách đi qua directive (giữ "phân loại phân xử thuộc về LLM"). Continue lại chồng thêm prefix [用户干预].
func stagePlanPrefix() string {
	return contentlang.Pick(
		"[阶段规划] 我暂停创作，和共创助手一起梳理了下面的后续方向，请按你的干预分类裁定如何落地，然后继续创作。后续方向如下：\n\n",
		"[阶段规划] Tôi tạm dừng sáng tác, đã cùng trợ lý đồng sáng tạo sắp xếp các hướng đi tiếp theo dưới đây, hãy theo phân loại can thiệp của bạn phán định cách triển khai, rồi tiếp tục sáng tác. Các hướng đi tiếp theo như sau:\n\n",
	)
}

// PauseForCoCreate vào đồng sáng tạo theo giai đoạn: đặt cờ chiếm dụng đồng sáng tạo, nếu đang chạy thì tạm dừng coordinator luôn.
// Trả về false nghĩa là không thể vào (cả sách đã hoàn thành hoặc đang trong đồng sáng tạo), bên gọi cứ bỏ qua.
// Cờ chiếm dụng chặn can thiệp đồng thời của import/simulate/start/resume/continue trong cửa sổ đồng sáng tạo —
// sau khi đang chạy bị tạm dừng thì lifecycle=paused, mutex ==running hiện có thất hiệu, dựa cờ này lấp lỗ;
// đã dừng (idle/paused) cũng cho phép vào, lập kế hoạch xong đi qua Continue để chạy tiếp.
func (h *Host) PauseForCoCreate() bool {
	h.mu.Lock()
	if h.cocreating || h.lifecycle == lifecycleCompleted {
		h.mu.Unlock()
		return false
	}
	h.cocreating = true
	running := h.lifecycle == lifecycleRunning
	h.mu.Unlock()

	// Khi đang chạy thì tái dùng abortWithEvent để dừng máy (running→paused + setAborting + Abort + sự kiện), cùng thứ tự
	// với tạm dừng thủ công, không chép lại lần nữa; đã dừng (idle/paused) chỉ đặt cờ, lập kế hoạch xong đi qua Continue chạy tiếp.
	if running {
		h.abortWithEvent(contentlang.Pick("进入阶段共创，创作已暂停", "Vào đồng sáng tạo theo giai đoạn, đã tạm dừng sáng tác"), "info")
	} else {
		h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: contentlang.Pick("进入阶段共创", "Vào đồng sáng tạo theo giai đoạn"), Level: "info"})
	}
	return true
}

// ResumeFromCoCreate kết thúc đồng sáng tạo theo giai đoạn: inject hướng đi tiếp theo do đồng sáng tạo sinh ra như một can thiệp và khôi phục sáng tác.
// Sau khi xóa cờ chiếm dụng thì tái dùng đường inject-sau-khi-dừng-máy của Continue (chịu ràng buộc tiền điều kiện budget).
// Lưu ý: khi draft rỗng thì trả về sớm, không xóa cờ là có chủ ý (đồng sáng tạo chưa kết thúc); guard canStart() phía TUI
// dùng cùng phán định "khác rỗng" như ở đây, bảo đảm đường này bất khả đạt, cocreating không bị rò vì thế.
func (h *Host) ResumeFromCoCreate(draft string) error {
	draft = strings.TrimSpace(draft)
	if draft == "" {
		return fmt.Errorf("draft is required")
	}
	h.mu.Lock()
	if !h.cocreating {
		h.mu.Unlock()
		return fmt.Errorf("not in co-create")
	}
	h.cocreating = false
	h.mu.Unlock()

	// Abort của PauseForCoCreate là bất đồng bộ: trước khi khôi phục chờ run cũ hội tụ, quay về tiền đề "thật sự dừng máy"
	// nhất quán với Continue sau tạm dừng thủ công, tránh steer chỉ thị chạy tiếp vào run cũ đang thoát. Khi vào đồng sáng tạo
	// ở trạng thái không-chạy (chưa Abort) thì coordinator vốn đã idle, WaitForIdle trả về ngay.
	h.coordinator.WaitForIdle()

	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: contentlang.Pick("阶段共创完成，已注入后续方向并恢复创作", "Đồng sáng tạo theo giai đoạn hoàn tất, đã chèn hướng đi tiếp theo và khôi phục sáng tác"), Level: "info"})
	return h.Continue(stagePlanPrefix() + draft)
}

// CancelCoCreate bỏ đồng sáng tạo theo giai đoạn: xóa cờ chiếm dụng, giữ trạng thái tạm dừng (người dùng có thể tiếp tục ở ô nhập hoặc khởi động lại Resume).
func (h *Host) CancelCoCreate() {
	h.mu.Lock()
	if !h.cocreating {
		h.mu.Unlock()
		return
	}
	h.cocreating = false
	h.mu.Unlock()
	h.emitEvent(Event{Time: time.Now(), Category: "SYSTEM", Summary: contentlang.Pick("已退出阶段共创，创作保持暂停（可在输入框继续）", "Đã thoát đồng sáng tạo theo giai đoạn, sáng tác vẫn tạm dừng (có thể tiếp tục ở ô nhập)"), Level: "info"})
}

// ── tiện ích ──

func (h *Host) refreshWriterRestore() {
	if h.writerRestore != nil {
		h.writerRestore.Refresh(h.store)
	}
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// ImportFrom khởi động một lần nhập đảo ngược tiểu thuyết bên ngoài: cắt phân đoạn → suy ngược foundation → phân tích từng chương rồi ghi xuống đĩa.
// Loại trừ lẫn nhau với Coordinator; nhập xong bên gọi có thể Resume() viết tiếp ngay.
// Kênh sự kiện trả về do imp.Run đóng, bên gọi chịu trách nhiệm tiêu thụ (đầy thì loại bỏ để tránh chặn goroutine phân tích).
func (h *Host) ImportFrom(ctx context.Context, opts imp.Options) (<-chan imp.Event, error) {
	if err := h.guardExclusive(contentlang.Pick("导入", "Nhập")); err != nil {
		return nil, err
	}

	rulesOpts := rules.DefaultOptions(h.bundle.RulesFS)
	deps := imp.Deps{
		Store:      h.store,
		CommitTool: tools.NewCommitChapterTool(h.store).WithRules(rulesOpts).WithOutputLang(h.cfg.ResolveOutputLang()),
		LLM:        h.models.ForRole("architect"),
		Prompts: imp.Prompts{
			Foundation: h.bundle.Prompts.ImportFoundation,
			Analyzer:   h.bundle.Prompts.ImportAnalyzer,
		},
	}
	return imp.Run(ctx, deps, opts)
}

// Simulate đọc thư mục simulate và sinh hoặc cập nhật tăng tiến hồ sơ mô phỏng.
func (h *Host) Simulate(ctx context.Context) (<-chan sim.Event, error) {
	if err := h.guardExclusive(contentlang.Pick("生成仿写画像", "Tạo hồ sơ mô phỏng")); err != nil {
		return nil, err
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working dir: %w", err)
	}
	deps := sim.Deps{
		Store: h.store,
		LLM:   h.models.ForRole("architect"),
		Prompts: sim.Prompts{
			Source: h.bundle.Prompts.SimulationSource,
			Merge:  h.bundle.Prompts.SimulationMerge,
		},
	}
	return sim.Run(ctx, deps, sim.Options{SourceDir: filepath.Join(wd, "simulate")})
}

// ImportSimulationProfile nhập hồ sơ mô phỏng đã sinh trước đó.
func (h *Host) ImportSimulationProfile(ctx context.Context, path string) (<-chan sim.Event, error) {
	if err := h.guardExclusive(contentlang.Pick("导入仿写画像", "Nhập hồ sơ mô phỏng")); err != nil {
		return nil, err
	}
	return sim.RunImport(ctx, h.store, path)
}

// guardExclusive kiểm tra chiếm dụng độc quyền: khi coordinator đang chạy hoặc trong cửa sổ đồng sáng tạo thì từ chối các lối vào sẽ ghi đè trạng thái
// (import/simulate). Lấp lỗ hổng đồng thời của giai đoạn paused vốn chỉ kiểm ==running.
func (h *Host) guardExclusive(action string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.lifecycle == lifecycleRunning:
		return fmt.Errorf(contentlang.Pick("coordinator 运行中，请先暂停后再%s", "coordinator đang chạy, hãy tạm dừng trước rồi mới %s"), action)
	case h.cocreating:
		return fmt.Errorf(contentlang.Pick("阶段共创进行中，请先结束共创后再%s", "đang trong đồng sáng tạo theo giai đoạn, hãy kết thúc đồng sáng tạo trước rồi mới %s"), action)
	}
	return nil
}

// Export xuất các chương đã hoàn thành thành file bên ngoài (hiện chỉ hỗ trợ TXT).
//
// Khác với ImportFrom: xuất là thao tác chỉ đọc (không động Progress / Checkpoint),
// nên **không yêu cầu Coordinator rảnh** — giữa chừng đang viết cũng có thể xuất "thành phẩm hiện tại" bất cứ lúc nào.
// Chỉ đọc một snapshot nhất quán của Progress.CompletedChapters + bản cuối các chương + dàn ý + premise.
func (h *Host) Export(ctx context.Context, opts exp.Options) (*exp.Result, error) {
	return exp.Run(ctx, exp.Deps{Store: h.store}, opts)
}
