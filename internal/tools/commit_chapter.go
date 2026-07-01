package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
)

// CommitChapterTool commit chương: nạp chính văn → lưu bản chốt → sinh tóm tắt → cập nhật trạng thái → cập nhật tiến độ.
type CommitChapterTool struct {
	store      *store.Store
	rulesOpts  rules.LoadOptions // tùy chọn; khi LoadOptions rỗng thì không sinh rule_violations
	outputLang string            // ngôn ngữ output; vi/en tắt các rule giả định văn bản CJK
}

func NewCommitChapterTool(store *store.Store) *CommitChapterTool {
	return &CommitChapterTool{store: store}
}

// WithOutputLang khai báo ngôn ngữ output truyện. Khi vi/en, các rule máy móc giả định
// văn bản CJK (non_cjk_fragments, chapter_words) bị lọc bỏ — xem filterCJKAssumingRules.
func (t *CommitChapterTool) WithOutputLang(lang string) *CommitChapterTool {
	t.outputLang = lang
	return t
}

// WithRules tiêm tùy chọn nạp quy tắc người dùng, để rule_violations kèm theo kết quả kiểm tra quy tắc người dùng.
// Khi không gọi phương thức này thì chỉ chạy Lint giới hạn nội tại (kiểm tra sót cơ chế, luôn bật).
func (t *CommitChapterTool) WithRules(opts rules.LoadOptions) *CommitChapterTool {
	t.rulesOpts = opts
	return t
}

// commitOutput nhúng các trường mở rộng lên trên domain.CommitResult, giữ package domain không phụ thuộc rules.
// Do trường nhúng được JSON marshaler nâng lên (promoted), kết quả tuần tự hóa tương đương cấu trúc phẳng.
type commitOutput struct {
	domain.CommitResult
	RuleViolations []rules.Violation `json:"rule_violations,omitempty"`
}

func (t *CommitChapterTool) Name() string { return "commit_chapter" }
func (t *CommitChapterTool) Description() string {
	return contentlang.Pick(
		"提交章节终稿。加载草稿正文保存为终稿，更新时间线、伏笔、关系、角色状态和进度。"+
			"返回结构化事实：next_chapter / review_required / arc_end / volume_end / needs_expansion / book_complete / flow 等",
		"Nộp bản cuối của chương. Nạp nội dung bản nháp lưu thành bản cuối, cập nhật dòng thời gian, phục bút, quan hệ, trạng thái nhân vật và tiến độ. "+
			"Trả về sự kiện có cấu trúc: next_chapter / review_required / arc_end / volume_end / needs_expansion / book_complete / flow v.v.",
	)
}
func (t *CommitChapterTool) Label() string { return i18n.T("ui.tool.commit_chapter.label") }

