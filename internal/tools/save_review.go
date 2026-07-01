package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SaveReviewTool lưu kết quả rà soát của Editor.
type SaveReviewTool struct {
	store *store.Store
}

func NewSaveReviewTool(store *store.Store) *SaveReviewTool {
	return &SaveReviewTool{store: store}
}

func (t *SaveReviewTool) Name() string { return "save_review" }
func (t *SaveReviewTool) Description() string {
	return contentlang.Pick(
		"保存审阅结果并更新流程状态。verdict 为 accept/polish/rewrite 之一。"+
			"工具内部执行评分卡门禁（可能升级 verdict），直接更新 Progress 的 flow 和 pending_rewrites。"+
			"返回结构化事实：final_verdict / affected_chapters / escalation_reason / next_flow / next_chapter",
		"Lưu kết quả thẩm định và cập nhật trạng thái quy trình. verdict là một trong accept/polish/rewrite. "+
			"Tool nội bộ thực thi cổng kiểm tra phiếu chấm điểm (có thể nâng cấp verdict), cập nhật trực tiếp flow và pending_rewrites của Progress. "+
			"Trả về sự kiện có cấu trúc: final_verdict / affected_chapters / escalation_reason / next_flow / next_chapter",
	)
}
func (t *SaveReviewTool) Label() string { return i18n.T("ui.tool.save_review.label") }

// Tool ghi (đồng thời cập nhật reviews/ và PendingRewrites/Flow của Progress), cấm song song.
func (t *SaveReviewTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *SaveReviewTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *SaveReviewTool) Schema() map[string]any {
	issueSchema := schema.Object(
		schema.Property("type", schema.Enum(contentlang.Pick("问题维度", "Chiều vấn đề"), "consistency", "character", "pacing", "continuity", "foreshadow", "hook", "aesthetic")).Required(),
		schema.Property("severity", schema.Enum(contentlang.Pick("严重程度", "Mức độ nghiêm trọng"), "critical", "error", "warning")).Required(),
		schema.Property("description", schema.String(contentlang.Pick("问题描述", "Mô tả vấn đề"))).Required(),
		schema.Property("evidence", schema.String(contentlang.Pick("证据：原文片段、具体情节或状态数据", "Bằng chứng: đoạn nguyên văn, tình tiết cụ thể hoặc dữ liệu trạng thái"))).Required(),
		schema.Property("suggestion", schema.String(contentlang.Pick("修改建议", "Đề xuất chỉnh sửa"))),
	)
	dimensionSchema := schema.Object(
		schema.Property("dimension", schema.Enum(contentlang.Pick("维度", "Chiều"), "consistency", "character", "pacing", "continuity", "foreshadow", "hook", "aesthetic")).Required(),
		schema.Property("score", schema.Int(contentlang.Pick("评分（0-100）", "Điểm số (0-100)"))).Required(),
		schema.Property("verdict", schema.Enum(contentlang.Pick("维度结论（可省略：系统按 score 自动推导，≥80 pass / ≥60 warning / <60 fail）", "Kết luận chiều (có thể bỏ qua: hệ thống tự suy theo score, ≥80 pass / ≥60 warning / <60 fail)"), "pass", "warning", "fail")),
		schema.Property("comment", schema.String(contentlang.Pick("该维度的简要结论；每个维度必填，aesthetic 必须引用原文或具体统计事实", "Kết luận ngắn gọn của chiều này; mỗi chiều bắt buộc, aesthetic phải trích nguyên văn hoặc sự thật thống kê cụ thể"))).Required(),
	)
	return schema.Object(
		schema.Property("chapter", schema.Int(contentlang.Pick("审阅的章节号（全局审阅填最新章节号）", "Số chương thẩm định (thẩm định toàn cục điền số chương mới nhất)"))).Required(),
		schema.Property("scope", schema.Enum(contentlang.Pick("审阅范围", "Phạm vi thẩm định"), "chapter", "global", "arc")).Required(),
		schema.Property("dimensions", schema.Array(contentlang.Pick("分维度评分（七个维度各一条）", "Chấm điểm theo từng chiều (mỗi chiều một mục, bảy chiều)"), dimensionSchema)).Required(),
		schema.Property("issues", schema.Array(contentlang.Pick("发现的问题", "Vấn đề phát hiện"), issueSchema)).Required(),
		schema.Property("contract_status", schema.Enum(contentlang.Pick("章节契约完成度", "Mức độ hoàn thành khế ước chương"), "met", "partial", "missed")),
		schema.Property("contract_misses", schema.Array(contentlang.Pick("未完成或违背的 contract 条目", "Các mục contract chưa hoàn thành hoặc vi phạm"), schema.String(""))),
		schema.Property("contract_notes", schema.String(contentlang.Pick("对 contract 履行情况的简要说明", "Giải thích ngắn gọn về tình hình thực hiện contract"))),
		schema.Property("verdict", schema.Enum(contentlang.Pick("审阅结论", "Kết luận thẩm định"), "accept", "polish", "rewrite")).Required(),
		schema.Property("summary", schema.String(contentlang.Pick("审阅总结", "Tổng kết thẩm định"))).Required(),
		schema.Property("affected_chapters", schema.Array(contentlang.Pick("需要重写或打磨的章节号列表（verdict 为 polish/rewrite 时必填）", "Danh sách số chương cần viết lại hoặc trau chuốt (bắt buộc khi verdict là polish/rewrite)"), schema.Int(""))),
	)
}

