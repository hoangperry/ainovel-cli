package diag

import (
	"fmt"

	"github.com/voocel/ainovel-cli/internal/contentlang"
)

// PlanActions sinh action có thể thực thi dựa trên Finding độ tin cậy cao.
// Chỉ Finding có Confidence==high && AutoLevel==safe mới sinh ra Action.
func PlanActions(findings []Finding) []Action {
	var actions []Action
	seen := make(map[string]struct{})

	for _, f := range findings {
		if f.Confidence != ConfHigh || f.AutoLevel != AutoSafe {
			continue
		}
		if _, ok := seen[f.Rule]; ok {
			continue
		}
		seen[f.Rule] = struct{}{}

		actions = append(actions, planRule(f)...)
	}
	return actions
}

func planRule(f Finding) []Action {
	key := findingFingerprint(f)

	switch f.Rule {
	case "PhaseFlowMismatch":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEmitNotice, Severity: f.Severity, Summary: f.Title, Message: f.Title, Fingerprint: key},
			{SourceRule: f.Rule, Kind: ActionEnqueueFollowUp, Severity: f.Severity, Summary: contentlang.Pick("状态机异常修复", "Sửa lỗi máy trạng thái"), Message: contentlang.Pick("状态机异常：", "Máy trạng thái bất thường: ") + f.Evidence + contentlang.Pick("。请先检查并修正 progress 的 phase/flow 状态，再继续运行。", ". Hãy kiểm tra và sửa trạng thái phase/flow của progress trước, rồi tiếp tục chạy."), Fingerprint: key},
		}
	case "OutlineExhausted":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEnqueueFollowUp, Severity: f.Severity, Summary: contentlang.Pick("大纲耗尽处理", "Xử lý cạn dàn ý"), Message: contentlang.Pick("已完成章节数达到已规划上限。请优先调用 Architect 展开下一弧或追加新卷，再继续写作。", "Số chương đã hoàn thành đạt trần đã quy hoạch. Hãy ưu tiên gọi Architect để mở rộng cung truyện tiếp theo hoặc thêm quyển mới, rồi tiếp tục viết."), Fingerprint: key},
		}
	case "OrphanedSteer":
		return []Action{
			{SourceRule: f.Rule, Kind: ActionEnqueueFollowUp, Severity: f.Severity, Summary: contentlang.Pick("消费未处理的用户干预", "Tiêu thụ can thiệp người dùng chưa xử lý"), Message: contentlang.Pick("存在未消费的用户干预指令，请优先处理 pending steer 后再继续当前任务。", "Tồn tại lệnh can thiệp người dùng chưa tiêu thụ, hãy ưu tiên xử lý pending steer rồi tiếp tục tác vụ hiện tại."), Fingerprint: key},
		}
	default:
		return nil
	}
}

func findingFingerprint(f Finding) string {
	return fmt.Sprintf("%s|%s|%s|%s", f.Rule, f.Target, f.Title, f.Evidence)
}
