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

// CheckConsistencyTool trả về nội dung chương và toàn bộ dữ liệu trạng thái, để Agent tự đối chiếu phán định.
// Tool IO thuần: chỉ chịu trách nhiệm nạp dữ liệu, không tiêm chỉ thị.
type CheckConsistencyTool struct {
	store *store.Store
}

func NewCheckConsistencyTool(store *store.Store) *CheckConsistencyTool {
	return &CheckConsistencyTool{store: store}
}

func (t *CheckConsistencyTool) Name() string { return "check_consistency" }
func (t *CheckConsistencyTool) Description() string {
	return contentlang.Pick(
		"加载已写草稿和对照数据（世界规则、伏笔、关系、别名、最近摘要），供你检查一致性。必须在 draft_chapter 之后调用",
		"Nạp bản nháp đã viết cùng dữ liệu đối chiếu (quy tắc thế giới, phục bút, quan hệ, biệt danh, tóm tắt gần đây) để bạn kiểm tra tính nhất quán. Bắt buộc gọi sau draft_chapter",
	)
}
func (t *CheckConsistencyTool) Label() string { return i18n.T("ui.tool.check_consistency.label") }

// Tool chỉ đọc (chỉ thêm sự kiện checkpoint, không đổi trạng thái), có thể được điều phối song song.
func (t *CheckConsistencyTool) ReadOnly(_ json.RawMessage) bool        { return true }
func (t *CheckConsistencyTool) ConcurrencySafe(_ json.RawMessage) bool { return true }

func (t *CheckConsistencyTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapter", schema.Int(contentlang.Pick("要检查的章节号", "Số chương cần kiểm tra"))).Required(),
	)
}

func (t *CheckConsistencyTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter int `json:"chapter"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}

	result := map[string]any{"chapter": a.Chapter}

	// Nội dung chương
	content, wordCount, err := t.store.Drafts.LoadChapterContent(a.Chapter)
	if err != nil {
		return nil, fmt.Errorf("load chapter content: %w: %w", errs.ErrStoreRead, err)
	}
	if content == "" {
		return nil, fmt.Errorf("no content found for chapter %d: %w", a.Chapter, errs.ErrToolPrecondition)
	}
	result["content"] = content
	result["word_count"] = wordCount

	// Dữ liệu đối chiếu: giữ lại dữ liệu kiểm tra nhất quán mang tính toàn cục, tránh nạp lại dữ liệu cửa sổ mà novel_context đã có
	if rules, _ := t.store.World.LoadWorldRules(); len(rules) > 0 {
		result["world_rules"] = rules
	}
	if foreshadow, _ := t.store.World.LoadActiveForeshadow(); len(foreshadow) > 0 {
		result["foreshadow_ledger"] = foreshadow
	}
	if relationships, _ := t.store.World.LoadRelationships(); len(relationships) > 0 {
		result["relationships"] = relationships
	}
	if chars, _ := t.store.Characters.Load(); len(chars) > 0 {
		aliasMap := make(map[string]string)
		for _, c := range chars {
			for _, alias := range c.Aliases {
				aliasMap[alias] = c.Name
			}
		}
		if len(aliasMap) > 0 {
			result["alias_map"] = aliasMap
		}
	}
	if summaries, _ := t.store.Summaries.LoadRecentSummaries(a.Chapter, 2); len(summaries) > 0 {
		result["recent_summaries"] = summaries
	}

	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(a.Chapter), "consistency_check",
		fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
	); err != nil {
		return nil, fmt.Errorf("checkpoint consistency check: %w", err)
	}

	return json.Marshal(result)
}
