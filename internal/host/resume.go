package host

import (
	"fmt"
	"os"
	"strings"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// buildResumePrompt dựa trên sự thật sinh ra prompt ngắn và nhãn UI dùng cho Resume.
//
// Ghi chú tái cấu trúc (2026-04-20): mọi quyết định "bước tiếp theo cụ thể nên làm gì" đã hạ xuống Host Flow Router.
// Hàm này không còn lập kế hoạch hành động thay Coordinator, chỉ làm ba việc:
//  1. Xét có cần khôi phục hay không (Phase=Complete hoặc không có Progress → trả về label rỗng nghĩa là tạo mới)
//  2. Sinh label thích hợp để hiển thị trên UI (kiểu "khôi phục: review cuối cung truyện đang chờ (V2 A3)")
//  3. Truyền tường minh PendingSteer mà người dùng để lại trong lúc dừng máy cho Coordinator
//
// Trả về (prompt, label, error). label rỗng nghĩa là không có trạng thái nào để khôi phục (nên đi tạo mới).
func buildResumePrompt(store *storepkg.Store) (string, string, error) {
	progress, err := store.Progress.Load()
	if err != nil && !os.IsNotExist(err) {
		return "", "", err
	}
	if progress == nil || progress.Phase == domain.PhaseComplete {
		return "", "", nil
	}

	label := describeResume(store, progress)

	var b strings.Builder
	title := progress.NovelName
	if title == "" {
		title = contentlang.Pick("当前小说", "Tiểu thuyết hiện tại")
	}
	b.WriteString(fmt.Sprintf(contentlang.Pick("[恢复] 本书「%s」", "[恢复] Cuốn sách 「%s」"), title))
	if n := len(progress.CompletedChapters); n > 0 {
		b.WriteString(fmt.Sprintf(contentlang.Pick("已完成 %d 章", " đã hoàn thành %d chương"), n))
		if progress.TotalChapters > 0 {
			b.WriteString(fmt.Sprintf(contentlang.Pick("（共 %d 章）", " (tổng %d chương)"), progress.TotalChapters))
		}
		b.WriteString(fmt.Sprintf(contentlang.Pick("，共 %d 字", ", tổng %d chữ"), progress.TotalWordCount))
	}
	b.WriteString(contentlang.Pick("。\n", ".\n"))
	b.WriteString(contentlang.Pick("Host 将根据当前事实下达下一步 `[Host 下达指令]` 消息。收到后立即执行，不要先调 novel_context 推理。\n", "Host sẽ căn cứ vào sự thật hiện tại để ra lệnh bước kế tiếp qua tin nhắn `[Host 下达指令]`. Nhận được thì thực thi ngay, không tra novel_context để suy luận trước.\n"))

	if meta, _ := store.RunMeta.Load(); meta != nil && meta.PendingSteer != "" {
		b.WriteString(contentlang.Pick("\n用户在停机期间留下了一条干预意见：\n「", "\nNgười dùng để lại một ý kiến can thiệp trong lúc dừng máy:\n「"))
		b.WriteString(meta.PendingSteer)
		b.WriteString(contentlang.Pick("」\n请先按 coordinator.md 的用户干预规则评估处理。", "」\nHãy đánh giá xử lý theo quy tắc can thiệp người dùng của coordinator.md trước."))
	}

	return b.String(), label, nil
}

// describeResume sinh nhãn khôi phục mà người đọc được; không ảnh hưởng hành vi của Coordinator.
// Mọi route thực thi do Flow Router suy ra theo sự thật; ở đây chỉ là "khôi phục: xxx" hướng tới UI.
func describeResume(store *storepkg.Store, progress *domain.Progress) string {
	switch progress.Phase {
	case domain.PhasePremise, domain.PhaseOutline:
		return fmt.Sprintf(contentlang.Pick("恢复：规划阶段（%s）", "Khôi phục: giai đoạn lập kế hoạch (%s)"), progress.Phase)
	case domain.PhaseWriting:
		// Độ ưu tiên khớp với độ ưu tiên quyết định của Router, cho label nhất quán với chỉ thị sắp được phái.
		if pending, _ := store.Signals.LoadPendingCommit(); pending != nil {
			return fmt.Sprintf(contentlang.Pick("恢复：第 %d 章提交中断", "Khôi phục: chương %d nộp dở dang"), pending.Chapter)
		}
		if len(progress.PendingRewrites) > 0 {
			verb := contentlang.Pick("重写", "viết lại")
			if progress.Flow == domain.FlowPolishing {
				verb = contentlang.Pick("打磨", "trau chuốt")
			}
			return fmt.Sprintf(contentlang.Pick("%s恢复：%d 章待处理", "%s — khôi phục: %d chương chờ xử lý"), verb, len(progress.PendingRewrites))
		}
		if progress.Flow == domain.FlowReviewing {
			return contentlang.Pick("恢复：审阅中断", "Khôi phục: rà soát dở dang")
		}
		if progress.InProgressChapter > 0 {
			return fmt.Sprintf(contentlang.Pick("恢复：第 %d 章进行中", "Khôi phục: chương %d đang tiến hành"), progress.InProgressChapter)
		}
		if label := describeArcEndLabel(store, progress); label != "" {
			return label
		}
		return fmt.Sprintf(contentlang.Pick("恢复：从第 %d 章继续", "Khôi phục: tiếp tục từ chương %d"), progress.NextChapter())
	}
	return contentlang.Pick("恢复", "Khôi phục")
}

// describeArcEndLabel sinh nhãn hợp với UI cho nhiều trạng thái trung gian ở cuối cung truyện/cuối quyển.
// Giữ cùng thứ tự với nhánh cuối cung truyện của flow.Route, bảo đảm label khớp với chỉ thị đầu tiên của Router.
func describeArcEndLabel(store *storepkg.Store, progress *domain.Progress) string {
	if !progress.Layered || len(progress.CompletedChapters) == 0 {
		return ""
	}
	lastCh := progress.CompletedChapters[len(progress.CompletedChapters)-1]
	boundary, err := store.Outline.CheckArcBoundary(lastCh)
	if err != nil || boundary == nil || !boundary.IsArcEnd {
		return ""
	}
	vol, arc := boundary.Volume, boundary.Arc
	switch {
	case !store.World.HasArcReview(lastCh):
		return fmt.Sprintf(contentlang.Pick("恢复：弧末评审待处理（V%d A%d）", "Khôi phục: chờ rà soát cuối cung truyện (V%d A%d)"), vol, arc)
	case !store.Summaries.HasArcSummary(vol, arc):
		return fmt.Sprintf(contentlang.Pick("恢复：弧摘要待生成（V%d A%d）", "Khôi phục: chờ tạo tóm tắt cung truyện (V%d A%d)"), vol, arc)
	case boundary.IsVolumeEnd && !store.Summaries.HasVolumeSummary(vol):
		return fmt.Sprintf(contentlang.Pick("恢复：卷摘要待生成（V%d）", "Khôi phục: chờ tạo tóm tắt quyển (V%d)"), vol)
	case boundary.NeedsExpansion && boundary.NextArc > 0:
		return fmt.Sprintf(contentlang.Pick("恢复：待展开下一弧（V%d A%d）", "Khôi phục: chờ mở rộng cung truyện kế (V%d A%d)"), boundary.NextVolume, boundary.NextArc)
	case boundary.NeedsNewVolume:
		return fmt.Sprintf(contentlang.Pick("恢复：待决策下一卷（V%d 末）", "Khôi phục: chờ quyết định quyển kế (cuối V%d)"), vol)
	}
	return ""
}
