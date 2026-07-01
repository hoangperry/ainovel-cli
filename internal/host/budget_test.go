package host

import (
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
)

type budgetRecorder struct {
	cost    float64
	aborts  []string
	reports []string
}

func (r *budgetRecorder) sentinel(cfg bootstrap.BudgetConfig) *BudgetSentinel {
	return NewBudgetSentinel(cfg,
		func() float64 { return r.cost },
		func(reason string) { r.aborts = append(r.aborts, reason) },
		func(level, summary string) { r.reports = append(r.reports, level+": "+summary) },
	)
}

func subagentEndEvent() agentcore.Event {
	return agentcore.Event{Type: agentcore.EventToolExecEnd, Tool: "subagent"}
}

func TestBudgetSentinelDisabled(t *testing.T) {
	r := &budgetRecorder{}
	if s := r.sentinel(bootstrap.BudgetConfig{}); s != nil {
		t.Fatal("disabled budget should return nil sentinel")
	}
	// nil-safe
	var s *BudgetSentinel
	s.OnCost(100)
	s.HandleEvent(subagentEndEvent())
	if err := s.Refuse(); err != nil {
		t.Errorf("nil sentinel Refuse should pass: %v", err)
	}
	if s.Limit() != 0 {
		t.Error("nil sentinel Limit should be 0")
	}
}

func TestBudgetSentinelWarnOnceThenBoundaryStop(t *testing.T) {
	r := &budgetRecorder{}
	s := r.sentinel(bootstrap.BudgetConfig{BookUSD: 10, WarnRatio: 0.8})

	// chưa tới mốc: không side effect
	s.OnCost(5)
	if len(r.reports) != 0 {
		t.Fatalf("below warn ratio should be silent, got %v", r.reports)
	}

	// vượt mốc cảnh báo: đúng một lần warn, callback lặp lại không phát nữa
	s.OnCost(8.5)
	s.OnCost(9)
	if len(r.reports) != 1 || !strings.HasPrefix(r.reports[0], "warn:") {
		t.Fatalf("expected exactly one warn, got %v", r.reports)
	}

	// vượt ngưỡng: vào stopPending, phát error, nhưng không dừng ngay (mặc định chờ biên)
	s.OnCost(10.5)
	if len(r.reports) != 2 || !strings.HasPrefix(r.reports[1], "error:") {
		t.Fatalf("expected error report on exceeding, got %v", r.reports)
	}
	if len(r.aborts) != 0 {
		t.Fatalf("default mode should not abort before boundary, got %v", r.aborts)
	}

	// sự kiện không phải biên thì không kích hoạt
	s.HandleEvent(agentcore.Event{Type: agentcore.EventToolExecEnd, Tool: "novel_context"})
	if len(r.aborts) != 0 {
		t.Fatal("non-subagent boundary should not trigger stop")
	}

	// biên sub-agent: đúng một lần dừng, biên lặp lại không dừng nữa
	r.cost = 10.5
	s.HandleEvent(subagentEndEvent())
	s.HandleEvent(subagentEndEvent())
	if len(r.aborts) != 1 {
		t.Fatalf("expected exactly one abort at boundary, got %v", r.aborts)
	}
}

func TestBudgetSentinelJumpStraightPastLimit(t *testing.T) {
	r := &budgetRecorder{}
	s := r.sentinel(bootstrap.BudgetConfig{BookUSD: 10, WarnRatio: 0.8})

	// một callback nhảy thẳng qua cảnh báo và trần: warn và error mỗi cái đúng một lần
	s.OnCost(12)
	if len(r.reports) != 2 {
		t.Fatalf("expected warn+error in single jump, got %v", r.reports)
	}
}

func TestBudgetSentinelHardStop(t *testing.T) {
	r := &budgetRecorder{}
	s := r.sentinel(bootstrap.BudgetConfig{BookUSD: 10, WarnRatio: 0.8, HardStop: true})

	s.OnCost(11)
	if len(r.aborts) != 1 {
		t.Fatalf("hard_stop should abort immediately, got %v", r.aborts)
	}
	// các biên về sau không dừng lặp lại nữa
	r.cost = 11
	s.HandleEvent(subagentEndEvent())
	if len(r.aborts) != 1 {
		t.Fatalf("stopped state should not abort again, got %v", r.aborts)
	}
}

func TestBudgetSentinelRefuse(t *testing.T) {
	r := &budgetRecorder{cost: 9.99}
	s := r.sentinel(bootstrap.BudgetConfig{BookUSD: 10, WarnRatio: 0.8})

	if err := s.Refuse(); err != nil {
		t.Errorf("below limit should pass: %v", err)
	}
	r.cost = 10 // đúng bằng trần → từ chối
	if err := s.Refuse(); err == nil {
		t.Error("at limit should refuse")
	} else if !strings.Contains(err.Error(), "book_usd") {
		t.Errorf("refuse error should mention how to recover, got %v", err)
	}
}

func TestBudgetSentinelZeroCostBlindWarning(t *testing.T) {
	r := &budgetRecorder{}
	s := r.sentinel(bootstrap.BudgetConfig{BookUSD: 10, WarnRatio: 0.8})

	// ghi sổ chi phí zero liên tiếp: tới lần thứ blindZeroStreak thì đúng một lần cảnh báo vùng mù, sau đó im lặng
	for range blindZeroStreak + 3 {
		s.OnCost(0)
	}
	if len(r.reports) != 1 || !strings.Contains(r.reports[0], "预算盲区") {
		t.Fatalf("expected exactly one blind warning, got %v", r.reports)
	}
	if len(r.aborts) != 0 {
		t.Fatal("blind warning must not abort")
	}

	// model tính giá bình thường không nên báo nhầm: mỗi lần ghi sổ tổng tăng dần
	r2 := &budgetRecorder{}
	s2 := r2.sentinel(bootstrap.BudgetConfig{BookUSD: 10, WarnRatio: 0.8})
	for i := range blindZeroStreak + 3 {
		s2.OnCost(0.1 * float64(i+1))
	}
	for _, rep := range r2.reports {
		if strings.Contains(rep, "盲区") {
			t.Fatalf("priced model should not trigger blind warning: %v", r2.reports)
		}
	}
}

func TestBudgetSentinelBlindWarningAfterModelSwitch(t *testing.T) {
	// giữa chừng long-run /model chuyển sang model không giá: total dừng ở giá trị lịch sử khác zero nhưng không tăng nữa, vẫn phải cảnh báo
	r := &budgetRecorder{}
	s := r.sentinel(bootstrap.BudgetConfig{BookUSD: 100, WarnRatio: 0.8})

	for i := range 5 {
		s.OnCost(1.0 * float64(i+1)) // giai đoạn tính giá: tổng tăng dần tới $5
	}
	for range blindZeroStreak {
		s.OnCost(5.0) // chuyển sang model không giá: tổng đứng yên
	}
	if len(r.reports) != 1 || !strings.Contains(r.reports[0], "盲区") {
		t.Fatalf("expected blind warning after switch to unpriced model, got %v", r.reports)
	}
}
