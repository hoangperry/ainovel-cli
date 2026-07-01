package diag

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/voocel/ainovel-cli/internal/i18n"
)

// ChronicLowDimension phát hiện một chiều rà soát liên tục điểm thấp qua nhiều chương.
func ChronicLowDimension(snap *Snapshot) []Finding {
	if len(snap.Reviews) < 2 {
		return nil
	}

	dimSums := make(map[string]float64)
	dimCounts := make(map[string]int)
	for _, r := range snap.Reviews {
		for _, d := range r.Dimensions {
			dimSums[d.Dimension] += float64(d.Score)
			dimCounts[d.Dimension]++
		}
	}

	var findings []Finding
	for name, sum := range dimSums {
		count := dimCounts[name]
		if count < 2 {
			continue
		}
		avg := sum / float64(count)
		if avg >= ThresholdDimScoreLow {
			continue
		}
		findings = append(findings, Finding{
			Rule:       "ChronicLowDimension",
			Category:   CatQuality,
			Severity:   SevWarning,
			Confidence: ConfMedium,
			AutoLevel:  AutoNone,
			Target:     "prompt.writer",
			Title:      i18n.Tf("diag.quality.chronic_low_dim.title", name, avg),
			Evidence:   i18n.Tf("diag.quality.chronic_low_dim.evidence", count, avg),
			Suggestion: i18n.Tf("diag.quality.chronic_low_dim.suggestion", name, name),
		})
	}
	return findings
}

// ContractMissPattern phát hiện tỉ lệ đạt hợp đồng quá thấp.
func ContractMissPattern(snap *Snapshot) []Finding {
	if len(snap.Reviews) == 0 {
		return nil
	}

	var total, missed int
	var missedChapters []string
	for ch, r := range snap.Reviews {
		total++
		if r.ContractStatus == "partial" || r.ContractStatus == "missed" {
			missed++
			missedChapters = append(missedChapters, fmt.Sprintf("ch%d", ch))
		}
	}
	if total == 0 {
		return nil
	}
	rate := float64(missed) / float64(total)
	if rate <= ThresholdContractMissRate {
		return nil
	}
	return []Finding{{
		Rule:       "ContractMissPattern",
		Category:   CatQuality,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "prompt.writer",
		Title:      i18n.Tf("diag.quality.contract_miss.title", rate*100),
		Evidence:   i18n.Tf("diag.quality.contract_miss.evidence", strings.Join(missedChapters, ", "), missed, total),
		Suggestion: i18n.T("diag.quality.contract_miss.suggestion"),
	}}
}

// HookWeakChain phát hiện điểm hook của chương liên tiếp hơi yếu.
func HookWeakChain(snap *Snapshot) []Finding {
	if len(snap.Reviews) < ThresholdHookWeakChain {
		return nil
	}

	chapters := sortedChapterReviews(snap)
	var weakChain []int
	for _, ch := range chapters {
		review := snap.Reviews[ch]
		if review == nil || review.Scope != "chapter" {
			continue
		}
		hook := review.Dimension("hook")
		if hook == nil || hook.Score >= ThresholdHookWeakScore {
			if len(weakChain) >= ThresholdHookWeakChain {
				break
			}
			weakChain = weakChain[:0]
			continue
		}
		weakChain = append(weakChain, ch)
	}
	if len(weakChain) < ThresholdHookWeakChain {
		return nil
	}

	var parts []string
	for _, ch := range weakChain {
		if hook := snap.Reviews[ch].Dimension("hook"); hook != nil {
			parts = append(parts, fmt.Sprintf("ch%d(%d)", ch, hook.Score))
		}
	}
	return []Finding{{
		Rule:       "HookWeakChain",
		Category:   CatQuality,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "prompt.writer",
		Title:      i18n.Tf("diag.quality.hook_weak.title", len(weakChain)),
		Evidence:   strings.Join(parts, ", "),
		Suggestion: i18n.T("diag.quality.hook_weak.suggestion"),
	}}
}

