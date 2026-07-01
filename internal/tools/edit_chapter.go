package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/voocel/agentcore/schema"
	agentcoretools "github.com/voocel/agentcore/tools"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// EditChapterTool thay thế chuỗi có định vị trên bản nháp chương, phù hợp cho tình huống mài giũa.
// So với việc draft_chapter viết lại cả chương, tiết kiệm token 10x+.
//
// Hợp đồng lưu đĩa: chỉ sửa drafts/{ch:02d}.draft.md, cấm sửa trực tiếp chapters/ (bản chốt do commit_chapter độc quyền).
// Ngữ nghĩa Seed: drafts không tồn tại nhưng chapters có → tự động sao chép chapters sang drafts làm điểm khởi đầu.
// Kiểm tra quy thuộc: khi chương đã hoàn thành thì phải nằm trong hàng đợi PendingRewrites, nếu không sẽ từ chối.
//
// Tool này là lớp bọc mỏng của agentcore.EditTool, logic tìm-thay (khớp dung sai nhiều cấp, xuất diff, giữ nguyên đầu cuối dòng/BOM)
// đều tái sử dụng cài đặt upstream.
type EditChapterTool struct {
	store *store.Store
	edit  *agentcoretools.EditTool
}

func NewEditChapterTool(s *store.Store) *EditChapterTool {
	return &EditChapterTool{
		store: s,
		edit:  agentcoretools.NewEdit(s.Dir(), nil),
	}
}

func (t *EditChapterTool) Name() string  { return "edit_chapter" }
func (t *EditChapterTool) Label() string { return i18n.T("ui.tool.edit_chapter.label") }

// ReadOnly khai báo rõ đây là tool ghi (kết hợp ConcurrencySafeTool để tránh bị điều phối song song).
func (t *EditChapterTool) ReadOnly(_ json.RawMessage) bool { return false }

// ConcurrencySafe cấm song song một cách tường minh: nhiều lần edit_chapter song song trên cùng chương sẽ gây tranh chấp đọc-sửa-ghi,
// kể cả các chương khác nhau chạy song song cũng làm xen kẽ thứ tự checkpoint. Tuần tự thống nhất là ổn định nhất.
func (t *EditChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

// ActivityDescription cung cấp mô tả hoạt động hiện tại của tool cho UI/log hiển thị.
func (t *EditChapterTool) ActivityDescription(_ json.RawMessage) string {
	return i18n.T("ui.tool.edit_chapter.activity")
}

func (t *EditChapterTool) Description() string {
	return contentlang.Pick(
		"对章节草稿做定点字符串替换（打磨场景首选，比 draft_chapter 整章重写省 token）。"+
			"找到 old_string 并替换为 new_string，要求精确匹配且唯一（多处匹配需 replace_all=true）。"+
			"写入 drafts/{ch}.draft.md；drafts 不存在时自动从 chapters 播种。"+
			"章节已完成且不在 PendingRewrites 队列中时拒绝执行。每次调用只改一处，多处修改请多次调用。",
		"Thay thế chuỗi có định vị trên bản nháp chương (ưu tiên cho tình huống mài giũa, tiết kiệm token hơn draft_chapter viết lại cả chương). "+
			"Tìm old_string và thay bằng new_string, yêu cầu khớp chính xác và duy nhất (khớp nhiều nơi cần replace_all=true). "+
			"Ghi vào drafts/{ch}.draft.md; khi drafts chưa tồn tại sẽ tự gieo từ chapters. "+
			"Khi chương đã hoàn thành mà không nằm trong hàng đợi PendingRewrites thì từ chối thực thi. Mỗi lần gọi chỉ sửa một chỗ, sửa nhiều chỗ thì gọi nhiều lần.",
	)
}

func (t *EditChapterTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapter", schema.Int(contentlang.Pick("章节号", "Số chương"))).Required(),
		schema.Property("old_string", schema.String(contentlang.Pick("要替换的原文精确片段，多行需包含换行；不加 replace_all 时必须在草稿中唯一出现", "Đoạn nguyên văn chính xác cần thay thế, nhiều dòng phải gồm ký tự xuống dòng; khi không thêm replace_all thì phải xuất hiện duy nhất trong bản nháp"))).Required(),
		schema.Property("new_string", schema.String(contentlang.Pick("替换后的新文本", "Văn bản mới sau khi thay thế"))).Required(),
		schema.Property("replace_all", schema.Bool(contentlang.Pick("替换所有匹配（默认 false）", "Thay thế tất cả khớp (mặc định false)"))),
	)
}

