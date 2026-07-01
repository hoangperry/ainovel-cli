package host

import (
	"context"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/host/imp"
	"github.com/voocel/ainovel-cli/internal/store"
)

// newFlagTestHost tạo một Host tối thiểu, chỉ đủ để chạy máy trạng thái cờ cocreating và guard đồng thời.
// emitEvent dùng recover + select không chặn, chỉ cần buffer kênh events, không cần coordinator/observer.
// Nhánh trạng thái chạy của PauseForCoCreate sẽ gọi coordinator.Abort (tái dùng đường tạm dừng Esc đã được kiểm chứng),
// không nằm trong unit test này; ở đây chỉ phủ logic trạng thái không-chạy và cờ/guard không phụ thuộc coordinator.
func newFlagTestHost(lc lifecycle, cocreating bool) *Host {
	return &Host{
		lifecycle:  lc,
		cocreating: cocreating,
		events:     make(chan Event, 16),
	}
}

func TestPauseForCoCreate_NonRunningSetsFlag(t *testing.T) {
	h := newFlagTestHost(lifecycleIdle, false)
	if !h.PauseForCoCreate() {
		t.Fatal("idle 态应允许进入阶段共创")
	}
	if !h.cocreating {
		t.Error("进入后 cocreating 应为 true")
	}
	if h.lifecycle != lifecycleIdle {
		t.Errorf("非运行态进入不应改 lifecycle，得 %s", h.lifecycle)
	}
}

func TestPauseForCoCreate_RejectsCompleted(t *testing.T) {
	h := newFlagTestHost(lifecycleCompleted, false)
	if h.PauseForCoCreate() {
		t.Error("全书完成后不应允许进入阶段共创")
	}
	if h.cocreating {
		t.Error("拒绝后不应置位 cocreating")
	}
}

func TestPauseForCoCreate_RejectsReentrant(t *testing.T) {
	h := newFlagTestHost(lifecyclePaused, true)
	if h.PauseForCoCreate() {
		t.Error("已在共创中应拒绝重入")
	}
}

func TestCancelCoCreate_ClearsFlag(t *testing.T) {
	h := newFlagTestHost(lifecyclePaused, true)
	h.CancelCoCreate()
	if h.cocreating {
		t.Error("取消后 cocreating 应清空")
	}
	if h.lifecycle != lifecyclePaused {
		t.Errorf("取消不应改 lifecycle，得 %s", h.lifecycle)
	}
}

func TestCancelCoCreate_NoopWhenNotCocreating(t *testing.T) {
	h := newFlagTestHost(lifecycleRunning, false)
	h.CancelCoCreate() // không nên panic, không nên đổi trạng thái
	if h.cocreating || h.lifecycle != lifecycleRunning {
		t.Error("非共创态 CancelCoCreate 应为 no-op")
	}
}

func TestResumeFromCoCreate_RejectsEmptyDraft(t *testing.T) {
	h := newFlagTestHost(lifecyclePaused, true)
	if err := h.ResumeFromCoCreate("   "); err == nil {
		t.Fatal("空 draft 应报错")
	}
	if !h.cocreating {
		t.Error("空 draft 在清标记前返回，cocreating 应保持 true")
	}
}

func TestResumeFromCoCreate_RejectsWhenNotCocreating(t *testing.T) {
	h := newFlagTestHost(lifecyclePaused, false)
	err := h.ResumeFromCoCreate("## 后续走向\n- 进入第二卷")
	if err == nil || !strings.Contains(err.Error(), "not in co-create") {
		t.Fatalf("非共创态应报 not in co-create，得 %v", err)
	}
}

func TestGuardExclusive(t *testing.T) {
	cases := []struct {
		name       string
		lc         lifecycle
		cocreating bool
		wantErr    string // rỗng = kỳ vọng cho qua
	}{
		{"running", lifecycleRunning, false, "运行中"},
		{"cocreating", lifecyclePaused, true, "阶段共创"},
		{"idle free", lifecycleIdle, false, ""},
		{"paused free", lifecyclePaused, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newFlagTestHost(c.lc, c.cocreating)
			err := h.guardExclusive("导入")
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("应放行，得 %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("应含 %q，得 %v", c.wantErr, err)
			}
			if !strings.Contains(err.Error(), "导入") {
				t.Errorf("错误文案应带 action %q，得 %v", "导入", err)
			}
		})
	}
}

// TestStageCoCreate_OccupancyBlocksConcurrentEntries kiểm chứng mọi lối vào độc quyền trong cửa sổ đồng sáng tạo đều bị chặn:
// import/start/resume/continue trong thời gian cocreating đều phải bị từ chối, lấp lỗ hổng giai đoạn paused chỉ kiểm ==running.
func TestStageCoCreate_OccupancyBlocksConcurrentEntries(t *testing.T) {
	h := newFlagTestHost(lifecycleIdle, false)
	if !h.PauseForCoCreate() {
		t.Fatal("进入阶段共创失败")
	}

	if _, err := h.ImportFrom(context.Background(), imp.Options{}); err == nil {
		t.Error("共创窗口内 ImportFrom 应被拒")
	}
	if err := h.StartPrepared("写个新故事"); err == nil {
		t.Error("共创窗口内 StartPrepared 应被拒")
	}
	if _, err := h.Resume(); err == nil {
		t.Error("共创窗口内 Resume 应被拒")
	}
	if err := h.Continue("继续写"); err == nil {
		t.Error("共创窗口内 Continue 应被拒")
	}

	// Sau khi thoát đồng sáng tạo thì giải phóng chiếm dụng (ở đây đi qua Cancel; đường inject Resume cần coordinator, để cho integration kiểm chứng)
	h.CancelCoCreate()
	if h.cocreating {
		t.Fatal("退出后占用标记应解除")
	}
}

func TestBuildStoryStateSummary_NilStore(t *testing.T) {
	if got := buildStoryStateSummary(nil); got != "" {
		t.Errorf("nil store 应返回空串，得 %q", got)
	}
}

func TestBuildStoryStateSummary_Populated(t *testing.T) {
	prev := contentlang.Current()
	contentlang.Set("zh")
	t.Cleanup(func() { contentlang.Set(prev) })
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if err := st.Progress.Init("影之诗", 100); err != nil {
		t.Fatal(err)
	}
	p, _ := st.Progress.Load()
	p.CompletedChapters = []int{1, 2, 3}
	p.TotalWordCount = 12000
	if err := st.Progress.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := st.Outline.SaveCompass(domain.StoryCompass{
		EndingDirection: "主角登临绝巅",
		OpenThreads:     []string{"师门血仇未报"},
		EstimatedScale:  "预计 4-6 卷",
	}); err != nil {
		t.Fatal(err)
	}

	got := buildStoryStateSummary(st)
	for _, want := range []string{"影之诗", "已完成 3 章", "下一章为第 4 章", "主角登临绝巅", "师门血仇未报", "预计 4-6 卷"} {
		if !strings.Contains(got, want) {
			t.Errorf("摘要应含 %q，实际:\n%s", want, got)
		}
	}
}