// PayoffMissPattern phát hiện chương có payoff_points lâu ngày không hoàn trả.
func PayoffMissPattern(snap *Snapshot) []Finding {
	var total, missed int
	var details []string
	for ch, plan := range snap.Plans {
		if plan == nil || len(plan.Contract.PayoffPoints) == 0 {
			continue
		}
		review := snap.Reviews[ch]
		if review == nil {
			continue
		}
		total++
		if review.ContractStatus == "partial" || review.ContractStatus == "missed" {
			missed++
			details = append(details, i18n.Tf("diag.quality.payoff_miss.detail", ch, len(plan.Contract.PayoffPoints)))
		}
	}
	if total < 2 {
		return nil
	}
	rate := float64(missed) / float64(total)
	if rate <= ThresholdPayoffMissRate {
		return nil
	}
	sort.Strings(details)
	return []Finding{{
		Rule:       "PayoffMissPattern",
		Category:   CatQuality,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "prompt.writer",
		Title:      i18n.Tf("diag.quality.payoff_miss.title", rate*100),
		Evidence:   i18n.Tf("diag.quality.payoff_miss.evidence", strings.Join(details, ", "), missed, total),
		Suggestion: i18n.T("diag.quality.payoff_miss.suggestion"),
	}}
}

// ExcessiveRewrites phát hiện tỉ lệ viết lại quá cao.
func ExcessiveRewrites(snap *Snapshot) []Finding {
	if len(snap.Reviews) < 2 {
		return nil
	}

	var total, rewrites int
	for _, r := range snap.Reviews {
		total++
		if r.Verdict == "rewrite" {
			rewrites++
		}
	}
	if total == 0 {
		return nil
	}
	rate := float64(rewrites) / float64(total)
	if rate <= ThresholdRewriteRate {
		return nil
	}
	return []Finding{{
		Rule:       "ExcessiveRewrites",
		Category:   CatQuality,
		Severity:   SevWarning,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "prompt.editor",
		Title:      i18n.Tf("diag.quality.excessive_rewrites.title", rewrites, total, rate*100),
		Evidence:   i18n.Tf("diag.quality.excessive_rewrites.evidence", total, rewrites),
		Suggestion: i18n.T("diag.quality.excessive_rewrites.suggestion"),
	}}
}

// WordCountAnomaly phát hiện số chữ chương bất thường.
func WordCountAnomaly(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.ChapterWordCounts) < 3 {
		return nil
	}
	wc := snap.Progress.ChapterWordCounts

	var sum float64
	for _, w := range wc {
		sum += float64(w)
	}
	avg := sum / float64(len(wc))
	if avg == 0 {
		return nil
	}

	var anomalies []string
	for ch, w := range wc {
		ratio := float64(w) / avg
		if ratio < ThresholdWordShortRatio {
			anomalies = append(anomalies, i18n.Tf("diag.quality.word_anomaly.detail", ch, w, ratio*100))
		} else if ratio > ThresholdWordLongRatio {
			anomalies = append(anomalies, i18n.Tf("diag.quality.word_anomaly.detail", ch, w, ratio*100))
		}
	}
	if len(anomalies) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "WordCountAnomaly",
		Category:   CatQuality,
		Severity:   SevInfo,
		Confidence: ConfLow,
		AutoLevel:  AutoNone,
		Target:     "context.window",
		Title:      i18n.Tf("diag.quality.word_anomaly.title", int(math.Round(avg))),
		Evidence:   strings.Join(anomalies, "; "),
		Suggestion: i18n.T("diag.quality.word_anomaly.suggestion"),
	}}
}

func sortedChapterReviews(snap *Snapshot) []int {
	chapters := make([]int, 0, len(snap.Reviews))
	for ch := range snap.Reviews {
		chapters = append(chapters, ch)
	}
	sort.Ints(chapters)
	return chapters
}
