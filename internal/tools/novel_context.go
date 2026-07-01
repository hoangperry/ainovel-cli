package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
)

// References tài liệu tham khảo được nhúng.
type References struct {
	// V0
	ChapterGuide      string
	HookTechniques    string
	QualityChecklist  string
	OutlineTemplate   string
	CharacterTemplate string
	ChapterTemplate   string
	// V1
	Consistency      string
	ContentExpansion string
	DialogueWriting  string
	// V2
	StyleReference   string // tham khảo bổ sung về phong cách (có thể rỗng)
	LongformPlanning string // tham khảo lập kế hoạch truyện dài thông dụng
	Differentiation  string // tham khảo thiết kế khác biệt hóa thông dụng
	ArcTemplates     string // mẫu cung truyện theo thể loại (nạp theo style, có thể rỗng)
	AntiAITone       string // thư viện tiêu chí khử giọng AI (writer/editor dùng chung, tiêm xuyên suốt)
}

// ContextTool lắp ráp context cần thiết cho chương hiện tại.
type ContextTool struct {
	store     *store.Store
	refs      References
	style     string
	rulesOpts rules.LoadOptions
}

// NewContextTool tạo context tool. rulesOpts điều khiển nguồn nạp của user_rules;
// LoadOptions rỗng vẫn an toàn, loader sẽ bỏ qua mọi nguồn chưa cấu hình, user_rules tiêm vào Bundle rỗng.
func NewContextTool(store *store.Store, refs References, style string, rulesOpts rules.LoadOptions) *ContextTool {
	return &ContextTool{store: store, refs: refs, style: style, rulesOpts: rulesOpts}
}

func (t *ContextTool) Name() string { return "novel_context" }
func (t *ContextTool) Description() string {
	return contentlang.Pick(
		"获取小说当前状态和创作上下文。"+
			"不传 chapter：返回 progress_status（phase/flow/next_chapter/pending_rewrites 等进度字段）+ 基础设定，用于判断下一步该做什么。"+
			"传 chapter=N：额外返回该章的前情摘要、伏笔、角色状态、风格规则等写作上下文",
		"Lấy trạng thái hiện tại và bối cảnh sáng tác của tiểu thuyết. "+
			"Không truyền chapter: trả về progress_status (các trường tiến độ phase/flow/next_chapter/pending_rewrites...) + thiết lập nền, dùng để phán đoán nên làm gì tiếp. "+
			"Truyền chapter=N: trả thêm bối cảnh viết của chương đó như tóm tắt tiền truyện, phục bút, trạng thái nhân vật, quy tắc văn phong",
	)
}
func (t *ContextTool) Label() string { return i18n.T("ui.tool.context.label") }

// Tool chỉ đọc, có thể được điều phối song song.
func (t *ContextTool) ReadOnly(_ json.RawMessage) bool        { return true }
func (t *ContextTool) ConcurrencySafe(_ json.RawMessage) bool { return true }

func (t *ContextTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapter", schema.Int(contentlang.Pick("章节号。不传则返回进度状态和基础设定（Coordinator 用于判断下一步）；传入则额外返回该章的写作上下文（Writer 用）", "Số chương. Không truyền thì trả về trạng thái tiến độ và thiết lập cơ bản (Coordinator dùng để phán đoán bước tiếp theo); truyền vào thì trả về thêm ngữ cảnh viết của chương đó (Writer dùng)"))),
	)
}

