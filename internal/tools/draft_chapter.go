package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// DraftChapterTool ghi bản nháp cả chương, thay thế pipeline cũ write_scene + polish_chapter.
// Agent tự quyết định viết xong một lần hay viết tiếp theo từng đợt.
type DraftChapterTool struct {
	store *store.Store
}

func NewDraftChapterTool(store *store.Store) *DraftChapterTool {
	return &DraftChapterTool{store: store}
}

func (t *DraftChapterTool) Name() string { return "draft_chapter" }
func (t *DraftChapterTool) Description() string {
	return contentlang.Pick(
		"写入章节正文。mode=write 覆盖写入整章，mode=append 追加到现有草稿（续写/修改）",
		"Ghi nội dung chính của chương. mode=write ghi đè toàn chương, mode=append nối thêm vào bản nháp hiện có (viết tiếp/sửa)",
	)
}
func (t *DraftChapterTool) Label() string { return i18n.T("ui.tool.draft_chapter.label") }

// Tool ghi, cấm song song (tranh chấp đọc-sửa-ghi).
func (t *DraftChapterTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *DraftChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *DraftChapterTool) Schema() map[string]any {
	// Đánh dấu mode là required để tương thích OpenAI strict tool calling — chế độ strict
	// yêu cầu mọi properties đều nằm trong danh sách required. Hành vi cũ "bỏ mode thì mặc định
	// đi write" giờ yêu cầu model truyền tường minh mode="write", nhánh default của Execute không đổi.
	return schema.Object(
		schema.Property("chapter", schema.Int(contentlang.Pick("章节号", "Số chương"))).Required(),
		schema.Property("content", schema.String(contentlang.Pick("章节正文", "Nội dung chính của chương"))).Required(),
		schema.Property("mode", schema.Enum(contentlang.Pick("写入模式", "Chế độ ghi"), "write", "append")).Required(),
	)
}

// StrictSchema bật strict tool calling của OpenAI, buộc model phải tuân thủ nghiêm ngặt
// schema: mọi trường required đều bắt buộc, arguments không được "EOT sớm" tạo ra đối tượng rỗng.
// litellm truyền thẳng trường strict; các backend hỗ trợ như OpenAI / xAI sẽ cưỡng chế thực thi, các backend khác
// theo thông lệ HTTP/JSON bỏ qua trường không xác định. Anthropic/Gemini/Bedrock đi qua chuỗi chuyển đổi riêng
// nên tự nhiên không thấy trường này.
func (t *DraftChapterTool) StrictSchema() bool { return true }

func (t *DraftChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter int    `json:"chapter"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	if a.Content == "" {
		return nil, fmt.Errorf("content must not be empty: %w", errs.ErrToolArgs)
	}
	if err := t.store.Progress.ValidateChapterWork(a.Chapter); err != nil {
		return nil, err
	}
	if err := EnsureChapterExpanded(t.store, a.Chapter); err != nil {
		return nil, err
	}
	if t.store.Progress.IsChapterCompleted(a.Chapter) {
		// Đường đi mài giũa/viết lại: chương tuy đã hoàn thành nhưng vẫn nằm trong pending_rewrites, cho phép ghi đè bản nháp
		progress, _ := t.store.Progress.Load()
		inRewriteQueue := progress != nil && slices.Contains(progress.PendingRewrites, a.Chapter)
		if !inRewriteQueue {
			return json.Marshal(map[string]any{
				"chapter":   a.Chapter,
				"skipped":   true,
				"completed": true,
				"reason": contentlang.Pick(
					fmt.Sprintf("第 %d 章已提交完成，不能覆盖", a.Chapter),
					fmt.Sprintf("Chương %d đã commit hoàn tất, không thể ghi đè", a.Chapter),
				),
			})
		}
	}
	if err := t.store.Progress.StartChapter(a.Chapter); err != nil {
		return nil, fmt.Errorf("mark chapter in progress: %w", err)
	}

	switch a.Mode {
	case "append":
		if err := t.store.Drafts.AppendDraft(a.Chapter, a.Content); err != nil {
			return nil, fmt.Errorf("append draft: %w", err)
		}
		full, err := t.store.Drafts.LoadDraft(a.Chapter)
		if err != nil {
			return nil, fmt.Errorf("load draft after append: %w", err)
		}
		if _, err := t.store.Checkpoints.AppendArtifact(
			domain.ChapterScope(a.Chapter), "draft",
			fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		); err != nil {
			return nil, fmt.Errorf("checkpoint draft: %w", err)
		}
		return json.Marshal(map[string]any{
			"written":    true,
			"chapter":    a.Chapter,
			"mode":       "append",
			"word_count": utf8.RuneCountInString(full),
			"next_step": contentlang.Pick(
				"先 read_chapter(source=draft) 回读草稿，再调用 check_consistency，最后 commit_chapter",
				"Trước tiên read_chapter(source=draft) để đọc lại bản nháp, rồi gọi check_consistency, cuối cùng commit_chapter",
			),
		})
	default: // write
		if err := t.store.Drafts.SaveDraft(a.Chapter, a.Content); err != nil {
			return nil, fmt.Errorf("save draft: %w", err)
		}
		if _, err := t.store.Checkpoints.AppendArtifact(
			domain.ChapterScope(a.Chapter), "draft",
			fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		); err != nil {
			return nil, fmt.Errorf("checkpoint draft: %w", err)
		}
		return json.Marshal(map[string]any{
			"written":    true,
			"chapter":    a.Chapter,
			"mode":       "write",
			"word_count": utf8.RuneCountInString(a.Content),
			"next_step": contentlang.Pick(
				"先 read_chapter(source=draft) 回读草稿，再调用 check_consistency，最后 commit_chapter",
				"Trước tiên read_chapter(source=draft) để đọc lại bản nháp, rồi gọi check_consistency, cuối cùng commit_chapter",
			),
		})
	}
}