func (t *SaveReviewTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var r domain.ReviewEntry
	if err := json.Unmarshal(args, &r); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if r.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0")
	}
	// verdict là hàm thuần của score (≥80 pass / ≥60 warning / <60 fail), suy ra một cách xác định từ code —
	// không để LLM cung cấp lại rồi phải kiểm tra nhất quán. Vừa loại bỏ dư thừa, vừa triệt tiêu loại tham số tự mâu thuẫn kiểu "score=85 nhưng cho warning".
	for i := range r.Dimensions {
		r.Dimensions[i].Verdict = expectedDimensionVerdict(r.Dimensions[i].Score)
	}
	if err := validateReviewEntry(r); err != nil {
		return nil, err
	}

	// Cổng kiểm soát thẻ điểm — nội tuyến logic nâng cấp gốc của policy/review.go
	finalVerdict := r.Verdict
	var escalationReason string

	if r.Verdict == "accept" {
		// Kiểm tra trạng thái hợp đồng
		if r.ContractStatus == "missed" {
			finalVerdict = "rewrite"
			escalationReason = contentlang.Pick("合同履约状态为 missed，升级为重写", "Trạng thái thực thi hợp đồng là missed, nâng cấp thành viết lại")
		} else if r.ContractStatus == "partial" {
			finalVerdict = "polish"
			escalationReason = contentlang.Pick("合同履约状态为 partial，升级为打磨", "Trạng thái thực thi hợp đồng là partial, nâng cấp thành đánh bóng")
		}
		// Cổng kiểm soát thẻ điểm
		if finalVerdict == "accept" {
			if gate := evaluateScorecardGate(r.Dimensions); gate != "" {
				if strings.Contains(gate, "rewrite") {
					finalVerdict = "rewrite"
				} else {
					finalVerdict = "polish"
				}
				escalationReason = gate
			}
		}
	}

	affected := r.AffectedChapters
	if finalVerdict == "rewrite" || finalVerdict == "polish" {
		if len(affected) == 0 && r.Chapter > 0 {
			affected = []int{r.Chapter}
		}
		if err := t.store.Progress.ValidatePendingRewrites(affected); err != nil {
			return nil, fmt.Errorf("validate pending rewrites: %w", err)
		}
	}

	if err := t.store.World.SaveReview(r); err != nil {
		return nil, fmt.Errorf("save review: %w", err)
	}

	// Cập nhật Progress theo verdict cuối cùng.
	// Khi ghi thất bại phải trả về sớm — về sau sẽ append review checkpoint, nếu nuốt err ở đây sẽ khiến Coordinator
	// thấy saved:true nhưng Store vẫn ở trạng thái trung gian Flow cũ / thiếu PendingRewrites.
	progress, _ := t.store.Progress.Load()
	if finalVerdict == "rewrite" || finalVerdict == "polish" {
		flow := domain.FlowRewriting
		if finalVerdict == "polish" {
			flow = domain.FlowPolishing
		}
		if err := t.store.Progress.SetPendingRewrites(affected, r.Summary); err != nil {
			return nil, fmt.Errorf("set pending rewrites: %w", err)
		}
		if err := t.store.Progress.SetFlow(flow); err != nil {
			return nil, fmt.Errorf("set flow %s: %w", flow, err)
		}
	} else {
		if err := t.store.Progress.SetFlow(domain.FlowWriting); err != nil {
			return nil, fmt.Errorf("set flow writing: %w", err)
		}
	}

	// Đọc ảnh chụp Progress sau cập nhật làm sự kiện
	latest, _ := t.store.Progress.Load()
	nextFlow := string(domain.FlowWriting)
	nextChapter := 0
	if latest != nil {
		nextFlow = string(latest.Flow)
		nextChapter = latest.NextChapter()
	}

	// Thêm checkpoint
	scope := domain.ChapterScope(r.Chapter)
	if r.Scope == "arc" {
		vol, arc := 0, 0
		if progress != nil {
			vol, arc = progress.CurrentVolume, progress.CurrentArc
		}
		scope = domain.ArcScope(vol, arc)
	}
	artifact := fmt.Sprintf("reviews/%02d.json", r.Chapter)
	if r.Scope == "global" {
		artifact = fmt.Sprintf("reviews/%02d-global.json", r.Chapter)
	}
	if _, err := t.store.Checkpoints.AppendArtifact(scope, "review", artifact); err != nil {
		return nil, fmt.Errorf("checkpoint review: %w", err)
	}

	return json.Marshal(map[string]any{
		"saved":             true,
		"chapter":           r.Chapter,
		"scope":             r.Scope,
		"verdict":           r.Verdict,
		"final_verdict":     finalVerdict,
		"escalation_reason": escalationReason,
		"affected_chapters": affected,
		"issues":            len(r.Issues),
		"next_flow":         nextFlow,
		"next_chapter":      nextChapter,
	})
}

