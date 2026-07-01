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

// SaveDirectiveTool lưu bền yêu cầu sáng tác dài hạn của người dùng (chỉ Coordinator nắm giữ).
// Lưu xuống meta/user_directives.json, novel_context tiêm vào working_memory.user_directives,
// mọi subagent đều tự động thấy mỗi chương — không phụ thuộc vào việc Coordinator truyền đạt thủ công khi giao việc, có hiệu lực xuyên qua nén và xuyên qua khởi động lại.
type SaveDirectiveTool struct {
	store *store.Store
}

func NewSaveDirectiveTool(s *store.Store) *SaveDirectiveTool {
	return &SaveDirectiveTool{store: s}
}

func (t *SaveDirectiveTool) Name() string  { return "save_directive" }
func (t *SaveDirectiveTool) Label() string { return i18n.T("ui.tool.save_directive.label") }

func (t *SaveDirectiveTool) Description() string {
	return contentlang.Pick(
		"持久化用户的长效创作要求（如\"以后对话占比提高\"\"章节标题只用中文\"）。"+
			"保存后所有子代理每章都会在 working_memory.user_directives 看到，无需再转达。"+
			"action=add 追加一条（text 必填，原样保留用户意图，可适当凝练）；"+
			"action=remove 按序号删除（index 必填，序号见上次返回的列表）。"+
			"返回更新后的全量列表。只保存状态式要求（任何时候重读都成立的描述）；"+
			"相对式/动作式指令（如\"增加10章\"）禁止保存——本工具不派发子代理，存了等于没人执行，请走子代理路由立即处理。",
		"Lưu lâu dài yêu cầu sáng tác dài hạn của người dùng (như \"sau này tăng tỷ lệ hội thoại\"\"tiêu đề chương chỉ dùng tiếng Trung\"). "+
			"Sau khi lưu, mọi sub-agent ở mỗi chương đều thấy trong working_memory.user_directives, không cần truyền đạt lại. "+
			"action=add nối thêm một mục (text bắt buộc, giữ nguyên ý người dùng, có thể cô đọng hợp lý); "+
			"action=remove xóa theo số thứ tự (index bắt buộc, số thứ tự xem ở danh sách trả về lần trước). "+
			"Trả về danh sách đầy đủ sau khi cập nhật. Chỉ lưu yêu cầu dạng trạng thái (mô tả luôn đúng bất cứ lúc nào đọc lại); "+
			"chỉ thị dạng tương đối/hành động (như \"thêm 10 chương\") cấm lưu — tool này không phái sub-agent, lưu cũng không ai thực thi, hãy đi qua định tuyến sub-agent để xử lý ngay.",
	)
}

// Tool ghi, cấm song song.
func (t *SaveDirectiveTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *SaveDirectiveTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *SaveDirectiveTool) ActivityDescription(_ json.RawMessage) string {
	return i18n.T("ui.tool.save_directive.activity")
}

func (t *SaveDirectiveTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("action", schema.Enum(contentlang.Pick("操作类型", "Loại thao tác"), "add", "remove")).Required(),
		schema.Property("text", schema.String(contentlang.Pick("要求内容（add 时必填）：一句话说清要求，保留用户原意", "Nội dung yêu cầu (bắt buộc khi add): nói rõ yêu cầu trong một câu, giữ nguyên ý người dùng"))),
		schema.Property("index", schema.Int(contentlang.Pick("要删除的条目序号（remove 时必填，1-based，见列表返回的 index）", "Số thứ tự mục cần xóa (bắt buộc khi remove, 1-based, xem index trả về trong danh sách)"))),
	)
}

func (t *SaveDirectiveTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Action string `json:"action"`
		Text   string `json:"text"`
		Index  int    `json:"index"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}

	var (
		list []domain.UserDirective
		err  error
	)
	switch a.Action {
	case "add":
		text := strings.TrimSpace(a.Text)
		if text == "" {
			return nil, fmt.Errorf(i18n.T("error.tool.directive.add_need_text"), errs.ErrToolArgs)
		}
		chapter, total := 0, 0
		if progress, perr := t.store.Progress.Load(); perr == nil && progress != nil {
			chapter = progress.NextChapter()
			total = progress.TotalChapters
		}
		list, err = t.store.Directives.Add(domain.UserDirective{
			Text:          text,
			Chapter:       chapter,
			TotalChapters: total,
			CreatedAt:     time.Now().Format(time.RFC3339),
		})
	case "remove":
		if a.Index < 1 {
			return nil, fmt.Errorf(i18n.T("error.tool.directive.remove_need_idx"), errs.ErrToolArgs)
		}
		list, err = t.store.Directives.Remove(a.Index)
	default:
		return nil, fmt.Errorf("unknown action %q: %w", a.Action, errs.ErrToolArgs)
	}
	if err != nil {
		return nil, err
	}

	items := directiveFacts(list)
	return json.Marshal(map[string]any{
		"saved":      true,
		"directives": items,
		"count":      len(items),
	})
}

// directiveFacts chuyển chỉ thị dài hạn thành khung nhìn sự kiện cho LLM (cùng hình dạng với kết quả tool và phần tiêm vào envelope):
// at_* là ảnh chụp tiến độ tại thời điểm ban hành — chỉ thị có hiệu lực từ at_chapter trở về sau, cách diễn đạt tương đối có thể dựa vào
// at_total_chapters để xác định đã thỏa mãn hay chưa. created_at là thông tin kiểm toán, không đưa vào LLM.
func directiveFacts(list []domain.UserDirective) []map[string]any {
	items := make([]map[string]any, len(list))
	for i, d := range list {
		items[i] = map[string]any{
			"index":             i + 1,
			"text":              d.Text,
			"at_chapter":        d.Chapter,
			"at_total_chapters": d.TotalChapters,
		}
	}
	return items
}
