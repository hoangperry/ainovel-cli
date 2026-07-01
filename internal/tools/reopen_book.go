package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// ReopenBookTool mở lại cuốn sách đã kết thúc để vào trạng thái làm lại (chỉ Coordinator nắm giữ).
// Sau khi hoàn thành sách, completePhaseGate chặn cứng mọi việc giao cho subagent, người dùng không thể làm lại các chương đã viết.
// Tool này không phải subagent, có thể gọi trong giai đoạn complete: nó nguyên tử chuyển phase về writing, đưa chương mục tiêu vào
// PendingRewrites, flow=rewriting, sau đó Flow Router theo hàng đợi làm lại sẵn có giao writer viết lại từng chương,
// hàng đợi chạy xong commit_chapter tự động thu xếp kết thúc lại. Logic Gate / Router / edit / commit đều không cần sửa.
type ReopenBookTool struct {
	store *store.Store
}

func NewReopenBookTool(s *store.Store) *ReopenBookTool {
	return &ReopenBookTool{store: s}
}

func (t *ReopenBookTool) Name() string  { return "reopen_book" }
func (t *ReopenBookTool) Label() string { return i18n.T("ui.tool.reopen_book.label") }

func (t *ReopenBookTool) Description() string {
	return contentlang.Pick(
		"把已完结（phase=complete）的全书重新打开进入返工态，用于用户在完本后要求重写/打磨某几章。"+
			"chapters 是要返工的已完成章节号；调用后这些章进入重写队列，Host 会逐章派 writer 重写，全部改完自动重新完结。"+
			"仅在全书已完结、且用户明确要求修改已写章节时使用；用户要新增剧情/扩展篇幅不属返工，不要用本工具。",
		"Mở lại toàn bộ sách đã hoàn thành (phase=complete) để vào trạng thái làm lại, dùng khi người dùng sau khi hoàn thành sách yêu cầu viết lại/mài giũa vài chương. "+
			"chapters là số chương đã hoàn thành cần làm lại; sau khi gọi, các chương này vào hàng đợi viết lại, Host sẽ lần lượt phái writer viết lại từng chương, sửa xong tất cả thì tự động hoàn thành lại. "+
			"Chỉ dùng khi toàn sách đã hoàn thành và người dùng yêu cầu rõ ràng sửa chương đã viết; người dùng muốn thêm tình tiết/mở rộng độ dài không thuộc làm lại, đừng dùng tool này.",
	)
}

// Tool ghi, cấm song song.
func (t *ReopenBookTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *ReopenBookTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *ReopenBookTool) ActivityDescription(_ json.RawMessage) string {
	return i18n.T("ui.tool.reopen_book.activity")
}

func (t *ReopenBookTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapters", schema.Array(contentlang.Pick("要返工的已完成章节号列表（至少一章）", "Danh sách số chương đã hoàn thành cần làm lại (ít nhất một chương)"), schema.Int(""))).Required(),
		schema.Property("reason", schema.String(contentlang.Pick("返工原因（可选，如\"清理特殊字符\"）", "Lý do làm lại (tùy chọn, ví dụ \"dọn dẹp ký tự đặc biệt\")"))),
	)
}

func (t *ReopenBookTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapters []int  `json:"chapters"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if len(a.Chapters) == 0 {
		return nil, fmt.Errorf(i18n.T("error.tool.reopen.chapters_empty"), errs.ErrToolArgs)
	}

	progress, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	if progress == nil {
		return nil, fmt.Errorf(i18n.T("error.tool.reopen.no_progress"), errs.ErrToolPrecondition)
	}
	// Chỉ được làm lại chương đã viết; số chương không nằm trong tập đã hoàn thành thuộc viết tiếp/vượt biên, từ chối rõ ràng và dẫn người dùng đi điều chỉnh độ dài.
	var invalid []int
	for _, ch := range a.Chapters {
		if !slices.Contains(progress.CompletedChapters, ch) {
			invalid = append(invalid, ch)
		}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf(i18n.T("error.tool.reopen.not_done"), invalid, errs.ErrToolPrecondition)
	}

	// Kiểm tra tiền điều kiện phase được dự phòng bên trong store.Reopen (chỉ complete mới gọi được).
	if err := t.store.Progress.Reopen(a.Chapters, a.Reason); err != nil {
		return nil, fmt.Errorf("reopen: %w: %w", errs.ErrStoreWrite, err)
	}

	// checkpoint: đối xứng với complete_book (GlobalScope + meta/progress.json).
	if _, err := t.store.Checkpoints.AppendArtifact(domain.GlobalScope(), "reopen", "meta/progress.json"); err != nil {
		return nil, fmt.Errorf("checkpoint reopen: %w: %w", errs.ErrStoreWrite, err)
	}

	return json.Marshal(map[string]any{
		"reopened":         true,
		"phase":            string(domain.PhaseWriting),
		"pending_rewrites": a.Chapters,
		"next_step": contentlang.Pick(
			"已重新打开并把目标章入队。请等待 Host 指令派 writer 逐章返工；全部改完后会自动重新完结。",
			"Đã mở lại và đưa các chương mục tiêu vào hàng đợi. Hãy chờ Host điều phối writer làm lại từng chương; sau khi sửa xong tất cả sẽ tự động hoàn kết lại.",
		),
	})
}