func (t *EditChapterTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter    int    `json:"chapter"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	if a.OldString == "" {
		return nil, fmt.Errorf(i18n.T("error.tool.edit.old_empty"), errs.ErrToolArgs)
	}
	if a.OldString == a.NewString {
		return nil, fmt.Errorf(i18n.T("error.tool.edit.old_eq_new"), errs.ErrToolArgs)
	}

	// Kiểm tra quy thuộc: chương đã hoàn thành phải nằm trong hàng đợi viết lại, tránh làm bẩn bản chốt
	if t.store.Progress.IsChapterCompleted(a.Chapter) {
		progress, _ := t.store.Progress.Load()
		if progress == nil || !slices.Contains(progress.PendingRewrites, a.Chapter) {
			return nil, fmt.Errorf(i18n.T("error.tool.edit.done_locked"), a.Chapter, errs.ErrToolPrecondition)
		}
	}

	// Seed: khi drafts không tồn tại thì sao chép một bản từ chapters làm điểm khởi đầu
	if err := t.ensureDraft(a.Chapter); err != nil {
		return nil, err
	}

	// Ủy thác cho agentcore.EditTool hoàn thành việc tìm-thay
	subArgs, _ := json.Marshal(map[string]any{
		"path":        fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		"file_path":   fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
		"old_text":    a.OldString,
		"old_string":  a.OldString,
		"new_text":    a.NewString,
		"new_string":  a.NewString,
		"replace_all": a.ReplaceAll,
	})
	result, err := t.edit.Execute(ctx, subArgs)
	if err != nil {
		return nil, fmt.Errorf("apply edit: %w: %w", errs.ErrToolPrecondition, err)
	}

	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(a.Chapter), "edit",
		fmt.Sprintf("drafts/%02d.draft.md", a.Chapter),
	); err != nil {
		return nil, fmt.Errorf("checkpoint edit: %w: %w", errs.ErrStoreWrite, err)
	}

	// Chỉ dẫn bổ sung: để writer biết các bước tiếp theo, tránh bỏ sót check_consistency / commit_chapter
	var passthrough map[string]any
	if err := json.Unmarshal(result, &passthrough); err != nil {
		return result, nil
	}
	passthrough["chapter"] = a.Chapter
	passthrough["next_step"] = contentlang.Pick(
		"edit 已落盘。仍有硬伤可再次 edit_chapter；否则 check_consistency 后 commit_chapter",
		"edit đã lưu. Nếu còn lỗi nặng có thể edit_chapter lại; nếu không thì check_consistency rồi commit_chapter",
	)
	return json.Marshal(passthrough)
}

// ensureDraft đảm bảo drafts/{ch}.draft.md tồn tại:
//   - đã có bản nháp → trả về ngay
//   - không có bản nháp nhưng có bản chốt → sao chép bản chốt sang drafts làm điểm khởi đầu để sửa (thường gặp trong tình huống mài giũa)
//   - đều không có → báo lỗi, nhắc dùng draft_chapter để tạo bản sơ thảo trước
func (t *EditChapterTool) ensureDraft(chapter int) error {
	draft, err := t.store.Drafts.LoadDraft(chapter)
	if err != nil {
		return fmt.Errorf("load draft: %w: %w", errs.ErrStoreRead, err)
	}
	if draft != "" {
		return nil
	}
	text, err := t.store.Drafts.LoadChapterText(chapter)
	if err != nil {
		return fmt.Errorf("load chapter: %w: %w", errs.ErrStoreRead, err)
	}
	if text == "" {
		return fmt.Errorf(i18n.T("error.tool.edit.no_draft"), chapter, chapter, errs.ErrToolPrecondition)
	}
	if err := t.store.Drafts.SaveDraft(chapter, text); err != nil {
		return fmt.Errorf("seed draft from chapter: %w: %w", errs.ErrStoreWrite, err)
	}
	return nil
}
