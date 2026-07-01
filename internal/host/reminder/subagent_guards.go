package reminder

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// subagentMaxConsecutiveBlocks chặn liên tiếp N lần thì nâng thành kết thúc, tránh model yếu rơi vào vòng lặp chết.
const subagentMaxConsecutiveBlocks = 3

// hardStopReasons là các lý do từ chối trả lời phía provider mà không thể khôi phục bằng tin nhắn thúc giục. Chèn
// "phải commit" với chúng vô hiệu, ngược lại mỗi lần tốn token của một lần gọi LLM đầy đủ,
// và cuối cùng sau khi escalate khiến coordinator phái lại toàn bộ SubAgent, cộng dồn lãng phí gấp nhiều lần
// (đo thực tế ch02 đụng safety thì một lần viết chương sinh 3 lần phái lại 17 lần gọi LLM, tỉ lệ trúng
// tụt từ 50% xuống 2.8%).
//
// Lưu ý StopReasonError / StopReasonAborted không cần liệt kê: agentcore khi nhận hai stop reason này
// ở loop.go thì kết thúc run ngay, hoàn toàn không gọi StopGuard.
// Ở đây chỉ liệt kê các ngữ nghĩa từ chối trả lời của provider thực sự đi tới StopGuard.
var hardStopReasons = map[agentcore.StopReason]struct{}{
	"safety":         {},
	"content_filter": {},
}

// newCheckpointDeltaGuard dựng một StopGuard:
// sau baseline nếu chưa xuất hiện checkpoint của step được chỉ định thì từ chối end_turn.
// baseline được caller bắt tại thời điểm factory, đảm bảo ngữ nghĩa per-run đúng.
func newCheckpointDeltaGuard(st *store.Store, agentName string, requiredSteps []string, blockMsg string) agentcore.StopGuard {
	var baseline int64
	if cp := st.Checkpoints.LatestGlobal(); cp != nil {
		baseline = cp.Seq
	}
	need := make(map[string]struct{}, len(requiredSteps))
	for _, s := range requiredSteps {
		need[s] = struct{}{}
	}
	var consecutive atomic.Int32
	return func(_ context.Context, info agentcore.StopInfo) agentcore.StopDecision {
		// Lỗi không thể khôi phục: nâng cấp ngay, không phí một lần thúc giục.
		if _, hard := hardStopReasons[info.Message.StopReason]; hard {
			slog.Error(i18n.T("log.reminder.subagent_unrecoverable_escalate"),
				"module", "host.reminder", "agent", agentName,
				"turn", info.TurnIndex, "stop_reason", info.Message.StopReason)
			return agentcore.StopDecision{Allow: false, Escalate: true}
		}
		// Quét ngược: checkpoint mới ở cuối, gặp <= baseline là có thể break.
		all := st.Checkpoints.All()
		for i := len(all) - 1; i >= 0; i-- {
			cp := all[i]
			if cp.Seq <= baseline {
				break
			}
			if _, ok := need[cp.Step]; ok {
				consecutive.Store(0)
				return agentcore.StopDecision{Allow: true}
			}
		}
		n := consecutive.Add(1)
		if n > subagentMaxConsecutiveBlocks {
			slog.Error(i18n.T("log.reminder.subagent_block_limit_escalate"),
				"module", "host.reminder", "agent", agentName, "turn", info.TurnIndex, "consecutive", n)
			return agentcore.StopDecision{Allow: false, Escalate: true}
		}
		slog.Warn(i18n.T("log.reminder.subagent_block_end_turn"),
			"module", "host.reminder", "agent", agentName, "turn", info.TurnIndex, "consecutive", n)
		return agentcore.StopDecision{Allow: false, InjectMessage: blockMsg}
	}
}

