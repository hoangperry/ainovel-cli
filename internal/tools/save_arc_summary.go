package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SaveArcSummaryTool lưu tóm tắt cấp cung truyện và snapshot nhân vật, Editor gọi khi cung truyện kết thúc.
type SaveArcSummaryTool struct {
	store *store.Store
}

func NewSaveArcSummaryTool(store *store.Store) *SaveArcSummaryTool {
	return &SaveArcSummaryTool{store: store}
}

func (t *SaveArcSummaryTool) Name() string { return "save_arc_summary" }
func (t *SaveArcSummaryTool) Description() string {
	return contentlang.Pick(
		"保存弧级摘要和角色状态快照（长篇模式，弧结束时调用）",
		"Lưu tóm tắt cấp cung truyện và ảnh chụp trạng thái nhân vật (chế độ truyện dài, gọi khi kết thúc cung truyện)",
	)
}
func (t *SaveArcSummaryTool) Label() string { return i18n.T("ui.tool.save_arc_summary.label") }

// Tool ghi, cấm song song.
func (t *SaveArcSummaryTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *SaveArcSummaryTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *SaveArcSummaryTool) Schema() map[string]any {
	snapshotSchema := schema.Object(
		schema.Property("name", schema.String(contentlang.Pick("角色名", "Tên nhân vật"))).Required(),
		schema.Property("status", schema.String(contentlang.Pick("当前状态（存活/受伤/失踪等）", "Trạng thái hiện tại (còn sống/bị thương/mất tích...)"))).Required(),
		schema.Property("power", schema.String(contentlang.Pick("能力变化", "Thay đổi năng lực"))),
		schema.Property("motivation", schema.String(contentlang.Pick("当前动机", "Động cơ hiện tại"))).Required(),
		schema.Property("relations", schema.String(contentlang.Pick("关键关系变化", "Thay đổi quan hệ then chốt"))),
	)
	voiceSchema := schema.Object(
		schema.Property("name", schema.String(contentlang.Pick("角色名", "Tên nhân vật"))).Required(),
		schema.Property("rules", schema.Array(contentlang.Pick("2-3 条语言特征规则（每条 ≤30 字）", "2-3 quy tắc đặc trưng ngôn ngữ (mỗi quy tắc ≤30 chữ)"), schema.String(""))).Required(),
	)
	styleRulesSchema := schema.Object(
		schema.Property("prose", schema.Array(contentlang.Pick("3-5 条叙述风格规则（每条 ≤50 字，要具体可执行）", "3-5 quy tắc phong cách tự sự (mỗi quy tắc ≤50 chữ, phải cụ thể và khả thi)"), schema.String(""))).Required(),
		schema.Property("dialogue", schema.Array(contentlang.Pick("核心角色的对话特征规则", "Quy tắc đặc trưng đối thoại của nhân vật cốt lõi"), voiceSchema)).Required(),
		schema.Property("taboos", schema.Array(contentlang.Pick("本小说需避免的写法", "Cách viết cần tránh của tiểu thuyết này"), schema.String(""))),
	)
	return schema.Object(
		schema.Property("volume", schema.Int(contentlang.Pick("卷号", "Số quyển"))).Required(),
		schema.Property("arc", schema.Int(contentlang.Pick("弧号", "Số cung truyện"))).Required(),
		schema.Property("title", schema.String(contentlang.Pick("弧标题", "Tiêu đề cung truyện"))).Required(),
		schema.Property("summary", schema.String(contentlang.Pick("弧摘要（500字以内）", "Tóm tắt cung truyện (trong 500 chữ)"))).Required(),
		schema.Property("key_events", schema.Array(contentlang.Pick("弧内关键事件", "Sự kiện then chốt trong cung truyện"), schema.String(""))).Required(),
		schema.Property("character_snapshots", schema.Array(contentlang.Pick("角色状态快照", "Ảnh chụp trạng thái nhân vật"), snapshotSchema)).Required(),
		schema.Property("style_rules", styleRulesSchema),
	)
}

