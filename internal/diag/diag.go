package diag

import (
	"fmt"
	"sort"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/store"
)

// ── Ngưỡng chẩn đoán ─────────────────────────────────────────────

const (
	ThresholdDimScoreLow      = 70  // ChronicLowDimension: điểm trung bình chiều thấp hơn giá trị này thì cảnh báo
	ThresholdContractMissRate = 0.3 // ContractMissPattern: trần tỉ lệ hợp đồng không đạt
	ThresholdRewriteRate      = 0.5 // ExcessiveRewrites: trần tỉ lệ viết lại
	ThresholdWordShortRatio   = 0.4 // WordCountAnomaly: số chữ thấp hơn trung bình theo tỉ lệ này thì xem là bất thường
	ThresholdWordLongRatio    = 2.5 // WordCountAnomaly: số chữ cao hơn trung bình theo tỉ lệ này thì xem là bất thường
	ThresholdHookWeakScore    = 75  // HookWeakChain: hook thấp hơn điểm này thì xem là hơi yếu
	ThresholdHookWeakChain    = 3   // HookWeakChain: ngưỡng số chương hơi yếu liên tiếp
	ThresholdPayoffMissRate   = 0.4 // PayoffMissPattern: trần tỉ lệ payoff không hoàn trả
	ThresholdCompassDrift     = 15  // CompassDrift: trần số chương la bàn không cập nhật
	ThresholdTimelineGapRate  = 0.3 // TimelineGaps: trần dung sai tỉ lệ thiếu
	ThresholdForeshadowMin    = 8   // StaleForeshadow: số chương tối thiểu để phục bút đình trệ
)

// allRules sắp xếp theo flow → quality → planning → context.
var allRules = []RuleFunc{
	// Flow
	InvalidPendingRewrites,
	RewritePendingPressure,
	OrphanedSteer,
	PhaseFlowMismatch,
	ChapterGaps,
	// Quality
	ChronicLowDimension,
	ContractMissPattern,
	HookWeakChain,
	PayoffMissPattern,
	ExcessiveRewrites,
	WordCountAnomaly,
	// Planning
	StaleForeshadow,
	CompassDrift,
	OutlineExhausted,
	MissingSummaries,
	// Context
	GhostCharacter,
	TimelineGaps,
	RelationshipStagnation,
}

// Analyze là điểm vào duy nhất của hệ thống chẩn đoán.
func Analyze(s *store.Store) Report {
	snap := Load(s)

	var findings []Finding
	for _, e := range snap.LoadErrors {
		findings = append(findings, Finding{
			Rule:       "LoadError",
			Category:   CatFlow,
			Severity:   SevWarning,
			Confidence: ConfHigh,
			AutoLevel:  AutoNone,
			Target:     "runtime.flow",
			Title:      fmt.Sprintf(contentlang.Pick("工件加载失败: %s", "Tải tạo phẩm thất bại: %s"), e),
			Suggestion: contentlang.Pick("文件可能损坏或权限不足，相关诊断规则的结果可能不完整。", "Tệp có thể hỏng hoặc thiếu quyền truy cập, kết quả của các quy tắc chẩn đoán liên quan có thể không đầy đủ."),
		})
	}
	for _, rule := range allRules {
		findings = append(findings, rule(&snap)...)
	}
	sortFindings(findings)

	return Report{
		Stats:    buildStats(&snap),
		Findings: findings,
		Actions:  PlanActions(findings),
	}
}

func buildStats(snap *Snapshot) Stats {
	st := Stats{}
	if snap.Progress == nil {
		return st
	}
	p := snap.Progress
	st.CompletedChapters = len(p.CompletedChapters)
	st.TotalChapters = p.TotalChapters
	st.TotalWords = p.TotalWordCount
	st.Phase = string(p.Phase)
	st.Flow = string(p.Flow)

	if st.CompletedChapters > 0 {
		st.AvgWordsPerCh = st.TotalWords / st.CompletedChapters
	}

	if snap.RunMeta != nil {
		st.PlanningTier = string(snap.RunMeta.PlanningTier)
	}

	// Thống kê rà soát
	st.ReviewCount = len(snap.Reviews)
	var totalScore float64
	var dimCount int
	for _, r := range snap.Reviews {
		if r.Verdict == "rewrite" {
			st.RewriteCount++
		}
		for _, d := range r.Dimensions {
			totalScore += float64(d.Score)
			dimCount++
		}
	}
	if dimCount > 0 {
		st.AvgReviewScore = totalScore / float64(dimCount)
	}

	// Thống kê phục bút
	latest := snap.LatestCompleted()
	for _, f := range snap.Foreshadow {
		if f.Status == "planted" || f.Status == "advanced" {
			st.ForeshadowOpen++
			if f.Status == "planted" && latest-f.PlantedAt > staleForeshadowThreshold(st.CompletedChapters) {
				st.ForeshadowStale++
			}
		}
	}
	return st
}

// sortFindings sắp xếp theo mức nghiêm trọng: critical > warning > info.
func sortFindings(findings []Finding) {
	order := map[Severity]int{SevCritical: 0, SevWarning: 1, SevInfo: 2}
	sort.SliceStable(findings, func(i, j int) bool {
		return order[findings[i].Severity] < order[findings[j].Severity]
	})
}

// staleForeshadowThreshold tính ngưỡng đình trệ phục bút dựa trên tổng số chương.
func staleForeshadowThreshold(completedChapters int) int {
	t := completedChapters / 3
	if t < ThresholdForeshadowMin {
		return ThresholdForeshadowMin
	}
	return t
}