var expectedReviewDimensions = map[string]struct{}{
	"consistency": {},
	"character":   {},
	"pacing":      {},
	"continuity":  {},
	"foreshadow":  {},
	"hook":        {},
	"aesthetic":   {},
}

func validateReviewEntry(r domain.ReviewEntry) error {
	if strings.TrimSpace(r.Scope) == "" {
		return fmt.Errorf("scope is required")
	}
	if strings.TrimSpace(r.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	for _, issue := range r.Issues {
		if strings.TrimSpace(issue.Description) == "" {
			return fmt.Errorf("issue description is required")
		}
		if strings.TrimSpace(issue.Evidence) == "" {
			return fmt.Errorf("issue evidence is required")
		}
	}
	if err := validateDimensions(r.Dimensions); err != nil {
		return err
	}
	if (r.Verdict == "rewrite" || r.Verdict == "polish") && len(r.AffectedChapters) == 0 {
		return fmt.Errorf("affected_chapters is required when verdict=%s", r.Verdict)
	}
	return nil
}

func validateDimensions(dimensions []domain.DimensionScore) error {
	if len(dimensions) != len(expectedReviewDimensions) {
		return fmt.Errorf("dimensions must contain exactly %d entries", len(expectedReviewDimensions))
	}

	seen := make(map[string]struct{}, len(dimensions))
	for _, dim := range dimensions {
		if _, ok := expectedReviewDimensions[dim.Dimension]; !ok {
			return fmt.Errorf("unknown dimension: %s", dim.Dimension)
		}
		if _, ok := seen[dim.Dimension]; ok {
			return fmt.Errorf("duplicate dimension: %s", dim.Dimension)
		}
		seen[dim.Dimension] = struct{}{}
		if dim.Score < 0 || dim.Score > 100 {
			return fmt.Errorf("invalid score for %s: %d", dim.Dimension, dim.Score)
		}
		if strings.TrimSpace(dim.Comment) == "" {
			return fmt.Errorf("dimension comment is required: %s", dim.Dimension)
		}
	}
	return nil
}

func expectedDimensionVerdict(score int) string {
	switch {
	case score >= 80:
		return "pass"
	case score >= 60:
		return "warning"
	default:
		return "fail"
	}
}

// criticalDimensions định nghĩa các chiều then chốt sẽ kích hoạt việc nâng cấp verdict.
var criticalDimensions = map[string]struct{}{
	"consistency": {},
	"character":   {},
	"continuity":  {},
}

// evaluateScorecardGate kiểm tra thẻ điểm có cần nâng cấp verdict không.
// Trả về chuỗi rỗng nghĩa là không nâng cấp.
func evaluateScorecardGate(dimensions []domain.DimensionScore) string {
	var criticalFails []string
	var polishIssues []string

	for _, dim := range dimensions {
		_, isCritical := criticalDimensions[dim.Dimension]
		if isCritical && (dim.Verdict == "fail" || dim.Score < 60) {
			criticalFails = append(criticalFails, fmt.Sprintf("%s(%d)", dim.Dimension, dim.Score))
		} else if dim.Verdict == "warning" || (isCritical && dim.Score < 80) {
			polishIssues = append(polishIssues, fmt.Sprintf("%s(%d)", dim.Dimension, dim.Score))
		}
	}

	if len(criticalFails) > 0 {
		return contentlang.Pick(
			fmt.Sprintf("rewrite: 关键维度不合格 %v", criticalFails),
			fmt.Sprintf("rewrite: các chiều then chốt không đạt %v", criticalFails),
		)
	}
	if len(polishIssues) > 0 {
		return contentlang.Pick(
			fmt.Sprintf("polish: 部分维度需打磨 %v", polishIssues),
			fmt.Sprintf("polish: một số chiều cần đánh bóng %v", polishIssues),
		)
	}
	return ""
}