func (t *ContextTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Chapter int `json:"chapter"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}

	result := make(map[string]any)
	var warnings []string
	seenWarnings := make(map[string]struct{})
	warn := func(scope string, err error) {
		if err == nil || os.IsNotExist(err) {
			return
		}
		msg := fmt.Sprintf(contentlang.Pick("%s 读取失败: %v", "%s đọc thất bại: %v"), scope, err)
		if _, ok := seenWarnings[msg]; ok {
			return
		}
		seenWarnings[msg] = struct{}{}
		warnings = append(warnings, msg)
	}

	if a.Chapter > 0 {
		// Đường đi Writer: nạp toàn bộ dữ liệu nền tảng + context chương
		t.buildBaseContext(result, warn)
		seed := newChapterContextEnvelope()
		state := t.prepareChapterContext(a.Chapter, &seed, warn)
		seed.apply(result)
		t.buildChapterContext(result, state, warn)
		// Chú thích ngữ nghĩa dữ liệu (chữa lỗi lặp lại tường thuật): episodic là ghi nhớ đã viết vào chính văn, không phải tư liệu chờ viết.
		// Chỉ gắn trong container, không vào mirror ở tầng đỉnh.
		if epi, ok := result["episodic_memory"].(map[string]any); ok && len(epi) > 0 {
			epi["_usage"] = contentlang.Pick("本容器为已写入正文的事实备忘（供一致性与衔接对照）；在新章正文中原样复述这些内容属于重复缺陷", "Container này là ghi nhớ sự thật đã viết vào chính văn (để đối chiếu tính nhất quán và liền mạch); sao chép nguyên văn nội dung này vào chính văn chương mới là lỗi lặp lại")
		}
	} else {
		// Đường đi Coordinator/Architect: chỉ trả về trạng thái + dữ liệu có cấu trúc, không nạp toàn bộ nguyên văn
		t.buildProgressStatus(result)
		t.buildArchitectContext(result, warn)
	}

	// Tiêm working_memory.user_rules (đường dẫn canonical). Đường đi Architect vốn không có working_memory,
	// buildUserRules sẽ tạo mới theo nhu cầu một container chỉ chứa user_rules. Khi rulesOpts rỗng thì Bundle là đối tượng rỗng,
	// nhưng vẫn xuất ra, tránh để LLM thấy user_rules=null mà đi vào nhánh ngoại lệ.
	if a.Chapter > 0 {
		t.buildSimulationProfile(result, "working_memory", warn)
	} else {
		t.buildSimulationProfile(result, "planning_memory", warn)
	}

	t.buildUserRules(result)
	t.buildUserDirectives(result, warn)

	if len(warnings) > 0 {
		result["_warnings"] = warnings
	}

	// Ngân sách ưu tiên: khi tổng kích thước vượt ngưỡng thì tự động cắt bớt dữ liệu ưu tiên thấp
	if a.Chapter > 0 {
		trimByBudget(result, 100*1024) // Writer: 100KB
	} else {
		trimByBudget(result, 60*1024) // Coordinator/Architect: 60KB
	}

	result["_loading_summary"] = buildLoadingSummary(result, a.Chapter)
	return json.Marshal(result)
}

// buildLoadingSummary thống kê lượng dữ liệu từng mục trong result đã lắp ráp, tạo một dòng tóm tắt dễ đọc.
func buildLoadingSummary(result map[string]any, chapter int) string {
	var parts []string

	if chapter > 0 {
		parts = append(parts, fmt.Sprintf("ch=%d", chapter))
	} else {
		parts = append(parts, "architect")
	}
	if tier, ok := result["planning_tier"].(domain.PlanningTier); ok && tier != "" {
		parts = append(parts, fmt.Sprintf("tier=%s", tier))
	}

	// Vị trí quyển/cung truyện
	if pos, ok := result["position"].(map[string]any); ok {
		parts = append(parts, fmt.Sprintf("V%dA%d", pos["volume"], pos["arc"]))
	}

	var items []string
	countSlice := func(key string) int {
		if v, ok := result[key]; ok {
			if s, ok := v.([]domain.Character); ok {
				return len(s)
			}
			// Phản chiếu (reflect) slice thông dụng
			return sliceLen(v)
		}
		return 0
	}

	// Nhân vật
	if n := countSlice("character_snapshots"); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("角色:%d(快照)", "Nhân vật:%d(ảnh chụp)"), n))
	} else if n := countSlice("characters"); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("角色:%d", "Nhân vật:%d"), n))
	}

	if working, ok := result["working_memory"].(map[string]any); ok && len(working) > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("工作记忆:%d", "Bộ nhớ làm việc:%d"), len(working)))
	}
	if episodic, ok := result["episodic_memory"].(map[string]any); ok && len(episodic) > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("情节记忆:%d", "Bộ nhớ tình tiết:%d"), len(episodic)))
	}
	if planning, ok := result["planning_memory"].(map[string]any); ok && len(planning) > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("规划记忆:%d", "Bộ nhớ quy hoạch:%d"), len(planning)))
	}
	if foundation, ok := result["foundation_memory"].(map[string]any); ok && len(foundation) > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("基础记忆:%d", "Bộ nhớ nền tảng:%d"), len(foundation)))
	}

	// Tóm tắt phân tầng
	if n := countSlice("volume_summaries"); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("卷摘要:%d", "Tóm tắt quyển:%d"), n))
	}
	if n := countSlice("arc_summaries"); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("弧摘要:%d", "Tóm tắt cung:%d"), n))
	}
	if n := countSlice("recent_summaries"); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("章摘要:%d", "Tóm tắt chương:%d"), n))
	}

	// Dàn ý phân tầng
	if n := countSlice("layered_outline"); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("分层大纲:%d卷", "Dàn ý phân tầng:%d quyển"), n))
	}

	// Dữ liệu trạng thái
	if n := countSlice("timeline"); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("时间线:%d", "Dòng thời gian:%d"), n))
	}
	if n := countSlice("foreshadow_ledger"); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("伏笔:%d", "Phục bút:%d"), n))
	}
	if n := countSlice("relationship_state"); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("关系:%d", "Quan hệ:%d"), n))
	}
	if n := countSlice("recent_state_changes"); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("状态变化:%d", "Thay đổi trạng thái:%d"), n))
	}
	if _, ok := result["previous_tail"]; ok {
		items = append(items, contentlang.Pick("前章尾部:ok", "Đuôi chương trước:ok"))
	}
	if _, ok := result["style_rules"]; ok {
		items = append(items, contentlang.Pick("风格规则:ok", "Quy tắc văn phong:ok"))
	}
	if n := sliceLen(result["related_chapters"]); n > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("相关章:%d", "Chương liên quan:%d"), n))
	}
	if selected, ok := result["selected_memory"].(map[string]any); ok && len(selected) > 0 {
		if n := sliceLen(selected["story_threads"]); n > 0 {
			items = append(items, fmt.Sprintf(contentlang.Pick("线索召回:%d", "Triệu hồi manh mối:%d"), n))
		}
		if n := sliceLen(selected["review_lessons"]); n > 0 {
			items = append(items, fmt.Sprintf(contentlang.Pick("评审召回:%d", "Triệu hồi rà soát:%d"), n))
		}
	}

	// Tài liệu tham khảo
	if refs, ok := result["references"].(map[string]string); ok && len(refs) > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("参考:%d项", "Tham khảo:%d mục"), len(refs)))
	}
	if pack, ok := result["reference_pack"].(map[string]any); ok && len(pack) > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("参考包:%d", "Gói tham khảo:%d"), len(pack)))
	}
	if _, ok := result["memory_policy"]; ok {
		items = append(items, contentlang.Pick("记忆策略:ok", "Chiến lược ghi nhớ:ok"))
	}
	if _, ok := result["simulation_profile"]; ok {
		items = append(items, contentlang.Pick("仿写画像:ok", "Chân dung phỏng tác:ok"))
	}
	if warnings, ok := result["_warnings"].([]string); ok && len(warnings) > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("告警:%d", "Cảnh báo:%d"), len(warnings)))
	}
	if trimmed, ok := result["_trimmed"].([]string); ok && len(trimmed) > 0 {
		items = append(items, fmt.Sprintf(contentlang.Pick("裁剪:%s", "Cắt bớt:%s"), strings.Join(trimmed, ",")))
	}

	if len(items) > 0 {
		parts = append(parts, strings.Join(items, " "))
	}
	return strings.Join(parts, " | ")
}

// sliceLen thử lấy độ dài slice đối với kiểu any.
func sliceLen(v any) int {
	switch s := v.(type) {
	case []domain.ChapterSummary:
		return len(s)
	case []domain.ArcSummary:
		return len(s)
	case []domain.VolumeSummary:
		return len(s)
	case []domain.CharacterSnapshot:
		return len(s)
	case []domain.TimelineEvent:
		return len(s)
	case []domain.ForeshadowEntry:
		return len(s)
	case []domain.RelationshipEntry:
		return len(s)
	case []domain.StateChange:
		return len(s)
	case []domain.VolumeOutline:
		return len(s)
	case []domain.Character:
		return len(s)
	case []domain.RelatedChapter:
		return len(s)
	case []domain.RecallItem:
		return len(s)
	default:
		return 0
	}
}

// loadFilteredCharacters lọc nhân vật theo Tier và sự xuất hiện trong cảnh.
// core/important luôn trả về; secondary/decorative chỉ trả về khi được nhắc trong dàn ý chương hiện tại.
func (t *ContextTool) loadFilteredCharacters(result map[string]any, chapter int, warn func(string, error)) {
	chars, err := t.store.Characters.Load()
	if err != nil {
		warn("characters", err)
		return
	}
	if len(chars) == 0 {
		return
	}

	// Lấy mô tả cảnh trong dàn ý chương hiện tại, dùng để khớp nhân vật phụ
	entry, err := t.store.Outline.GetChapterOutline(chapter)
	if err != nil {
		warn("current_chapter_outline", err)
		result["characters"] = chars
		return
	}
	sceneText := strings.Join(entry.Scenes, " ") + " " + entry.CoreEvent + " " + entry.Title

	var filtered []domain.Character
	for _, c := range chars {
		switch c.Tier {
		case "secondary", "decorative":
			if matchCharacter(sceneText, c) {
				filtered = append(filtered, c)
			}
		default: // core, important, hoặc chưa đặt
			filtered = append(filtered, c)
		}
	}
	result["characters"] = filtered
}

// matchCharacter kiểm tra văn bản cảnh có chứa tên chính thức hay bất kỳ bí danh nào của nhân vật không.
func matchCharacter(text string, c domain.Character) bool {
	if strings.Contains(text, c.Name) {
		return true
	}
	for _, alias := range c.Aliases {
		if strings.Contains(text, alias) {
			return true
		}
	}
	return false
}

// loadLayeredSummaries nạp tóm tắt phân tầng: tóm tắt quyển + tóm tắt cung truyện của quyển hiện tại + tóm tắt chương trong cung truyện.
func (t *ContextTool) loadLayeredSummaries(result map[string]any, chapter, summaryWindow int, warn func(string, error)) {
	vol, arc, err := t.store.Outline.LocateChapter(chapter)
	if err != nil {
		warn("layered_outline_position", err)
		// Lùi về chế độ phẳng
		if summaries, err := t.store.Summaries.LoadRecentSummaries(chapter, summaryWindow); err == nil && len(summaries) > 0 {
			result["recent_summaries"] = summaries
		} else {
			warn("recent_summaries", err)
		}
		return
	}

	// 1. Tóm tắt quyển của các quyển đã hoàn thành
	if volSummaries, err := t.store.Summaries.LoadAllVolumeSummaries(); err == nil && len(volSummaries) > 0 {
		result["volume_summaries"] = volSummaries
	} else {
		warn("volume_summaries", err)
	}

	// 2. Tóm tắt cung truyện của các cung đã hoàn thành trong quyển hiện tại (không gồm cung hiện tại)
	if arcSummaries, err := t.store.Summaries.LoadArcSummaries(vol); err == nil && len(arcSummaries) > 0 {
		var prior []domain.ArcSummary
		for _, s := range arcSummaries {
			if s.Arc < arc {
				prior = append(prior, s)
			}
		}
		if len(prior) > 0 {
			result["arc_summaries"] = prior
		}
	} else {
		warn("arc_summaries", err)
	}

	// 3. Tóm tắt chương của N chương gần nhất trong cung truyện hiện tại
	if summaries, err := t.store.Summaries.LoadRecentSummaries(chapter, summaryWindow); err == nil && len(summaries) > 0 {
		result["recent_summaries"] = summaries
	} else {
		warn("recent_summaries", err)
	}
}

// loadLayeredCharacters nạp nhân vật trong chế độ Layered: ưu tiên dùng snapshot gần nhất, lùi về thiết lập gốc + lọc Tier.
func (t *ContextTool) loadLayeredCharacters(result map[string]any, chapter int, warn func(string, error)) {
	snapshots, err := t.store.Characters.LoadLatestSnapshots()
	if err == nil && len(snapshots) > 0 {
		result["character_snapshots"] = snapshots
		// Đồng thời giữ lại nhân vật core/important trong thiết lập gốc (snapshot có thể không chứa nhân vật mới xuất hiện)
		t.loadFilteredCharacters(result, chapter, warn)
		return
	}
	warn("character_snapshots", err)
	// Khi không có snapshot thì lùi về thiết lập gốc
	t.loadFilteredCharacters(result, chapter, warn)
}

// writerReferences trả về tài liệu tham khảo viết. Chương 1 trả về toàn bộ, các chương sau cắt bớt các mẫu không còn cần.
func (t *ContextTool) writerReferences(chapter int) map[string]string {
	refs := map[string]string{}
	add := func(k, v string) {
		if v != "" {
			refs[k] = v
		}
	}
	// Nạp tăng dần: luôn giữ tham khảo cốt lõi, 3 chương đầu nạp thêm hướng dẫn viết đầy đủ
	add("consistency", t.refs.Consistency)
	add("hook_techniques", t.refs.HookTechniques)
	add("quality_checklist", t.refs.QualityChecklist)
	add("anti_ai_tone", t.refs.AntiAITone) // tiêu chí khử giọng AI tiêm xuyên suốt, không cắt theo chương
	if chapter <= 3 {
		add("chapter_guide", t.refs.ChapterGuide)
		add("dialogue_writing", t.refs.DialogueWriting)
		add("style_reference", t.refs.StyleReference)
	}

	// Tham khảo bổ sung chỉ nạp ở chương đầu
	if chapter <= 1 {
		add("chapter_template", t.refs.ChapterTemplate)
		add("content_expansion", t.refs.ContentExpansion)
	}
	return refs
}

func (t *ContextTool) architectReferences() map[string]string {
	refs := map[string]string{}
	add := func(k, v string) {
		if v != "" {
			refs[k] = v
		}
	}
	add("outline_template", t.refs.OutlineTemplate)
	add("character_template", t.refs.CharacterTemplate)
	add("longform_planning", t.refs.LongformPlanning)
	add("differentiation", t.refs.Differentiation)
	add("style_reference", t.refs.StyleReference)
	add("arc_templates", t.refs.ArcTemplates)
	add("anti_ai_tone", t.refs.AntiAITone) // dàn ý architect khử giọng AI; cũng dự phòng cho editor đi đường Chapter=0
	return refs
}

// foundationStatus kiểm tra tính đầy đủ của thiết lập nền tảng, trả về danh sách các mục còn thiếu.
// Dùng chung logic phán định store.FoundationMissing với tool save_foundation, đảm bảo ready/missing mà LLM
// thấy từ novel_context và foundation_ready do save_foundation trả về
// luôn nhất quán (các chi tiết như mục bắt buộc của compass truyện dài không bị trôi).
func (t *ContextTool) foundationStatus() map[string]any {
	missing := t.store.FoundationMissing()
	status := map[string]any{"ready": len(missing) == 0}
	if len(missing) > 0 {
		status["missing"] = missing
	}
	return status
}

// ContextSummary trả về tóm tắt ngắn gọn trạng thái hiện tại (dùng cho log).
func (t *ContextTool) ContextSummary() string {
	var parts []string
	if p, _ := t.store.Outline.LoadPremise(); p != "" {
		parts = append(parts, "premise:ok")
	}
	if o, _ := t.store.Outline.LoadOutline(); o != nil {
		parts = append(parts, fmt.Sprintf("outline:%d chapters", len(o)))
	}
	if c, _ := t.store.Characters.Load(); c != nil {
		parts = append(parts, fmt.Sprintf("characters:%d", len(c)))
	}
	if len(parts) == 0 {
		return "empty"
	}
	return strings.Join(parts, ", ")
}

// trimByBudget cắt bớt result theo ưu tiên, để tổng kích thước JSON không vượt budget byte.
// Ưu tiên (từ thấp đến cao): references < voice_samples < style_anchors < previous_tail < timeline
//
//	< recent_state_changes < foreshadow_ledger < relationship_state < phần còn lại (không cắt)
//
// Các key bị cắt sẽ được ghi vào result["_trimmed"] để tra cứu log.
func trimByBudget(result map[string]any, budget int) {
	// Đo kích thước hiện tại trước
	data, err := json.Marshal(result)
	if err != nil || len(data) <= budget {
		return
	}

	// Liệt kê các key có thể cắt theo ưu tiên từ thấp đến cao
	trimOrder := []string{
		"references",
		"voice_samples",
		"style_anchors",
		"style_rules",
		"style_stats",
		"previous_tail",
		"timeline",
		"recent_state_changes",
		"foreshadow_ledger",
		"relationship_state",
	}

	var trimmed []string
	for _, key := range trimOrder {
		if _, ok := result[key]; !ok {
			continue
		}
		deleteContextKey(result, key)
		trimmed = append(trimmed, key)
		data, err = json.Marshal(result)
		if err != nil || len(data) <= budget {
			break
		}
	}
	if len(trimmed) > 0 {
		result["_trimmed"] = trimmed
	}
}

func deleteContextKey(result map[string]any, key string) {
	delete(result, key)
	for _, containerKey := range []string{
		"working_memory",
		"episodic_memory",
		"planning_memory",
		"foundation_memory",
		"reference_pack",
	} {
		section, ok := result[containerKey].(map[string]any)
		if !ok {
			continue
		}
		delete(section, key)
	}
}

// buildRelatedChapters tra ngược các chương lịch sử liên quan đến chương hiện tại dựa trên dữ liệu có cấu trúc.
// Gợi ý từ bốn chiều: phục bút, sự xuất hiện nhân vật, thay đổi trạng thái, quan hệ; sau khi khử trùng lặp trả về tối đa 5 mục.
// Mọi dữ liệu truyền vào qua tham số, không thực hiện IO bổ sung.
func (t *ContextTool) buildRelatedChapters(
	chapter int,
	entry *domain.OutlineEntry,
	foreshadow []domain.ForeshadowEntry,
	relationships []domain.RelationshipEntry,
	stateChanges []domain.StateChange,
) []domain.RelatedChapter {
	const recentWindow = 10
	const maxResults = 5

	seen := make(map[int]struct{})
	var results []domain.RelatedChapter
	add := func(ch int, reason string) {
		if ch <= 0 || ch >= chapter {
			return
		}
		// Vài chương gần nhất quá gần, không gợi ý
		if ch > chapter-recentWindow {
			return
		}
		if _, ok := seen[ch]; ok {
			return
		}
		seen[ch] = struct{}{}
		results = append(results, domain.RelatedChapter{Chapter: ch, Reason: reason})
	}

	// Ghép văn bản dàn ý để khớp từ khóa
	outlineText := entry.Title + " " + entry.CoreEvent
	for _, s := range entry.Scenes {
		outlineText += " " + s
	}

	// 1. Tra ngược phục bút: mô tả của phục bút đang hoạt động có liên quan đến dàn ý chương hiện tại không
	for _, f := range foreshadow {
		if strings.Contains(outlineText, f.ID) || containsAny(outlineText, strings.Fields(f.Description)) {
			add(f.PlantedAt, fmt.Sprintf(contentlang.Pick("伏笔%s(%s)埋设章", "Chương cài phục bút %s(%s)"), f.ID, truncateRunes(f.Description, 15)))
		}
		if len(results) >= maxResults {
			break
		}
	}

	// 2. Tra ngược sự xuất hiện nhân vật: duyệt một lần theo lô, IO giảm từ O(số nhân vật×số chương) xuống O(số chương)
	chars, _ := t.store.Characters.Load()
	outlineChars := matchOutlineCharacters(outlineText, chars)
	if len(outlineChars) > 0 {
		appearances := t.store.Summaries.FindCharacterAppearances(outlineChars, chapter, recentWindow)
		for _, name := range outlineChars {
			if len(results) >= maxResults {
				break
			}
			if ch, ok := appearances[name]; ok {
				add(ch, fmt.Sprintf(contentlang.Pick("角色'%s'最后出场章", "Chương xuất hiện cuối của nhân vật '%s'"), name))
			}
		}
	}

	// 3. Tra ngược thay đổi trạng thái: thao tác trên slice đã nạp, không IO
	for _, name := range outlineChars {
		if len(results) >= maxResults {
			break
		}
		ch := findLastStateChange(stateChanges, name, chapter)
		if ch > 0 && ch <= chapter-recentWindow {
			add(ch, fmt.Sprintf(contentlang.Pick("'%s'状态变化章", "Chương thay đổi trạng thái của '%s'"), name))
		}
	}

	// 4. Tra ngược quan hệ: thay đổi quan hệ gần nhất giữa các cặp nhân vật liên quan trong chương hiện tại
	if len(relationships) > 0 && len(outlineChars) >= 2 {
		charSet := make(map[string]struct{}, len(outlineChars))
		for _, c := range outlineChars {
			charSet[c] = struct{}{}
		}
		for _, r := range relationships {
			if len(results) >= maxResults {
				break
			}
			_, aIn := charSet[r.CharacterA]
			_, bIn := charSet[r.CharacterB]
			if aIn && bIn {
				add(r.Chapter, fmt.Sprintf(contentlang.Pick("%s-%s关系变化", "Thay đổi quan hệ %s-%s"), r.CharacterA, r.CharacterB))
			}
		}
	}

	return results
}

// findLastStateChange tìm số chương của lần thay đổi gần nhất của thực thể trong danh sách thay đổi trạng thái đã nạp.
func findLastStateChange(changes []domain.StateChange, entity string, currentChapter int) int {
	for i := len(changes) - 1; i >= 0; i-- {
		if changes[i].Entity == entity && changes[i].Chapter < currentChapter {
			return changes[i].Chapter
		}
	}
	return 0
}

// matchOutlineCharacters khớp tên nhân vật xuất hiện từ văn bản dàn ý.
func matchOutlineCharacters(text string, chars []domain.Character) []string {
	var matched []string
	for _, c := range chars {
		if strings.Contains(text, c.Name) {
			matched = append(matched, c.Name)
			continue
		}
		for _, alias := range c.Aliases {
			if strings.Contains(text, alias) {
				matched = append(matched, c.Name)
				break
			}
		}
	}
	return matched
}

// containsAny kiểm tra text có chứa bất kỳ từ nào trong words không (phải ít nhất 2 ký tự mới khớp, tránh nhiễu).
func containsAny(text string, words []string) bool {
	for _, w := range words {
		if len([]rune(w)) >= 2 && strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func (t *ContextTool) selectStoryThreads(state contextBuildState) []domain.RecallItem {
	if state.currentEntry == nil {
		return nil
	}
	if len(state.foreshadow) < storyThreadRecallThreshold {
		return nil
	}

	const maxThreads = 5
	var items []domain.RecallItem
	seen := make(map[string]struct{})
	picked := make(map[string]struct{}) // ID phục bút đã chọn, dùng để khử trùng lặp khi bù theo tuổi tồn
	add := func(item domain.RecallItem) {
		key := item.Kind + "|" + item.Key + "|" + item.Summary
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		picked[item.Key] = struct{}{}
		items = append(items, item)
	}

	// 1. Triệu hồi theo độ liên quan: các phục bút có từ trùng với từ focus của chương hiện tại.
	focusTerms := recallFocusTerms(state.currentEntry, state.chapterPlan)
	focusText := strings.Join(focusTerms, " ")
	for _, entry := range state.foreshadow {
		if !matchesRecallTerms(entry.ID+" "+entry.Description, focusTerms) && !strings.Contains(focusText, entry.ID) {
			continue
		}
		add(domain.RecallItem{
			Kind:    "story_thread",
			Key:     entry.ID,
			Chapter: entry.PlantedAt,
			Reason:  contentlang.Pick("当前章可能需要承接既有伏笔", "Chương hiện tại có thể cần tiếp nối phục bút sẵn có"),
			Summary: fmt.Sprintf(contentlang.Pick("伏笔“%s”埋于第%d章：%s", "Phục bút “%s” cài ở chương %d: %s"), entry.ID, entry.PlantedAt, truncateRunes(entry.Description, 30)),
		})
		if len(items) >= maxThreads {
			return items
		}
	}

	// 2. Bù theo tuổi tồn: các phục bút không liên quan đến chương hiện tại nhưng treo lâu chưa thu hồi (cũ nhất ưu tiên), bù cho số suất còn lại.
	//    Bù vào chính là điểm mù tự nhiên của triệu hồi theo độ liên quan — sợi dây treo một mình quá lâu mà không đụng từ khóa trong chương này.
	for _, entry := range agingForeshadow(state.foreshadow, state.chapter, picked) {
		add(domain.RecallItem{
			Kind:    "story_thread",
			Key:     entry.ID,
			Chapter: entry.PlantedAt,
			Reason:  contentlang.Pick("伏笔久挂未回收，注意适时推进或回收", "Phục bút treo lâu chưa thu hồi, lưu ý thúc đẩy hoặc thu hồi đúng lúc"),
			Summary: fmt.Sprintf(contentlang.Pick("伏笔“%s”埋于第%d章，已 %d 章未回收：%s", "Phục bút “%s” cài ở chương %d, đã %d chương chưa thu hồi: %s"), entry.ID, entry.PlantedAt, state.chapter-entry.PlantedAt, truncateRunes(entry.Description, 30)),
		})
		if len(items) >= maxThreads {
			break
		}
	}

	return items
}

// agingForeshadow trả về các phục bút chưa thu hồi có tuổi tồn ≥ foreshadowAgingChapters, sắp xếp cũ nhất ưu tiên,
// bỏ qua những cái đã được triệu hồi theo độ liên quan chọn trong picked. Tham số all vốn đã là danh sách active (chưa thu hồi), nên không cần lọc trạng thái nữa.
func agingForeshadow(all []domain.ForeshadowEntry, chapter int, picked map[string]struct{}) []domain.ForeshadowEntry {
	var aging []domain.ForeshadowEntry
	for _, e := range all {
		if _, ok := picked[e.ID]; ok {
			continue
		}
		if e.PlantedAt <= 0 || chapter-e.PlantedAt < foreshadowAgingChapters {
			continue
		}
		aging = append(aging, e)
	}
	sort.SliceStable(aging, func(i, j int) bool {
		return aging[i].PlantedAt < aging[j].PlantedAt
	})
	return aging
}

func (t *ContextTool) selectReviewLessons(chapter int, warn func(string, error)) []domain.RecallItem {
	if chapter <= 1 {
		return nil
	}

	var items []domain.RecallItem
	seen := make(map[string]struct{})
	add := func(item domain.RecallItem) {
		key := item.Summary
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}

	appendReview := func(review *domain.ReviewEntry) bool {
		if review == nil {
			return false
		}
		for i, miss := range review.ContractMisses {
			add(domain.RecallItem{
				Kind:    "review_lesson",
				Key:     fmt.Sprintf("review-%d-contract-%d", review.Chapter, i),
				Chapter: review.Chapter,
				Reason:  contentlang.Pick("最近审阅指出 contract 漏项", "Rà soát gần đây chỉ ra contract bị thiếu mục"),
				Summary: fmt.Sprintf(contentlang.Pick("第%d章 contract 漏项：%s", "Chương %d contract thiếu mục: %s"), review.Chapter, miss),
			})
			if len(items) >= 3 {
				return true
			}
		}
		for i, issue := range review.Issues {
			switch issue.Severity {
			case "", "warning", "error", "critical":
				add(domain.RecallItem{
					Kind:    "review_lesson",
					Key:     fmt.Sprintf("review-%d-issue-%d", review.Chapter, i),
					Chapter: review.Chapter,
					Reason:  contentlang.Pick("最近审阅指出需要避免重复问题", "Rà soát gần đây chỉ ra cần tránh lặp lại vấn đề"),
					Summary: fmt.Sprintf(contentlang.Pick("第%d章审阅提醒：%s", "Nhắc nhở rà soát chương %d: %s"), review.Chapter, truncateRunes(issue.Description, 36)),
				})
			}
			if len(items) >= 3 {
				return true
			}
		}
		return false
	}

	for ch := chapter - 1; ch >= max(chapter-3, 1); ch-- {
		review, err := t.store.World.LoadReview(ch)
		if err != nil {
			warn("review", err)
			continue
		}
		if appendReview(review) {
			return items
		}
	}

	globalReview, err := t.store.World.LoadLastReview(chapter - 1)
	if err != nil {
		warn("global_review", err)
	} else if appendReview(globalReview) {
		return items
	}
	return items
}

func recallFocusTerms(entry *domain.OutlineEntry, plan *domain.ChapterPlan) []string {
	if entry == nil {
		return nil
	}
	var terms []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			terms = append(terms, v)
		}
	}

	add(entry.Title)
	add(entry.CoreEvent)
	add(entry.Hook)
	for _, scene := range entry.Scenes {
		add(scene)
	}
	if plan != nil {
		add(plan.Goal)
		add(plan.Hook)
		for _, point := range plan.Contract.PayoffPoints {
			add(point)
		}
		add(plan.Contract.HookGoal)
	}
	return terms
}

func matchesRecallTerms(text string, terms []string) bool {
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if len([]rune(term)) < 2 {
			continue
		}
		if strings.Contains(text, term) || strings.Contains(term, text) {
			return true
		}
		if hasMeaningfulOverlap(term, text) {
			return true
		}
	}
	return false
}

func hasMeaningfulOverlap(a, b string) bool {
	ar := []rune(strings.TrimSpace(a))
	br := []rune(strings.TrimSpace(b))
	if len(ar) < 5 || len(br) < 5 {
		return false
	}
	shorter := len(ar)
	if len(br) < shorter {
		shorter = len(br)
	}
	threshold := 5
	switch {
	case shorter >= 12:
		threshold = 7
	case shorter >= 9:
		threshold = 6
	}
	return longestCommonSubstringRunes(ar, br) >= threshold
}

const storyThreadRecallThreshold = 6
const storyThreadRecallMinSelected = 2

// foreshadowAgingChapters: một phục bút kể từ khi cài cắm vượt quá ngần này chương vẫn chưa thu hồi thì xem là "treo lâu".
// Loại phục bút này dù không liên quan đến từ khóa chương hiện tại cũng được bù vào story_threads, tránh bị quên hẳn trong truyện dài
// (triệu hồi theo độ liên quan vốn chỉ thấy những sợi liên quan đến chương này, không thấy sợi treo một mình quá lâu).
// Tuổi tồn là sự kiện thuần túy suy ra từ code (chương hiện tại - chương cài cắm), chỉ nêu "đã treo N chương chưa thu hồi", không ra chỉ thị.
const foreshadowAgingChapters = 30

func longestCommonSubstringRunes(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	best := 0
	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		for j := 1; j <= len(b); j++ {
			if a[i-1] != b[j-1] {
				continue
			}
			curr[j] = prev[j-1] + 1
			if curr[j] > best {
				best = curr[j]
			}
		}
		prev = curr
	}
	return best
}

// truncateRunes cắt chuỗi xuống số rune chỉ định.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
