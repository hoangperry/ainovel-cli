package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// ReadChapterTool đọc nguyên văn chương, để Agent có thể đọc lại văn của chính mình và phần trước.
type ReadChapterTool struct {
	store *store.Store
}

func NewReadChapterTool(store *store.Store) *ReadChapterTool {
	return &ReadChapterTool{store: store}
}

func (t *ReadChapterTool) Name() string { return "read_chapter" }
func (t *ReadChapterTool) Description() string {
	return contentlang.Pick(
		"读取章节原文。可读终稿、草稿，或提取角色对话片段",
		"Đọc nguyên văn chương. Có thể đọc bản cuối, bản nháp, hoặc trích đoạn hội thoại của nhân vật",
	)
}
func (t *ReadChapterTool) Label() string { return i18n.T("ui.tool.read_chapter.label") }

// Tool chỉ đọc, có thể được điều phối song song (editor khi rà soát thường đọc nhiều chương một lần).
func (t *ReadChapterTool) ReadOnly(_ json.RawMessage) bool        { return true }
func (t *ReadChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return true }

func (t *ReadChapterTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapter", schema.Int(contentlang.Pick("章节号（读单章时必填）", "Số chương (bắt buộc khi đọc một chương)"))),
		schema.Property("from", schema.Int(contentlang.Pick("起始章节号（读范围时使用）", "Số chương bắt đầu (dùng khi đọc theo phạm vi)"))),
		schema.Property("to", schema.Int(contentlang.Pick("结束章节号（读范围时使用）", "Số chương kết thúc (dùng khi đọc theo phạm vi)"))),
		schema.Property("source", schema.Enum(contentlang.Pick("来源", "Nguồn"), "final", "draft")).Required(),
		schema.Property("character", schema.String(contentlang.Pick("角色名（提取对话片段时使用）", "Tên nhân vật (dùng khi trích đoạn đối thoại)"))),
		schema.Property("max_runes", schema.Int(contentlang.Pick("每章最大字符数（范围读取时截取，默认 2000）", "Số ký tự tối đa mỗi chương (cắt khi đọc theo phạm vi, mặc định 2000)"))),
	)
}

func (t *ReadChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter   int    `json:"chapter"`
		From      int    `json:"from"`
		To        int    `json:"to"`
		Source    string `json:"source"`
		Character string `json:"character"`
		MaxRunes  int    `json:"max_runes"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}

	// Chế độ 1: trích xuất hội thoại nhân vật
	if a.Character != "" {
		chars, _ := t.store.Characters.Load()
		var aliases []string
		for _, c := range chars {
			if c.Name == a.Character {
				aliases = c.Aliases
				break
			}
		}
		var maxCompleted int
		if p, _ := t.store.Progress.Load(); p != nil {
			maxCompleted = maxCompletedChapter(p.CompletedChapters)
		}
		samples := t.store.Drafts.ExtractDialogue(a.Character, aliases, 8, maxCompleted)
		result := map[string]any{
			"character": a.Character,
			"samples":   samples,
		}
		if len(samples) == 0 {
			result["hint"] = contentlang.Pick(
				"该角色暂无对话样本，无需重试，直接进入下一步",
				"Nhân vật này chưa có mẫu hội thoại, không cần thử lại, hãy chuyển sang bước tiếp theo",
			)
		}
		return json.Marshal(result)
	}

	// Chế độ 2: đọc theo phạm vi
	if a.From > 0 && a.To > 0 {
		maxRunes := a.MaxRunes
		if maxRunes <= 0 {
			maxRunes = 2000
		}
		texts, err := t.store.Drafts.LoadChapterRange(a.From, a.To, maxRunes)
		if err != nil {
			return nil, fmt.Errorf("load chapter range: %w", err)
		}
		return json.Marshal(map[string]any{
			"chapters": texts,
			"from":     a.From,
			"to":       a.To,
		})
	}

	// Chế độ 3: đọc một chương
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter is required")
	}

	var content string
	var err error
	switch a.Source {
	case "draft":
		content, err = t.store.Drafts.LoadDraft(a.Chapter)
	default: // final
		content, err = t.store.Drafts.LoadChapterText(a.Chapter)
		if err == nil && content == "" {
			slog.Warn(i18n.T("log.tool.final_empty_fallback_draft"), "module", "tool", "chapter", a.Chapter)
			content, err = t.store.Drafts.LoadDraft(a.Chapter)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("read chapter %d: %w", a.Chapter, err)
	}
	if content == "" {
		return json.Marshal(map[string]any{
			"chapter": a.Chapter,
			"exists":  false,
			"hint": contentlang.Pick(
				"该章节尚未写入，如需写作请先调用 draft_chapter",
				"Chương này chưa được ghi, nếu cần viết hãy gọi draft_chapter trước",
			),
		})
	}

	return json.Marshal(map[string]any{
		"chapter":    a.Chapter,
		"content":    content,
		"word_count": len([]rune(content)),
	})
}

// maxCompletedChapter trả về số chương lớn nhất trong danh sách chương đã hoàn thành.
func maxCompletedChapter(completed []int) int {
	m := 0
	for _, ch := range completed {
		if ch > m {
			m = ch
		}
	}
	return m
}