// NewWriterStopGuard yêu cầu writer trong vòng này tạo ra ít nhất một commit_chapter thành công.
func NewWriterStopGuard(st *store.Store) agentcore.StopGuard {
	return newCheckpointDeltaGuard(st, "writer",
		[]string{"commit"},
		contentlang.Pick("你必须调用 commit_chapter 提交本章后才能结束。draft_chapter 只是保存草稿，不算完成。", "Bạn phải gọi commit_chapter để nộp chương này thì mới được kết thúc. draft_chapter chỉ lưu bản nháp, không tính là hoàn thành."),
	)
}

// NewArchitectStopGuard yêu cầu architect trong vòng này ghi xuống ít nhất một save_foundation.
func NewArchitectStopGuard(st *store.Store) agentcore.StopGuard {
	return newCheckpointDeltaGuard(st, "architect",
		[]string{
			"premise", "outline", "layered_outline", "characters", "world_rules",
			"expand_arc", "append_volume", "update_compass", "complete_book",
		},
		contentlang.Pick("你必须调用 save_foundation 将产出落盘后才能结束。只输出 Markdown/JSON 文字等于丢失。", "Bạn phải gọi save_foundation để ghi sản phẩm xuống đĩa thì mới được kết thúc. Chỉ xuất văn bản Markdown/JSON coi như mất."),
	)
}

// NewEditorStopGuard yêu cầu editor trong vòng này ghi xuống sản phẩm khớp với "nhiệm vụ" mới được kết thúc.
//
// Nhận biết nhiệm vụ: khi được phái đi tạo tóm tắt, chỉ save_review (rà soát) không tính là hoàn thành — phải tạo ra tóm tắt tương ứng.
// Nếu không thì editor "bị phái tạo tóm tắt cung truyện nhưng lại rà soát trước" sẽ thỏa mãn tiêu chí lỏng cũ và kết thúc sớm, tóm tắt cung truyện không bao giờ được ghi xuống
// (kết hợp với việc khử trùng của dispatcher bị câm từng gây vòng lặp chết ở cung truyện khung xương giữa quyển, xem outline-exhaustion-livelock).
// Thoát bằng StopAfterTool sẽ né StopGuard (loop.go), nên build.go đồng bộ chuyển save_review ra khỏi hard stop,
// để sau khi rà soát có thể đi tiếp tới tool tóm tắt, rồi giao guard này canh khâu kết.
func NewEditorStopGuard(st *store.Store, task string) agentcore.StopGuard {
	switch {
	case strings.Contains(task, "save_volume_summary") || strings.Contains(task, "卷摘要"):
		return newCheckpointDeltaGuard(st, "editor", []string{"volume_summary"},
			contentlang.Pick("本次任务是生成卷摘要：你必须调用 save_volume_summary 落盘后才能结束，save_review 复核不算完成。", "Nhiệm vụ lần này là tạo tóm tắt quyển: bạn phải gọi save_volume_summary ghi xuống đĩa thì mới được kết thúc, save_review rà soát không tính là hoàn thành."))
	case strings.Contains(task, "save_arc_summary") || strings.Contains(task, "弧摘要"):
		return newCheckpointDeltaGuard(st, "editor", []string{"arc_summary"},
			contentlang.Pick("本次任务是生成弧摘要：你必须调用 save_arc_summary 落盘后才能结束，save_review 复核不算完成。", "Nhiệm vụ lần này là tạo tóm tắt cung truyện: bạn phải gọi save_arc_summary ghi xuống đĩa thì mới được kết thúc, save_review rà soát không tính là hoàn thành."))
	default:
		// Rà soát hoặc nhiệm vụ tạm thời: ghi xuống bất kỳ rà soát/tóm tắt nào cũng được (giữ hành vi lỏng sẵn có).
		return newCheckpointDeltaGuard(st, "editor",
			[]string{"review", "arc_summary", "volume_summary"},
			contentlang.Pick("你必须调用 save_review / save_arc_summary / save_volume_summary 之一落盘结果后才能结束。", "Bạn phải gọi một trong save_review / save_arc_summary / save_volume_summary để ghi kết quả xuống đĩa thì mới được kết thúc."))
	}
}
