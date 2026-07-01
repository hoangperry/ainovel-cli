package reminder

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// StopGuard là phòng tuyến cuối "vật lý không thể dừng máy".
// Khi LLM thử end_turn:
//   - Progress.Phase = Complete → cho qua
//   - nếu không thì chèn user message, để agent tiếp tục turn kế
//   - chặn liên tiếp quá maxConsecutive lần → Escalate kết thúc run (cho thấy prompt/reminder hỏng nặng)
//
// Guard duy trì bên trong bộ đếm consecutive block; một khi cho qua thành công hoặc chèn thành công thì reset về 0.
// Cái thực sự thúc đẩy hành vi Coordinator là Reminder + Prompt, StopGuard chỉ là phương án cuối.
const maxConsecutiveBlocks = 5

// NewStopGuard dựng StopGuard chuyên dụng cho Coordinator.
// onBlock tùy chọn, khi khác nil thì mỗi lần chặn gọi một lần, dùng để kiểm toán.
func NewStopGuard(st *store.Store, onBlock func(reason string, consecutive int32)) agentcore.StopGuard {
	var consecutive atomic.Int32
	var lastBlockTurn atomic.Int64 // TurnIndex của lần block trước; -1 nghĩa là chưa từng block
	lastBlockTurn.Store(-1)
	return func(_ context.Context, info agentcore.StopInfo) agentcore.StopDecision {
		progress, _ := st.Progress.Load()
		if progress != nil && progress.Phase == domain.PhaseComplete {
			consecutive.Store(0)
			lastBlockTurn.Store(-1)
			return agentcore.StopDecision{Allow: true}
		}
		// Chỉ "các turn liền kề bị chặn liên tiếp" mới cộng dồn bộ đếm; nếu không thì coi là vòng mới (LLM đã làm tool call và có tiến triển,
		// hoặc người dùng chèn / resume khiến TurnIndex chạy ngược), reset bộ đếm.
		last := lastBlockTurn.Load()
		if last < 0 || int64(info.TurnIndex) != last+1 {
			consecutive.Store(0)
		}
		lastBlockTurn.Store(int64(info.TurnIndex))
		n := consecutive.Add(1)
		if n > maxConsecutiveBlocks {
			slog.Error(i18n.T("log.reminder.block_limit_escalate"),
				"module", "host.reminder", "turn", info.TurnIndex, "consecutive", n)
			if onBlock != nil {
				onBlock("escalated", n)
			}
			return agentcore.StopDecision{Allow: false, Escalate: true}
		}
		inject := contentlang.Pick("禁止结束对话。Phase 尚未 Complete，请继续下一步（查 novel_context 或调子代理）。", "Cấm kết thúc hội thoại. Phase chưa Complete, hãy tiếp tục bước kế (tra novel_context hoặc gọi subagent).")
		if progress != nil && len(progress.PendingRewrites) > 0 {
			inject = fmt.Sprintf(contentlang.Pick("禁止结束对话。待重写队列未清：%v，请立即调 writer 处理。", "Cấm kết thúc hội thoại. Hàng đợi chờ viết lại chưa hết: %v, hãy gọi ngay writer để xử lý."), progress.PendingRewrites)
		}
		slog.Warn(i18n.T("log.reminder.block_end_turn"),
			"module", "host.reminder", "turn", info.TurnIndex, "consecutive", n)
		if onBlock != nil {
			onBlock("blocked", n)
		}
		return agentcore.StopDecision{Allow: false, InjectMessage: inject}
	}
}