func (t *SaveArcSummaryTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Volume             int                        `json:"volume"`
		Arc                int                        `json:"arc"`
		Title              string                     `json:"title"`
		Summary            string                     `json:"summary"`
		KeyEvents          []string                   `json:"key_events"`
		CharacterSnapshots []domain.CharacterSnapshot `json:"character_snapshots"`
		StyleRules         *arcSummaryStyleRules      `json:"style_rules"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		if strings.Contains(err.Error(), "style_rules.dialogue") {
			return nil, fmt.Errorf("invalid args: style_rules.dialogue must be an array of objects {name, rules}, not strings: %w: %w", errs.ErrToolArgs, err)
		}
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Volume <= 0 || a.Arc <= 0 {
		return nil, fmt.Errorf("volume and arc must be > 0: %w", errs.ErrToolArgs)
	}
	if err := validateArcSummaryStyleRules(a.StyleRules); err != nil {
		return nil, err
	}

	arcSummary := domain.ArcSummary{
		Volume:    a.Volume,
		Arc:       a.Arc,
		Title:     a.Title,
		Summary:   a.Summary,
		KeyEvents: a.KeyEvents,
	}
	if err := t.store.Summaries.SaveArcSummary(arcSummary); err != nil {
		return nil, fmt.Errorf("save arc summary: %w: %w", errs.ErrStoreWrite, err)
	}

	if len(a.CharacterSnapshots) > 0 {
		for i := range a.CharacterSnapshots {
			a.CharacterSnapshots[i].Volume = a.Volume
			a.CharacterSnapshots[i].Arc = a.Arc
		}
		if err := t.store.Characters.SaveSnapshots(a.Volume, a.Arc, a.CharacterSnapshots); err != nil {
			return nil, fmt.Errorf("save character snapshots: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	styleRulesSaved := false
	if a.StyleRules != nil && len(a.StyleRules.Prose) > 0 {
		rules := domain.WritingStyleRules{
			Volume:    a.Volume,
			Arc:       a.Arc,
			Prose:     a.StyleRules.Prose,
			Dialogue:  a.StyleRules.Dialogue,
			Taboos:    a.StyleRules.Taboos,
			UpdatedAt: time.Now().Format(time.RFC3339),
		}
		if err := t.store.World.SaveStyleRules(rules); err != nil {
			return nil, fmt.Errorf("save style rules: %w: %w", errs.ErrStoreWrite, err)
		}
		styleRulesSaved = true
	}

	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ArcScope(a.Volume, a.Arc), "arc_summary",
		fmt.Sprintf("summaries/arc-v%02da%02d.json", a.Volume, a.Arc),
	); err != nil {
		return nil, fmt.Errorf("checkpoint arc summary: %w: %w", errs.ErrStoreWrite, err)
	}

	return json.Marshal(map[string]any{
		"saved": true, "type": "arc_summary",
		"volume": a.Volume, "arc": a.Arc,
		"snapshots":         len(a.CharacterSnapshots),
		"style_rules_saved": styleRulesSaved,
	})
}

type arcSummaryStyleRules struct {
	Prose    []string                `json:"prose"`
	Dialogue []domain.CharacterVoice `json:"dialogue"`
	Taboos   []string                `json:"taboos"`
}

func validateArcSummaryStyleRules(rules *arcSummaryStyleRules) error {
	if rules == nil {
		return nil
	}
	if len(rules.Prose) == 0 {
		return fmt.Errorf("style_rules.prose is required when style_rules is provided: %w", errs.ErrToolArgs)
	}
	if len(rules.Dialogue) == 0 {
		return fmt.Errorf("style_rules.dialogue is required when style_rules is provided; expected array of objects {name, rules}: %w", errs.ErrToolArgs)
	}
	for i, voice := range rules.Dialogue {
		if strings.TrimSpace(voice.Name) == "" {
			return fmt.Errorf("style_rules.dialogue[%d].name is required: %w", i, errs.ErrToolArgs)
		}
		if len(voice.Rules) == 0 {
			return fmt.Errorf("style_rules.dialogue[%d].rules is required: %w", i, errs.ErrToolArgs)
		}
		for j, rule := range voice.Rules {
			if strings.TrimSpace(rule) == "" {
				return fmt.Errorf("style_rules.dialogue[%d].rules[%d] is empty: %w", i, j, errs.ErrToolArgs)
			}
		}
	}
	return nil
}
