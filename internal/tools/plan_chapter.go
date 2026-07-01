package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// PlanChapterTool lưu ý tưởng chương, Agent tự quyết định độ chi tiết của kế hoạch.
type PlanChapterTool struct {
	store *store.Store
}

func NewPlanChapterTool(store *store.Store) *PlanChapterTool {
	return &PlanChapterTool{store: store}
}

func (t *PlanChapterTool) Name() string { return "plan_chapter" }
func (t *PlanChapterTool) Description() string {
	return contentlang.Pick(
		"保存章节写作构思。Agent 自主决定规划粒度，不强制场景拆分",
		"Lưu ý tưởng viết chương. Agent tự quyết định độ chi tiết của kế hoạch, không bắt buộc tách cảnh",
	)
}
func (t *PlanChapterTool) Label() string { return i18n.T("ui.tool.plan_chapter.label") }

// Tool ghi, cấm song song.
func (t *PlanChapterTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *PlanChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *PlanChapterTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapter", schema.Int(contentlang.Pick("章节号", "Số chương"))).Required(),
		schema.Property("title", schema.String(contentlang.Pick("章节标题", "Tiêu đề chương"))).Required(),
		schema.Property("goal", schema.String(contentlang.Pick("本章目标", "Mục tiêu chương này"))).Required(),
		schema.Property("conflict", schema.String(contentlang.Pick("核心冲突", "Xung đột cốt lõi"))).Required(),
		schema.Property("hook", schema.String(contentlang.Pick("章末钩子", "Hook cuối chương"))).Required(),
		schema.Property("emotion_arc", schema.String(contentlang.Pick("情绪曲线", "Đường cong cảm xúc"))),
		schema.Property("notes", schema.String(contentlang.Pick("自由备忘（任何你觉得写作时需要记住的东西）", "Ghi chú tự do (bất cứ điều gì bạn thấy cần nhớ khi viết)"))),
		schema.Property("required_beats", schema.Array(contentlang.Pick("本章必须完成的推进项", "Các hạng mục thúc đẩy bắt buộc hoàn thành trong chương này"), schema.String(""))),
		schema.Property("forbidden_moves", schema.Array(contentlang.Pick("本章明确不能发生的推进", "Các thúc đẩy chắc chắn không được xảy ra trong chương này"), schema.String(""))),
		schema.Property("continuity_checks", schema.Array(contentlang.Pick("本章需特别核对的连续性点", "Các điểm liên tục cần đối chiếu đặc biệt trong chương này"), schema.String(""))),
		schema.Property("evaluation_focus", schema.Array(contentlang.Pick("Editor 重点检查项", "Hạng mục Editor cần kiểm tra trọng điểm"), schema.String(""))),
		schema.Property("emotion_target", schema.String(contentlang.Pick("可选：本章希望读者主要感受到的情绪", "Tùy chọn: cảm xúc chính mà chương này muốn người đọc cảm nhận"))),
		schema.Property("payoff_points", schema.Array(contentlang.Pick("可选：关键章希望回应的情节点或兑现点", "Tùy chọn: điểm cốt truyện hoặc điểm hồi đáp mà chương then chốt muốn đáp lại"), schema.String(""))),
		schema.Property("hook_goal", schema.String(contentlang.Pick("可选：章末希望驱动的追读欲望或悬念目标", "Tùy chọn: ham muốn đọc tiếp hoặc mục tiêu hồi hộp muốn thúc đẩy ở cuối chương"))),
	)
}

func (t *PlanChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	plan, err := decodeChapterPlanArgs(args)
	if err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if plan.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	if t.store.Progress.IsChapterCompleted(plan.Chapter) {
		return json.Marshal(map[string]any{
			"chapter":   plan.Chapter,
			"skipped":   true,
			"completed": true,
			"reason": contentlang.Pick(
				fmt.Sprintf("第 %d 章已提交完成，不能重新规划", plan.Chapter),
				fmt.Sprintf("Chương %d đã commit hoàn tất, không thể lập kế hoạch lại", plan.Chapter),
			),
		})
	}
	if err := t.store.Progress.ValidateChapterWork(plan.Chapter); err != nil {
		return nil, err
	}
	if err := EnsureChapterExpanded(t.store, plan.Chapter); err != nil {
		return nil, err
	}

	if err := t.store.Drafts.SaveChapterPlan(plan); err != nil {
		return nil, fmt.Errorf("save chapter plan: %w", err)
	}
	if err := t.store.Progress.StartChapter(plan.Chapter); err != nil {
		return nil, fmt.Errorf("mark chapter in progress: %w", err)
	}

	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(plan.Chapter), "plan",
		fmt.Sprintf("drafts/%02d.plan.json", plan.Chapter),
	); err != nil {
		return nil, fmt.Errorf("checkpoint chapter plan: %w", err)
	}

	return json.Marshal(map[string]any{
		"planned": true,
		"chapter": plan.Chapter,
		"next_step": contentlang.Pick(
			"立即调用 draft_chapter(chapter=本章节号, content=完整正文字符串) 写入正文，不要重复规划同一章",
			"Gọi ngay draft_chapter(chapter=số chương này, content=chuỗi chính văn đầy đủ) để ghi chính văn, không lập kế hoạch lại cùng một chương",
		),
	})
}

func decodeChapterPlanArgs(args json.RawMessage) (domain.ChapterPlan, error) {
	var a struct {
		Chapter          int      `json:"chapter"`
		Title            string   `json:"title"`
		Goal             string   `json:"goal"`
		Conflict         string   `json:"conflict"`
		Hook             string   `json:"hook"`
		EmotionArc       string   `json:"emotion_arc"`
		Notes            string   `json:"notes"`
		RequiredBeats    []string `json:"required_beats"`
		ForbiddenMoves   []string `json:"forbidden_moves"`
		ContinuityChecks []string `json:"continuity_checks"`
		EvaluationFocus  []string `json:"evaluation_focus"`
		EmotionTarget    string   `json:"emotion_target"`
		PayoffPoints     []string `json:"payoff_points"`
		HookGoal         string   `json:"hook_goal"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return domain.ChapterPlan{}, err
	}

	return domain.ChapterPlan{
		Chapter:    a.Chapter,
		Title:      a.Title,
		Goal:       a.Goal,
		Conflict:   a.Conflict,
		Hook:       a.Hook,
		EmotionArc: a.EmotionArc,
		Notes:      a.Notes,
		Contract: domain.ChapterContract{
			RequiredBeats:    a.RequiredBeats,
			ForbiddenMoves:   a.ForbiddenMoves,
			ContinuityChecks: a.ContinuityChecks,
			EvaluationFocus:  a.EvaluationFocus,
			EmotionTarget:    a.EmotionTarget,
			PayoffPoints:     a.PayoffPoints,
			HookGoal:         a.HookGoal,
		},
	}, nil
}
