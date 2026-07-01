package diag

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Ngưỡng phát hiện trạng thái chạy.
const (
	repeatCritical = 8 // Trùng lặp cận điểm đạt số lần này thì nâng lên critical
	streamIdleWarn = 3 // Ngưỡng cảnh báo tích luỹ stream_idle
)

// RuntimeRuleFunc là chữ ký thống nhất của quy tắc chẩn đoán trạng thái chạy (tương ứng RuleFunc phía sáng tác).
// Tham số vào là RuntimeCapture đã tổng hợp khử nhạy cảm, sinh ra Finding kiểu báo cáo — tất cả AutoNone,
// chỉ chẩn đoán, không sinh Action (kỷ luật quan sát viên, xem architecture.md §2.3).
type RuntimeRuleFunc func(rc *RuntimeCapture) []Finding

var runtimeRules = []RuntimeRuleFunc{
	repeatedErrors,
	stuckStep,
	streamIdleStorm,
}

// runtimeFindings chạy toàn bộ quy tắc trạng thái chạy.
func runtimeFindings(rc *RuntimeCapture) []Finding {
	var out []Finding
	for _, rule := range runtimeRules {
		out = append(out, rule(rc)...)
	}
	return out
}

// Diagnose là điểm vào chẩn đoán đầy đủ của /diag: chẩn đoán sáng tác + tín hiệu trạng thái chạy + phát hiện trạng thái chạy,
// trả về Report đã hợp nhất và RuntimeCapture gốc (để export tái dùng, tránh bắt lại).
// Finding trạng thái chạy chỉ gộp vào Findings để hiển thị, không đổi Actions — giữ thuần quan sát.
func Diagnose(s *store.Store) (Report, RuntimeCapture) {
	rep := Analyze(s)
	rc := CaptureRuntime(s)
	rep.Findings = append(rep.Findings, runtimeFindings(&rc)...)
	sortFindings(rep.Findings)
	return rep, rc
}

// repeatedErrors chỉ phán thành Finding với "lỗi / tham số không hợp lệ xuất hiện lặp lại ở cận điểm".
// Không đụng tới trùng lặp tool thông thường — subagent/novel_context/read_chapter ... trong chạy dài vốn dĩ
// tần suất cao, số lần tích luỹ không phải tín hiệu vòng lặp; cái "lặp mà không tiến" thật sự do stuckStep gánh.
func repeatedErrors(rc *RuntimeCapture) []Finding {
	var out []Finding
	for _, r := range rc.Repeats {
		var rule, title, sugg string
		switch {
		case strings.Contains(r.Sig, " · err: "):
			rule = "RepeatedToolError"
			title = contentlang.Pick("工具反复报同一错误", "Công cụ liên tục báo cùng một lỗi")
			sugg = contentlang.Pick("近端同一工具反复返回同一错误，多为模型参数不合规或工具契约不符；查 agentcore 工具校验 / prompt 参数约定（参见 #34）。", "Cùng một công cụ ở cận điểm liên tục trả về cùng một lỗi, thường do tham số model không hợp lệ hoặc không khớp hợp đồng công cụ; kiểm tra kiểm định công cụ agentcore / quy ước tham số prompt (xem #34).")
		case strings.Contains(r.Sig, "(args invalid)"):
			rule = "ArgsInvalidLoop"
			title = contentlang.Pick("参数反复无法解析", "Tham số liên tục không phân tích được")
			sugg = contentlang.Pick("模型发来的参数无法解析却不断重试；看 agentcore 是否对该类型做了宽松强转（参见 #34）。", "Tham số do model gửi không phân tích được nhưng liên tục thử lại; xem agentcore có ép kiểu nới lỏng cho loại này không (xem #34).")
		default:
			continue // Trùng lặp tool thông thường không sinh Finding
		}
		sev := SevWarning
		if r.Count >= repeatCritical {
			sev = SevCritical
		}
		out = append(out, Finding{
			Rule:       rule,
			Category:   CatFlow,
			Severity:   sev,
			Confidence: ConfHigh,
			AutoLevel:  AutoNone,
			Target:     "runtime.flow",
			Title:      title,
			Evidence:   fmt.Sprintf("`%s` ×%d", r.Sig, r.Count),
			Suggestion: sugg,
		})
	}
	return out
}

// stuckStep phát hiện checkpoint liên tiếp dừng ở cùng một step.
func stuckStep(rc *RuntimeCapture) []Finding {
	if rc.StuckStep == "" {
		return nil
	}
	sev := SevWarning
	if rc.StuckCount >= repeatCritical {
		sev = SevCritical
	}
	return []Finding{{
		Rule:       "StuckStep",
		Category:   CatFlow,
		Severity:   sev,
		Confidence: ConfHigh,
		AutoLevel:  AutoNone,
		Target:     "runtime.flow",
		Title:      contentlang.Pick("checkpoint 停滞在同一 step", "checkpoint dừng lại ở cùng một step"),
		Evidence:   fmt.Sprintf(contentlang.Pick("连续停在 `%s` ×%d", "Liên tục dừng ở `%s` ×%d"), rc.StuckStep, rc.StuckCount),
		Suggestion: contentlang.Pick("同一 step 反复写入而不推进；结合上面的重复签名定位是哪个子代理卡住。", "Cùng một step liên tục ghi mà không tiến lên; kết hợp chữ ký lặp ở trên để xác định sub-agent nào bị kẹt."),
	}}
}

// streamIdleStorm phát hiện gián đoạn stream xảy ra dồn dập (#32).
func streamIdleStorm(rc *RuntimeCapture) []Finding {
	n := rc.LogKinds["stream_idle"]
	if n < streamIdleWarn {
		return nil
	}
	return []Finding{{
		Rule:       "StreamIdleStorm",
		Category:   CatFlow,
		Severity:   SevWarning,
		Confidence: ConfHigh,
		AutoLevel:  AutoNone,
		Target:     "runtime.provider",
		Title:      contentlang.Pick("流式中断频发（stream_idle）", "Gián đoạn stream xảy ra dồn dập (stream_idle)"),
		Evidence:   fmt.Sprintf("stream_idle ×%d", n),
		Suggestion: contentlang.Pick("上游长时间不吐 token 被 watchdog 误杀；慢思考模型调大 streamIdleTimeout，或排查 provider 连接稳定性（参见 #32）。", "Upstream lâu không nhả token nên bị watchdog giết nhầm; với model suy nghĩ chậm hãy tăng streamIdleTimeout, hoặc kiểm tra độ ổn định kết nối provider (xem #32)."),
	}}
}
