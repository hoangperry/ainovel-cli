package diag

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
)

// StaleForeshadow phát hiện phục bút lâu ngày không được đẩy tiến.
func StaleForeshadow(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Foreshadow) == 0 {
		return nil
	}
	latest := snap.LatestCompleted()
	threshold := staleForeshadowThreshold(snap.CompletedCount())

	var stale []string
	for _, f := range snap.Foreshadow {
		if f.Status != "planted" {
			continue
		}
		gap := latest - f.PlantedAt
		if gap > threshold {
			stale = append(stale, fmt.Sprintf(contentlang.Pick("%s(ch%d埋下,已过%d章)", "%s(gieo ở ch%d, đã qua %d chương)"), f.ID, f.PlantedAt, gap))
		}
	}
	if len(stale) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "StaleForeshadow",
		Category:   CatPlanning,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "context.foreshadow",
		Title:      fmt.Sprintf(contentlang.Pick("伏笔停滞: %d 条超过 %d 章未推进", "Phục bút đình trệ: %d phục bút quá %d chương chưa đẩy tiến"), len(stale), threshold),
		Evidence:   strings.Join(stale, "; "),
		Suggestion: contentlang.Pick("novel_context 的伏笔提醒加载可能未生效，或 Writer prompt 缺少推进伏笔的指引。检查 foreshadow_ledger 与上下文注入逻辑。", "Nạp nhắc phục bút của novel_context có thể chưa có hiệu lực, hoặc prompt của Writer thiếu hướng dẫn đẩy tiến phục bút. Kiểm tra foreshadow_ledger và logic chèn ngữ cảnh."),
	}}
}

// CompassDrift phát hiện la bàn lâu ngày không cập nhật.
func CompassDrift(snap *Snapshot) []Finding {
	if snap.Progress == nil || !snap.Progress.Layered {
		return nil
	}
	if snap.Compass == nil {
		if snap.CompletedCount() > 5 {
			return []Finding{{
				Rule:       "CompassDrift",
				Category:   CatPlanning,
				Severity:   SevWarning,
				Confidence: ConfMedium,
				AutoLevel:  AutoNone,
				Target:     "prompt.architect",
				Title:      contentlang.Pick("长篇模式缺少指南针", "Chế độ truyện dài thiếu la bàn"),
				Evidence:   fmt.Sprintf("layered=true, completed=%d, compass=nil", snap.CompletedCount()),
				Suggestion: contentlang.Pick("Architect 应在初始规划时创建 compass。检查 architect-long.md 是否包含 compass 创建指令。", "Architect nên tạo compass khi quy hoạch ban đầu. Kiểm tra architect-long.md có chứa lệnh tạo compass không."),
			}}
		}
		return nil
	}

	gap := snap.LatestCompleted() - snap.Compass.LastUpdated
	if gap <= ThresholdCompassDrift {
		return nil
	}
	return []Finding{{
		Rule:       "CompassDrift",
		Category:   CatPlanning,
		Severity:   SevInfo,
		Confidence: ConfLow,
		AutoLevel:  AutoNone,
		Target:     "prompt.architect",
		Title:      fmt.Sprintf(contentlang.Pick("指南针已 %d 章未更新", "La bàn đã %d chương chưa cập nhật"), gap),
		Evidence:   fmt.Sprintf("last_updated=ch%d, latest=ch%d, open_threads=%d", snap.Compass.LastUpdated, snap.LatestCompleted(), len(snap.Compass.OpenThreads)),
		Suggestion: contentlang.Pick("Architect 应在弧/卷边界更新 compass。检查 architect-long.md 中是否包含 compass 更新指令。", "Architect nên cập nhật compass ở ranh giới cung truyện/quyển. Kiểm tra architect-long.md có chứa lệnh cập nhật compass không."),
	}}
}

// OutlineExhausted phát hiện dàn ý cạn nhưng tiểu thuyết chưa kết thúc.
func OutlineExhausted(snap *Snapshot) []Finding {
	if snap.Progress == nil {
		return nil
	}
	p := snap.Progress
	if p.Phase == domain.PhaseComplete || p.Phase == domain.PhaseInit {
		return nil
	}

	completed := snap.CompletedCount()
	if completed == 0 {
		return nil
	}

	outlinedCount := p.TotalChapters
	if outlinedCount <= 0 {
		outlinedCount = len(snap.Outline)
	}
	if outlinedCount <= 0 {
		return nil
	}

	if completed < outlinedCount {
		return nil
	}

	return []Finding{{
		Rule:       "OutlineExhausted",
		Category:   CatPlanning,
		Severity:   SevCritical,
		Confidence: ConfHigh,
		AutoLevel:  AutoSafe,
		Target:     "runtime.recovery",
		Title:      fmt.Sprintf(contentlang.Pick("大纲耗尽: 已完成 %d 章 >= 已规划 %d 章", "Cạn dàn ý: đã hoàn thành %d chương >= đã quy hoạch %d chương"), completed, outlinedCount),
		Evidence:   fmt.Sprintf("phase=%s, completed=%d, outlined=%d", p.Phase, completed, outlinedCount),
		Suggestion: contentlang.Pick("展开/新卷信号可能未触发。检查宿主侧提交策略和恢复逻辑，确认弧边界检测、expand_arc 或 append_volume 是否正常执行。", "Tín hiệu mở rộng/quyển mới có thể chưa được kích hoạt. Kiểm tra chiến lược commit và logic khôi phục phía host, xác nhận phát hiện ranh giới cung truyện, expand_arc hoặc append_volume có chạy bình thường không."),
	}}
}

// MissingSummaries phát hiện chương đã hoàn thành thiếu tóm tắt.
func MissingSummaries(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.CompletedChapters) == 0 {
		return nil
	}

	var missing []int
	for _, ch := range snap.Progress.CompletedChapters {
		if _, ok := snap.Summaries[ch]; !ok {
			missing = append(missing, ch)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "MissingSummaries",
		Category:   CatPlanning,
		Severity:   SevWarning,
		Confidence: ConfHigh,
		AutoLevel:  AutoNone,
		Target:     "runtime.flow",
		Title:      fmt.Sprintf(contentlang.Pick("缺少摘要: %d 章无摘要", "Thiếu tóm tắt: %d chương không có tóm tắt"), len(missing)),
		Evidence:   fmt.Sprintf("missing=[%s]", intsToStr(missing)),
		Suggestion: contentlang.Pick("摘要是上下文连续性的关键。检查 commit_chapter 的摘要写入逻辑是否正常工作。", "Tóm tắt là then chốt cho tính liên tục của ngữ cảnh. Kiểm tra logic ghi tóm tắt của commit_chapter có hoạt động bình thường không."),
	}}
}