// Tool ghi (thao tác nguyên tử liên miền: bản nháp→bản chốt→tóm tắt→tiến độ→checkpoint), cấm song song.
func (t *CommitChapterTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *CommitChapterTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *CommitChapterTool) Schema() map[string]any {
	timelineSchema := schema.Object(
		schema.Property("time", schema.String(contentlang.Pick("故事内时间", "Thời gian trong truyện"))).Required(),
		schema.Property("event", schema.String(contentlang.Pick("事件描述", "Mô tả sự kiện"))).Required(),
		schema.Property("characters", schema.Array(contentlang.Pick("涉及角色", "Nhân vật liên quan"), schema.String(""))),
	)
	foreshadowSchema := schema.Object(
		schema.Property("id", schema.String(contentlang.Pick("伏笔 ID", "ID phục bút"))).Required(),
		schema.Property("action", schema.Enum(contentlang.Pick("操作", "Thao tác"), "plant", "advance", "resolve")).Required(),
		schema.Property("description", schema.String(contentlang.Pick("伏笔描述（仅 plant 时必需）", "Mô tả phục bút (chỉ bắt buộc khi plant)"))),
	)
	relationshipSchema := schema.Object(
		schema.Property("character_a", schema.String(contentlang.Pick("角色 A", "Nhân vật A"))).Required(),
		schema.Property("character_b", schema.String(contentlang.Pick("角色 B", "Nhân vật B"))).Required(),
		schema.Property("relation", schema.String(contentlang.Pick("当前关系描述", "Mô tả quan hệ hiện tại"))).Required(),
	)
	stateChangeSchema := schema.Object(
		schema.Property("entity", schema.String(contentlang.Pick("角色名或实体名", "Tên nhân vật hoặc tên thực thể"))).Required(),
		schema.Property("field", schema.String(contentlang.Pick("变化属性", "Thuộc tính thay đổi"))).Required(),
		schema.Property("old_value", schema.String(contentlang.Pick("变化前的值", "Giá trị trước khi thay đổi"))),
		schema.Property("new_value", schema.String(contentlang.Pick("变化后的值", "Giá trị sau khi thay đổi"))).Required(),
		schema.Property("reason", schema.String(contentlang.Pick("变化原因", "Lý do thay đổi"))),
	)
	feedbackSchema := schema.Object(
		schema.Property("deviation", schema.String(contentlang.Pick("偏离大纲的描述", "Mô tả sai lệch so với dàn ý"))).Required(),
		schema.Property("suggestion", schema.String(contentlang.Pick("对后续大纲的调整建议", "Đề xuất điều chỉnh dàn ý phía sau"))).Required(),
	)
	return schema.Object(
		schema.Property("chapter", schema.Int(contentlang.Pick("章节号", "Số chương"))).Required(),
		schema.Property("summary", schema.String(contentlang.Pick("本章内容摘要（200字以内）", "Tóm tắt nội dung chương này (trong 200 chữ)"))).Required(),
		schema.Property("characters", schema.Array(contentlang.Pick("本章出场角色名", "Tên nhân vật xuất hiện trong chương này"), schema.String(""))).Required(),
		schema.Property("key_events", schema.Array(contentlang.Pick("本章关键事件", "Sự kiện then chốt của chương này"), schema.String(""))).Required(),
		schema.Property("timeline_events", schema.Array(contentlang.Pick("本章时间线事件", "Sự kiện dòng thời gian của chương này"), timelineSchema)),
		schema.Property("foreshadow_updates", schema.Array(contentlang.Pick("伏笔操作", "Thao tác phục bút"), foreshadowSchema)),
		schema.Property("relationship_changes", schema.Array(contentlang.Pick("关系变化", "Thay đổi quan hệ"), relationshipSchema)),
		schema.Property("state_changes", schema.Array(contentlang.Pick("角色/实体状态变化", "Thay đổi trạng thái nhân vật/thực thể"), stateChangeSchema)),
		schema.Property("cast_intros", schema.Array(contentlang.Pick("本章首次引入且后续可能再出现的次要角色简介（不含主角及 characters.json 已有角色）", "Giới thiệu nhân vật phụ lần đầu xuất hiện trong chương này và có thể xuất hiện lại sau (không gồm nhân vật chính và nhân vật đã có trong characters.json)"), schema.Object(
			schema.Property("name", schema.String(contentlang.Pick("角色名", "Tên nhân vật"))).Required(),
			schema.Property("brief_role", schema.String(contentlang.Pick("一句话定位（如：客栈老板/赌坊打手）", "Định vị một câu (ví dụ: chủ quán trọ/tay đấm sòng bạc)"))).Required(),
		))),
		schema.Property("hook_type", schema.Enum(contentlang.Pick("章末钩子类型", "Loại hook cuối chương"), "crisis", "mystery", "desire", "emotion", "choice")),
		schema.Property("dominant_strand", schema.Enum(contentlang.Pick("本章主导叙事线", "Tuyến tự sự chủ đạo của chương này"), "quest", "fire", "constellation")),
		schema.Property("feedback", feedbackSchema),
	)
}

