package diag

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// InvalidPendingRewrites phát hiện chương chưa hoàn thành lẫn vào hàng đợi làm lại.
func InvalidPendingRewrites(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.PendingRewrites) == 0 {
		return nil
	}
	p := snap.Progress
	completed := append([]int(nil), p.CompletedChapters...)
	slices.Sort(completed)

	var invalid []int
	for _, ch := range p.PendingRewrites {
		if ch <= 0 || !slices.Contains(completed, ch) {
			invalid = append(invalid, ch)
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	slices.Sort(invalid)
	return []Finding{{
		Rule:       "InvalidPendingRewrites",
		Category:   CatFlow,
		Severity:   SevCritical,
		Confidence: ConfHigh,
		AutoLevel:  AutoSuggest,
		Target:     "meta/progress.json",
		Title:      i18n.Tf("diag.flow.invalid_pending.title", intsToStr(invalid)),
		Evidence:   i18n.Tf("diag.flow.invalid_pending.evidence", intsToStr(p.PendingRewrites), intsToStr(completed), p.Flow),
		Suggestion: i18n.T("diag.flow.invalid_pending.suggestion"),
	}}
}

// RewritePendingPressure phát hiện có chương chờ viết lại (hiện chỉ phát hiện trạng thái tồn tại, không phán định đình trệ).
func RewritePendingPressure(snap *Snapshot) []Finding {
	if snap.Progress == nil {
		return nil
	}
	p := snap.Progress
	if len(p.PendingRewrites) == 0 {
		return nil
	}
	if p.Flow != domain.FlowRewriting && p.Flow != domain.FlowPolishing {
		return nil
	}
	chapters := intsToStr(p.PendingRewrites)
	return []Finding{{
		Rule:       "RewritePendingPressure",
		Category:   CatFlow,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "runtime.flow",
		Title:      i18n.Tf("diag.flow.rewrite_pending.title", chapters),
		Evidence:   i18n.Tf("diag.flow.rewrite_pending.evidence", p.Flow, chapters),
		Suggestion: i18n.T("diag.flow.rewrite_pending.suggestion"),
	}}
}

// OrphanedSteer phát hiện chỉ thị chuyển hướng của người dùng chưa được tiêu thụ.
func OrphanedSteer(snap *Snapshot) []Finding {
	if snap.RunMeta == nil || snap.RunMeta.PendingSteer == "" {
		return nil
	}
	if snap.Progress != nil && snap.Progress.Flow == domain.FlowSteering {
		return nil // Đang xử lý, không tính là mồ côi
	}
	return []Finding{{
		Rule:       "OrphanedSteer",
		Category:   CatFlow,
		Severity:   SevWarning,
		Confidence: ConfHigh,
		AutoLevel:  AutoSafe,
		Target:     "runtime.recovery",
		Title:      i18n.T("diag.flow.orphaned_steer.title"),
		Evidence:   i18n.Tf("diag.flow.orphaned_steer.evidence", truncStr(snap.RunMeta.PendingSteer, 60), flowStr(snap.Progress)),
		Suggestion: i18n.T("diag.flow.orphaned_steer.suggestion"),
	}}
}

// PhaseFlowMismatch phát hiện giai đoạn và trạng thái quy trình không khớp.
func PhaseFlowMismatch(snap *Snapshot) []Finding {
	if snap.Progress == nil {
		return nil
	}
	p := snap.Progress
	if p.Phase == domain.PhaseWriting || p.Phase == "" {
		return nil
	}
	if p.Flow == "" || p.Flow == domain.FlowWriting {
		return nil
	}
	return []Finding{{
		Rule:       "PhaseFlowMismatch",
		Category:   CatFlow,
		Severity:   SevCritical,
		Confidence: ConfHigh,
		AutoLevel:  AutoSafe,
		Target:     "runtime.flow",
		Title:      i18n.Tf("diag.flow.phase_mismatch.title", p.Phase, p.Flow),
		Evidence:   i18n.Tf("diag.flow.phase_mismatch.evidence", p.Phase, p.Flow),
		Suggestion: i18n.T("diag.flow.phase_mismatch.suggestion"),
	}}
}

// ChapterGaps phát hiện số nhảy cách trong danh sách chương đã hoàn thành.
func ChapterGaps(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.CompletedChapters) < 2 {
		return nil
	}
	sorted := append([]int(nil), snap.Progress.CompletedChapters...)
	sort.Ints(sorted)

	var gaps []int
	for i := 1; i < len(sorted); i++ {
		for ch := sorted[i-1] + 1; ch < sorted[i]; ch++ {
			gaps = append(gaps, ch)
		}
	}
	if len(gaps) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "ChapterGaps",
		Category:   CatFlow,
		Severity:   SevWarning,
		Confidence: ConfHigh,
		AutoLevel:  AutoNone,
		Target:     "runtime.flow",
		Title:      i18n.Tf("diag.flow.chapter_gaps.title", intsToStr(gaps)),
		Evidence:   i18n.Tf("diag.flow.chapter_gaps.evidence", intsToStr(sorted)),
		Suggestion: i18n.T("diag.flow.chapter_gaps.suggestion"),
	}}
}

func flowStr(p *domain.Progress) string {
	if p == nil {
		return "<nil>"
	}
	return string(p.Flow)
}

func truncStr(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-3]) + "..."
}

func intsToStr(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = fmt.Sprintf("%d", n)
	}
	return strings.Join(parts, ", ")
}