func (t *CommitChapterTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter             int                        `json:"chapter"`
		Summary             string                     `json:"summary"`
		Characters          []string                   `json:"characters"`
		KeyEvents           []string                   `json:"key_events"`
		TimelineEvents      []domain.TimelineEvent     `json:"timeline_events"`
		ForeshadowUpdates   []domain.ForeshadowUpdate  `json:"foreshadow_updates"`
		RelationshipChanges []domain.RelationshipEntry `json:"relationship_changes"`
		StateChanges        []domain.StateChange       `json:"state_changes"`
		CastIntros          []domain.CastIntro         `json:"cast_intros"`
		HookType            string                     `json:"hook_type"`
		DominantStrand      string                     `json:"dominant_strand"`
		Feedback            *domain.OutlineFeedback    `json:"feedback"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if a.Chapter <= 0 {
		return nil, fmt.Errorf("chapter must be > 0: %w", errs.ErrToolArgs)
	}
	if t.store.Progress.IsChapterCompleted(a.Chapter) {
		// Dọn PendingCommit có thể còn sót (sự cố xảy ra sau ProgressMarked, trước ClearPendingCommit)
		if pending, _ := t.store.Signals.LoadPendingCommit(); pending != nil && pending.Chapter == a.Chapter {
			_ = t.store.Signals.ClearPendingCommit()
		}
		// Đường đi mài giũa/viết lại: chương tuy đã hoàn thành nhưng vẫn nằm trong pending_rewrites, cho phép ghi đè và drain hàng đợi
		progress, _ := t.store.Progress.Load()
		if progress != nil && slices.Contains(progress.PendingRewrites, a.Chapter) {
			return t.executeRewriteCommit(a.Chapter, a.Summary, a.Characters, a.KeyEvents,
				a.HookType, a.DominantStrand, progress)
		}
		return t.buildSkipResult(a.Chapter, progress)
	}
	existingPending, err := t.store.Signals.LoadPendingCommit()
	if err != nil {
		return nil, fmt.Errorf("load pending commit: %w: %w", errs.ErrStoreRead, err)
	}
	if existingPending != nil && existingPending.Chapter != a.Chapter {
		return nil, fmt.Errorf(i18n.T("error.tool.commit.pending_unrecovered"), existingPending.Chapter, existingPending.Stage, errs.ErrToolConflict)
	}
	if err := t.store.Progress.ValidateChapterWork(a.Chapter); err != nil {
		// Xung đột hàng đợi giữ nguyên (đã có phân loại ErrToolConflict); các lỗi IO khác quy về Precondition.
		if errors.Is(err, errs.ErrToolConflict) {
			return nil, err
		}
		return nil, fmt.Errorf(i18n.T("error.tool.commit.not_allowed"), errs.ErrToolPrecondition, err)
	}

	// Chặn vượt biên trong chế độ phân tầng: phải đặt trước mọi thao tác ghi, nếu không commit vượt biên sẽ làm hỏng file chương, tóm tắt,
	// và cả Progress. boundary được tái dùng cho bước 6b bên dưới để tính tín hiệu cung truyện/quyển.
	var boundary *store.ArcBoundary
	if progress, perr := t.store.Progress.Load(); perr == nil && progress != nil && progress.Layered {
		b, bErr := t.store.Outline.CheckArcBoundary(a.Chapter)
		if bErr != nil {
			return nil, fmt.Errorf(i18n.T("error.tool.commit.arc_boundary_failed"), a.Chapter, errs.ErrStoreRead, bErr)
		}
		if b == nil {
			return nil, fmt.Errorf("%s: %w", contentlang.Pick(
				fmt.Sprintf("第 %d 章不在分层大纲范围内：写作必须先 expand_arc 扩展弧或 append_volume 追加卷；若全书已完结请调 save_foundation type=complete_book", a.Chapter),
				fmt.Sprintf("Chương %d không nằm trong phạm vi dàn ý phân tầng: viết phải expand_arc mở rộng cung hoặc append_volume thêm quyển trước; nếu toàn bộ truyện đã hoàn kết hãy gọi save_foundation type=complete_book", a.Chapter),
			), errs.ErrToolPrecondition)
		}
		boundary = b
	}

	// 1. Nạp chính văn chương
	content, wordCount, err := t.store.Drafts.LoadChapterContent(a.Chapter)
	if err != nil {
		return nil, fmt.Errorf("load chapter content: %w: %w", errs.ErrStoreRead, err)
	}
	if content == "" {
		return nil, fmt.Errorf("no content found for chapter %d: %w", a.Chapter, errs.ErrToolPrecondition)
	}

	now := time.Now().Format(time.RFC3339)
	pending := domain.PendingCommit{
		Chapter:        a.Chapter,
		Stage:          domain.CommitStageStarted,
		Summary:        a.Summary,
		HookType:       a.HookType,
		DominantStrand: a.DominantStrand,
		StartedAt:      now,
		UpdatedAt:      now,
	}
	if err := t.store.Signals.SavePendingCommit(pending); err != nil {
		return nil, fmt.Errorf("save pending commit: %w: %w", errs.ErrStoreWrite, err)
	}

	// 2. Lưu bản chốt
	if err := t.store.Drafts.SaveFinalChapter(a.Chapter, content); err != nil {
		return nil, fmt.Errorf("save final chapter: %w: %w", errs.ErrStoreWrite, err)
	}

	// 3. Lưu tóm tắt
	summary := domain.ChapterSummary{
		Chapter:    a.Chapter,
		Summary:    a.Summary,
		Characters: a.Characters,
		KeyEvents:  a.KeyEvents,
	}
	if err := t.store.Summaries.SaveSummary(summary); err != nil {
		return nil, fmt.Errorf("save summary: %w: %w", errs.ErrStoreWrite, err)
	}

	// 4. Cập nhật trạng thái tăng dần
	if len(a.TimelineEvents) > 0 {
		for i := range a.TimelineEvents {
			a.TimelineEvents[i].Chapter = a.Chapter
		}
		if err := t.store.World.AppendTimelineEvents(a.TimelineEvents); err != nil {
			return nil, fmt.Errorf("append timeline: %w: %w", errs.ErrStoreWrite, err)
		}
	}
	if len(a.ForeshadowUpdates) > 0 {
		if err := t.store.World.UpdateForeshadow(a.Chapter, a.ForeshadowUpdates); err != nil {
			return nil, fmt.Errorf("update foreshadow: %w: %w", errs.ErrStoreWrite, err)
		}
	}
	if len(a.RelationshipChanges) > 0 {
		for i := range a.RelationshipChanges {
			a.RelationshipChanges[i].Chapter = a.Chapter
		}
		if err := t.store.World.UpdateRelationships(a.RelationshipChanges); err != nil {
			return nil, fmt.Errorf("update relationships: %w: %w", errs.ErrStoreWrite, err)
		}
	}
	if len(a.StateChanges) > 0 {
		for i := range a.StateChanges {
			a.StateChanges[i].Chapter = a.Chapter
		}
		if err := t.store.World.AppendStateChanges(a.StateChanges); err != nil {
			return nil, fmt.Errorf("append state changes: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	// 4b. Cộng dồn danh bạ nhân vật phụ: các nhân vật không cốt lõi xuất hiện trong chương này vào cast_ledger, để novel_context triệu hồi.
	// Khi thất bại chỉ warn chứ không chặn commit — danh bạ là dữ liệu thứ yếu, có thể tự lành qua commit chương sau.
	if len(a.Characters) > 0 {
		coreNames := loadCoreCharacterNameSet(t.store)
		if err := t.store.Cast.MergeAppearances(a.Chapter, a.Characters, a.CastIntros, coreNames); err != nil {
			slog.Warn(i18n.T("log.tool.cast_accumulate_failed"), "module", "commit", "chapter", a.Chapter, "err", err)
		}
	}

	pending.Stage = domain.CommitStageStateApplied
	pending.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := t.store.Signals.SavePendingCommit(pending); err != nil {
		return nil, fmt.Errorf("update pending commit stage: %w: %w", errs.ErrStoreWrite, err)
	}

	// 5. Cập nhật tiến độ
	if err := t.store.Progress.MarkChapterComplete(a.Chapter, wordCount, a.HookType, a.DominantStrand); err != nil {
		return nil, fmt.Errorf("mark chapter complete: %w: %w", errs.ErrStoreWrite, err)
	}

	// 6. Phán định có cần rà soát không
	progress, err := t.store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	completedCount := 0
	if progress != nil {
		completedCount = len(progress.CompletedChapters)
	}

	// 6b. Tín hiệu cung truyện/quyển ở chế độ truyện dài: boundary đã được kiểm tra trước ở lối vào, khi Layered đảm bảo khác nil
	var arcEnd, volumeEnd, needsExpansion, needsNewVolume bool
	var vol, arc, nextVol, nextArc int
	if progress != nil && progress.Layered && boundary != nil {
		arcEnd = boundary.IsArcEnd
		volumeEnd = boundary.IsVolumeEnd
		vol = boundary.Volume
		arc = boundary.Arc
		needsExpansion = boundary.NeedsExpansion
		needsNewVolume = boundary.NeedsNewVolume
		nextVol = boundary.NextVolume
		nextArc = boundary.NextArc
		_ = t.store.Progress.UpdateVolumeArc(vol, arc)
	}

	var reviewRequired bool
	var reviewReason string
	if progress != nil && progress.Layered {
		reviewRequired, reviewReason = domain.ShouldArcReview(arcEnd, volumeEnd, vol, arc)
	} else {
		reviewRequired, reviewReason = domain.ShouldReview(completedCount)
	}

	// 7. Dựng tín hiệu có cấu trúc
	result := domain.CommitResult{
		Chapter:        a.Chapter,
		Committed:      true,
		WordCount:      wordCount,
		NextChapter:    a.Chapter + 1,
		ReviewRequired: reviewRequired,
		ReviewReason:   reviewReason,
		HookType:       a.HookType,
		DominantStrand: a.DominantStrand,
		Feedback:       a.Feedback,
		ArcEnd:         arcEnd,
		VolumeEnd:      volumeEnd,
		Volume:         vol,
		Arc:            arc,
		NeedsExpansion: needsExpansion,
		NeedsNewVolume: needsNewVolume,
		NextVolume:     nextVol,
		NextArc:        nextArc,
	}

	// 8. Phán định trạng thái hoàn thành: phi phân tầng viết xong chương cuối / phân tầng chương cuối của quyển cuối → MarkComplete
	if t.applyCompletion(&result, progress) {
		result.BookComplete = true
	}
	if p, _ := t.store.Progress.Load(); p != nil {
		result.Flow = string(p.Flow)
	}

	pending.Stage = domain.CommitStageProgressMarked
	pending.Result = &result
	pending.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := t.store.Signals.SavePendingCommit(pending); err != nil {
		return nil, fmt.Errorf("update pending commit result: %w: %w", errs.ErrStoreWrite, err)
	}

	// 9. Xóa trạng thái trung gian của tiến độ
	if err := t.store.Progress.ClearInProgress(); err != nil {
		return nil, fmt.Errorf("clear in-progress: %w: %w", errs.ErrStoreWrite, err)
	}
	if err := t.store.Signals.ClearPendingCommit(); err != nil {
		return nil, fmt.Errorf("clear pending commit: %w: %w", errs.ErrStoreWrite, err)
	}

	// 10. Thêm checkpoint
	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(a.Chapter), "commit",
		fmt.Sprintf("chapters/%02d.md", a.Chapter),
	); err != nil {
		return nil, fmt.Errorf("checkpoint commit: %w: %w", errs.ErrStoreWrite, err)
	}

	// 11. Kiểm tra quy tắc máy móc (chỉ trả về sự kiện, không chặn)
	violations := t.checkRules(content, wordCount)
	return json.Marshal(commitOutput{CommitResult: result, RuleViolations: violations})
}

// checkRules kiểm tra máy móc trên chính văn chương: Lint giới hạn sản phẩm nội tại (sót cơ chế, luôn chạy)
// + Check quy tắc người dùng (khi rulesOpts hoàn toàn rỗng thì loader trả về layers rỗng, checker trả nil).
func (t *CommitChapterTool) checkRules(text string, wordCount int) []rules.Violation {
	violations := rules.Lint(text)
	bundle := rules.Merge(rules.Load(t.rulesOpts))
	violations = append(violations, rules.Check(text, wordCount, bundle.Structured)...)
	return filterCJKAssumingRules(violations, t.outputLang)
}

// filterCJKAssumingRules loại các rule máy móc giả định văn bản CJK khi output là vi/en:
//   - non_cjk_fragments: coi đoạn chữ Latin liên tục là khiếm khuyết → bắn nhầm toàn bộ thân VI/EN.
//   - chapter_words: ngưỡng số chữ đếm theo rune, chỉ đúng cho CJK; range VI/EN do file rule
//     per-lang cung cấp sau (Wave 5 bước 2). Tới lúc đó, tắt để tránh tín hiệu sai.
//
// "original" (zh) và mọi giá trị khác giữ nguyên đầy đủ rule (hành vi gốc).
func filterCJKAssumingRules(vs []rules.Violation, outputLang string) []rules.Violation {
	if outputLang != "vi" && outputLang != "en" {
		return vs
	}
	out := vs[:0]
	for _, v := range vs {
		if v.Rule == "non_cjk_fragments" || v.Rule == "chapter_words" {
			continue
		}
		out = append(out, v)
	}
	return out
}

// executeRewriteCommit xử lý commit cho chương mài giũa/viết lại: ghi đè bản chốt và tóm tắt, cập nhật số chữ, drain hàng đợi.
// Bỏ qua mọi việc thêm trạng thái thế giới (timeline / foreshadow / relationship / state_changes) và phát hiện ranh giới cung truyện,
// những thứ này đã được áp dụng khi commit gốc của chương.
func (t *CommitChapterTool) executeRewriteCommit(
	chapter int,
	summary string,
	characters, keyEvents []string,
	hookType, dominantStrand string,
	progress *domain.Progress,
) (json.RawMessage, error) {
	// 1. Nạp chính văn sau khi mài giũa
	content, wordCount, err := t.store.Drafts.LoadChapterContent(chapter)
	if err != nil {
		return nil, fmt.Errorf("rewrite: load chapter content: %w: %w", errs.ErrStoreRead, err)
	}
	if content == "" {
		return nil, fmt.Errorf("no content found for chapter %d: %w", chapter, errs.ErrToolPrecondition)
	}

	// 2. Kiểm tra cứng: drafts giống hệt bản chốt hiện tại → phán định là chưa thật sự mài giũa/viết lại (writer đã bỏ qua draft_chapter)
	// Từ chối commit, buộc writer gọi draft_chapter(mode=write) ghi phiên bản mới trước.
	existingFinal, _ := t.store.Drafts.LoadChapterText(chapter)
	if existingFinal != "" && existingFinal == content {
		mode := i18n.T("error.tool.commit.mode_rewrite")
		if progress != nil && progress.Flow == domain.FlowPolishing {
			mode = i18n.T("error.tool.commit.mode_polish")
		}
		return nil, fmt.Errorf(i18n.T("error.tool.commit.no_change"),
			chapter, mode, chapter, mode, errs.ErrToolPrecondition)
	}

	// 3. Ghi đè bản chốt
	if err := t.store.Drafts.SaveFinalChapter(chapter, content); err != nil {
		return nil, fmt.Errorf("rewrite: save final chapter: %w: %w", errs.ErrStoreWrite, err)
	}

	// 3. Ghi đè tóm tắt
	if err := t.store.Summaries.SaveSummary(domain.ChapterSummary{
		Chapter:    chapter,
		Summary:    summary,
		Characters: characters,
		KeyEvents:  keyEvents,
	}); err != nil {
		return nil, fmt.Errorf("rewrite: save summary: %w: %w", errs.ErrStoreWrite, err)
	}

	// 4. Cập nhật số chữ (MarkChapterComplete là idempotent với chương đã hoàn thành: replaces word count, slice.Contains chống đưa vào hàng đợi trùng)
	if err := t.store.Progress.MarkChapterComplete(chapter, wordCount, hookType, dominantStrand); err != nil {
		return nil, fmt.Errorf("rewrite: update word count: %w: %w", errs.ErrStoreWrite, err)
	}

	// 5. Drain hàng đợi chờ xử lý; khi hàng đợi rỗng CompleteRewrite sẽ tự động chuyển flow về writing
	if err := t.store.Progress.CompleteRewrite(chapter); err != nil {
		return nil, fmt.Errorf("rewrite: complete rewrite: %w: %w", errs.ErrStoreWrite, err)
	}

	// 6. Checkpoint
	if _, err := t.store.Checkpoints.AppendArtifact(
		domain.ChapterScope(chapter), "commit",
		fmt.Sprintf("chapters/%02d.md", chapter),
	); err != nil {
		return nil, fmt.Errorf("rewrite: checkpoint commit: %w: %w", errs.ErrStoreWrite, err)
	}

	// 7. Đọc ảnh chụp Progress sau drain, trả về làm sự kiện
	mode := "rewrite"
	if progress.Flow == domain.FlowPolishing {
		mode = "polish"
	}
	latest, _ := t.store.Progress.Load()
	remaining := []int{}
	nextChapter := chapter + 1
	flow := string(domain.FlowWriting)
	if latest != nil {
		remaining = append(remaining, latest.PendingRewrites...)
		nextChapter = latest.NextChapter()
		flow = string(latest.Flow)
	}
	drained := len(remaining) == 0

	// Sau khi hàng đợi sạch mới phán định kết thúc: commit làm lại không đi qua đường chính applyCompletion, kết thúc chỉ có thể kích hoạt ở đây.
	//   - Phân tầng + viết xuôi: dùng layeredBookComplete cấp chất lượng (yêu cầu các sợi truyện thu lại), chưa thỏa thì nhường cho Architect.
	//   - Phân tầng + reopen làm lại (ReopenedFromComplete): làm lại chỉ sửa chương đã có, không tăng giảm cấu trúc, theo cấu trúc đầy đủ
	//     là kết thúc lại — nếu vì làm lại làm xáo trộn một sợi truyện mà kẹt ở writing, cuối quyển cuối sẽ rơi vào vòng lặp viết tiếp vượt biên.
	//   - Phi phân tầng: viết đủ TotalChapters là kết thúc (làm lại không tăng giảm số chương, vốn đã đủ).
	bookComplete := false
	if drained && latest != nil {
		reComplete := false
		switch {
		case latest.Layered && latest.ReopenedFromComplete:
			reComplete = t.layeredStructurallyComplete(latest)
		case latest.Layered:
			reComplete = t.layeredBookComplete(latest)
		default:
			reComplete = latest.TotalChapters > 0 && len(latest.CompletedChapters) >= latest.TotalChapters
		}
		if reComplete {
			if cerr := t.store.Progress.MarkComplete(); cerr == nil {
				bookComplete = true
				if p, _ := t.store.Progress.Load(); p != nil {
					flow = string(p.Flow)
				}
			}
		}
	}

	// Cùng đường chính: rewrite/polish cũng kiểm tra máy móc và kèm rule_violations
	violations := t.checkRules(content, wordCount)
	return json.Marshal(map[string]any{
		"chapter":         chapter,
		"rewritten":       true,
		"mode":            mode,
		"word_count":      wordCount,
		"remaining_queue": remaining,
		"queue_drained":   drained,
		"next_chapter":    nextChapter,
		"flow":            flow,
		"book_complete":   bookComplete,
		"rule_violations": violations,
	})
}

// buildSkipResult dựng sự kiện trả về tương đồng với commit bình thường cho "commit trùng của chương đã hoàn thành".
// Coordinator dựa vào đó ra quyết định tiếp theo (giao writer/editor/architect), mà không bị ảo giác vì nhận được gợi ý prose.
func (t *CommitChapterTool) buildSkipResult(chapter int, progress *domain.Progress) (json.RawMessage, error) {
	_, wordCount, _ := t.store.Drafts.LoadChapterContent(chapter)

	result := domain.CommitResult{
		Chapter:     chapter,
		Committed:   true,
		WordCount:   wordCount,
		NextChapter: chapter + 1,
	}

	if progress != nil && progress.Layered {
		if boundary, _ := t.store.Outline.CheckArcBoundary(chapter); boundary != nil {
			result.ArcEnd = boundary.IsArcEnd
			result.VolumeEnd = boundary.IsVolumeEnd
			result.Volume = boundary.Volume
			result.Arc = boundary.Arc
			result.NeedsExpansion = boundary.NeedsExpansion
			result.NeedsNewVolume = boundary.NeedsNewVolume
			result.NextVolume = boundary.NextVolume
			result.NextArc = boundary.NextArc
		}
		result.ReviewRequired, result.ReviewReason = domain.ShouldArcReview(result.ArcEnd, result.VolumeEnd, result.Volume, result.Arc)
	} else if progress != nil {
		result.ReviewRequired, result.ReviewReason = domain.ShouldReview(len(progress.CompletedChapters))
	}

	if progress != nil {
		if progress.Phase == domain.PhaseComplete {
			result.BookComplete = true
		}
		result.Flow = string(progress.Flow)
	}

	return json.Marshal(result)
}

// loadCoreCharacterNameSet nạp tập hợp tên nhân vật đã có trong characters.json (kèm bí danh).
// Dùng làm tập lọc "cốt lõi đã biết" của cast_ledger — nhân vật cốt lõi không vào danh bạ nhân vật phụ.
// Khi nạp thất bại trả về nil (lúc merge mọi characters đều vào ledger, chấp nhận được).
func loadCoreCharacterNameSet(s *store.Store) map[string]bool {
	chars, err := s.Characters.Load()
	if err != nil || len(chars) == 0 {
		return nil
	}
	set := make(map[string]bool, len(chars)*2)
	for _, c := range chars {
		if c.Name != "" {
			set[c.Name] = true
		}
		for _, alias := range c.Aliases {
			if alias != "" {
				set[alias] = true
			}
		}
	}
	return set
}

// applyCompletion phán định lần commit này có khiến toàn bộ sách kết thúc không, nếu có thì MarkComplete và trả về true.
//   - Phi phân tầng: viết xong tổng số chương đã quy ước là kết thúc.
//   - Phân tầng: Architect gọi tường minh save_foundation type=complete_book là đường chính; ở đây thêm một lớp
//     dự phòng xác định — khi toàn bộ sách đã khách quan thỏa điều kiện kết thúc (xem layeredBookComplete) thì tự động thu xếp.
//     Tránh việc model ở điểm cuối không append_volume cũng không complete_book, dẫn đến livelock "writer chạy trần vượt biên chương →
//     bộ canh vượt biên chặn → thử lại liên tục" (căn nguyên của vụ "Phàm Cốt" ch204..347).
func (t *CommitChapterTool) applyCompletion(result *domain.CommitResult, progress *domain.Progress) bool {
	if progress == nil {
		return false
	}
	if progress.Layered {
		if t.layeredBookComplete(progress) {
			_ = t.store.Progress.MarkComplete()
			return true
		}
		return false
	}
	if progress.TotalChapters > 0 && result.NextChapter > progress.TotalChapters {
		_ = t.store.Progress.MarkComplete()
		return true
	}
	return false
}

// layeredStructurallyComplete phán định truyện dài phân tầng có "viết xong về mặt cấu trúc" không: hàng đợi làm lại rỗng + không còn cung truyện khung chờ mở rộng
// + mọi chương đã mở rộng đều đã viết. Đây là sự kiện trạng thái cuối mang tính xác định, không bao gồm phán định ngữ nghĩa như phục bút/sợi truyện dài — dùng làm lưới an toàn "chống vòng lặp
// trạng thái cuối" (sau khi làm lại sạch thì dựa vào đó kết thúc lại).
func (t *CommitChapterTool) layeredStructurallyComplete(progress *domain.Progress) bool {
	// 1. Hàng đợi làm lại phải sạch
	if len(progress.PendingRewrites) > 0 {
		return false
	}
	volumes, err := t.store.Outline.LoadLayeredOutline()
	if err != nil || len(volumes) == 0 {
		return false
	}
	// 2. Không được còn cung truyện khung chờ mở rộng (trong kế hoạch vẫn còn nội dung phải viết)
	for i := range volumes {
		for j := range volumes[i].Arcs {
			if !volumes[i].Arcs[j].IsExpanded() {
				return false
			}
		}
	}
	// 3. Mọi chương đã mở rộng phải viết xong hết
	expanded := len(domain.FlattenOutline(volumes))
	return expanded > 0 && len(progress.CompletedChapters) >= expanded
}

// layeredBookComplete dùng sự kiện khách quan phán định truyện dài phân tầng đã thật sự viết xong chưa, đối chiếu vài mục
// định lượng được trong danh sách phán định kết thúc của architect-long.md + sự kiện cấu trúc. Trên nền cấu trúc đầy đủ còn yêu cầu phục bút về không, sợi truyện dài thu lại — chỉ cần một mục không thỏa
// là nhường cho Architect tiếp tục expand_arc / append_volume, tuyệt đối không tranh thu xếp khi truyện chưa viết xong. Khi không có compass thì bảo thủ
// phán là chưa kết thúc. Đây là phán định kết thúc "cấp chất lượng" của viết xuôi, nghiêm hơn layeredStructurallyComplete.
func (t *CommitChapterTool) layeredBookComplete(progress *domain.Progress) bool {
	if !t.layeredStructurallyComplete(progress) {
		return false
	}
	// 4. Phục bút đang hoạt động phải về không (lời hứa đã thực hiện)
	if active, aerr := t.store.World.LoadActiveForeshadow(); aerr != nil || len(active) > 0 {
		return false
	}
	// 5. Sợi truyện dài đang hoạt động của compass phải thu lại (không có compass / sợi dài chưa sạch đều trả lại cho Architect phán quyết)
	compass, cerr := t.store.Outline.LoadCompass()
	if cerr != nil || compass == nil || len(compass.OpenThreads) > 0 {
		return false
	}
	return true
}
