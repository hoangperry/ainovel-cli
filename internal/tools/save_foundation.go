package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/voocel/agentcore/schema"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/i18n"
	"github.com/voocel/ainovel-cli/internal/store"
)

// SaveFoundationTool lưu thiết lập nền tảng (premise/outline/characters), dành riêng cho Architect.
type SaveFoundationTool struct {
	store *store.Store
}

func NewSaveFoundationTool(store *store.Store) *SaveFoundationTool {
	return &SaveFoundationTool{store: store}
}

func (t *SaveFoundationTool) Name() string { return "save_foundation" }
func (t *SaveFoundationTool) Description() string {
	return contentlang.Pick(
		"保存小说基础设定（premise/outline/characters/world_rules/compass 等）。**这是唯一持久化入口**：未经此工具调用保存的内容不会进入 store，只在消息里输出 Markdown/JSON 等于丢失。参数固定为 {type, content, scale?, volume?, arc?}。type 可选 premise / outline / layered_outline / characters / world_rules / expand_arc / append_volume / update_compass / complete_book。premise 时 content 必须是 Markdown 字符串；其他类型 content 优先直接传 JSON 数组或对象。expand_arc 展开骨架弧的详细章节（需 volume + arc）；append_volume 追加新卷（content 为完整 VolumeOutline JSON，含弧结构）；update_compass 更新终局方向（content 为 StoryCompass JSON）；complete_book 宣告全书完结（content 传空对象 {}，直接推 Phase=Complete；调用前必须先通过终卷判定清单，且无返工队列）。scale 可选，仅允许 short / mid / long。",
		"Lưu thiết lập nền của tiểu thuyết (premise/outline/characters/world_rules/compass...). **Đây là cổng lưu trữ duy nhất**: nội dung không lưu qua tool này sẽ không vào store, chỉ in Markdown/JSON trong tin nhắn coi như mất. Tham số cố định là {type, content, scale?, volume?, arc?}. type chọn premise / outline / layered_outline / characters / world_rules / expand_arc / append_volume / update_compass / complete_book. Khi premise thì content phải là chuỗi Markdown; các type khác content ưu tiên truyền thẳng mảng hoặc đối tượng JSON. expand_arc mở chi tiết chương của cung khung (cần volume + arc); append_volume nối quyển mới (content là VolumeOutline JSON đầy đủ, gồm cấu trúc cung); update_compass cập nhật hướng kết cục (content là StoryCompass JSON); complete_book tuyên bố toàn sách hoàn thành (content truyền đối tượng rỗng {}, đẩy thẳng Phase=Complete; trước khi gọi phải qua danh sách kiểm tra quyết định quyển cuối, và không còn hàng đợi làm lại). scale tùy chọn, chỉ cho phép short / mid / long.",
	)
}
func (t *SaveFoundationTool) Label() string { return i18n.T("ui.tool.save_foundation.label") }

// Tool ghi (cập nhật liên miền Outline/Progress/Characters), cấm song song.
func (t *SaveFoundationTool) ReadOnly(_ json.RawMessage) bool        { return false }
func (t *SaveFoundationTool) ConcurrencySafe(_ json.RawMessage) bool { return false }

func (t *SaveFoundationTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("type", schema.Enum(contentlang.Pick("设定类型", "Loại thiết lập"), "premise", "outline", "layered_outline", "characters", "world_rules", "expand_arc", "append_volume", "update_compass", "complete_book")).Required(),
		schema.Property("content", map[string]any{
			"description": contentlang.Pick("内容。premise 传 Markdown 字符串；其他类型直接传 JSON 数组或对象即可，也兼容传 JSON 字符串。expand_arc 时传章节数组。", "Nội dung. premise truyền chuỗi Markdown; các loại khác truyền trực tiếp mảng hoặc đối tượng JSON, cũng tương thích truyền chuỗi JSON. Khi expand_arc thì truyền mảng chương."),
		}).Required(),
		schema.Property("scale", schema.Enum(contentlang.Pick("规划级别", "Cấp độ quy hoạch"), "short", "mid", "long")),
		schema.Property("volume", schema.Int(contentlang.Pick("目标卷序号（仅 expand_arc 时必传）", "Số thứ tự quyển mục tiêu (chỉ bắt buộc khi expand_arc)"))),
		schema.Property("arc", schema.Int(contentlang.Pick("目标弧序号（仅 expand_arc 时必传）", "Số thứ tự cung truyện mục tiêu (chỉ bắt buộc khi expand_arc)"))),
	)
}

func (t *SaveFoundationTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
		Scale   string          `json:"scale"`
		Volume  int             `json:"volume"`
		Arc     int             `json:"arc"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	content, err := normalizeFoundationContent(a.Content)
	if err != nil {
		return nil, err
	}
	if a.Scale != "" {
		switch domain.PlanningTier(a.Scale) {
		case domain.PlanningTierShort, domain.PlanningTierMid, domain.PlanningTierLong:
		default:
			return nil, fmt.Errorf("invalid scale %q, expected short/mid/long: %w", a.Scale, errs.ErrToolArgs)
		}
		if err := t.store.RunMeta.SetPlanningTier(domain.PlanningTier(a.Scale)); err != nil {
			return nil, fmt.Errorf("save planning tier: %w: %w", errs.ErrStoreWrite, err)
		}
	}

	result := map[string]any{"saved": true, "type": a.Type, "scale": a.Scale}

	// Trong giai đoạn viết cấm ghi đè toàn bộ dàn ý, chỉ cho phép thao tác tăng dần (expand_arc / append_volume)
	if (a.Type == "outline" || a.Type == "layered_outline") && t.isWriting() {
		return nil, fmt.Errorf("%s: %w", contentlang.Pick(
			fmt.Sprintf("写作阶段禁止使用 %s 全量覆盖大纲。请使用 expand_arc 展开骨架弧，或 append_volume 追加新卷", a.Type),
			fmt.Sprintf("Giai đoạn viết cấm dùng %s để ghi đè toàn bộ dàn ý. Hãy dùng expand_arc để mở rộng cung khung, hoặc append_volume để thêm quyển mới", a.Type),
		), errs.ErrToolPrecondition)
	}

	decode := func(typeName string, out any) error {
		return decodeFoundationJSON(typeName, content, out)
	}

	switch a.Type {
	case "premise":
		name := domain.ExtractNovelNameFromPremise(content)
		if err := t.store.Outline.SavePremise(content); err != nil {
			return nil, fmt.Errorf("save premise: %w: %w", errs.ErrStoreWrite, err)
		}
		if name != "" {
			_ = t.store.Progress.SetNovelName(name)
			result["novel_name"] = name
		}
		_ = t.store.Progress.UpdatePhase(domain.PhasePremise)

	case "outline":
		var entries []domain.OutlineEntry
		if err := decode("outline", &entries); err != nil {
			return nil, err
		}
		if err := t.store.Outline.SaveOutline(entries); err != nil {
			return nil, fmt.Errorf("save outline: %w: %w", errs.ErrStoreWrite, err)
		}
		_ = t.store.Progress.UpdatePhase(domain.PhaseOutline)
		_ = t.store.Progress.SetTotalChapters(len(entries))
		if domain.PlanningTier(a.Scale) != domain.PlanningTierLong {
			_ = t.store.Progress.SetLayered(false)
			_ = t.store.Progress.UpdateVolumeArc(0, 0)
			_ = t.store.Outline.ClearLayeredOutline()
		}
		result["chapters"] = len(entries)

	case "layered_outline":
		var volumes []domain.VolumeOutline
		if err := decode("layered_outline", &volumes); err != nil {
			return nil, err
		}
		if err := t.store.Outline.SaveLayeredOutline(volumes); err != nil {
			return nil, fmt.Errorf("save layered_outline: %w: %w", errs.ErrStoreWrite, err)
		}
		flat := domain.FlattenOutline(volumes)
		if err := t.store.Outline.SaveOutline(flat); err != nil {
			return nil, fmt.Errorf("save flattened outline: %w: %w", errs.ErrStoreWrite, err)
		}
		total := domain.TotalChapters(volumes)
		_ = t.store.Progress.UpdatePhase(domain.PhaseOutline)
		_ = t.store.Progress.SetTotalChapters(total)
		_ = t.store.Progress.SetLayered(true)
		if len(volumes) > 0 && len(volumes[0].Arcs) > 0 {
			_ = t.store.Progress.UpdateVolumeArc(volumes[0].Index, volumes[0].Arcs[0].Index)
		}
		result["volumes"] = len(volumes)
		result["chapters"] = total

	case "characters":
		var chars []domain.Character
		if err := decode("characters", &chars); err != nil {
			return nil, err
		}
		if err := t.store.Characters.Save(chars); err != nil {
			return nil, fmt.Errorf("save characters: %w: %w", errs.ErrStoreWrite, err)
		}
		result["count"] = len(chars)

	case "world_rules":
		var rules []domain.WorldRule
		if err := decode("world_rules", &rules); err != nil {
			return nil, err
		}
		if err := t.store.World.SaveWorldRules(rules); err != nil {
			return nil, fmt.Errorf("save world_rules: %w: %w", errs.ErrStoreWrite, err)
		}
		result["count"] = len(rules)

	case "expand_arc":
		if a.Volume <= 0 || a.Arc <= 0 {
			return nil, fmt.Errorf("expand_arc requires volume and arc parameters: %w", errs.ErrToolArgs)
		}
		var chapters []domain.OutlineEntry
		if err := decode("expand_arc chapters", &chapters); err != nil {
			return nil, err
		}
		if err := t.store.ExpandArc(a.Volume, a.Arc, chapters); err != nil {
			return nil, fmt.Errorf("expand arc: %w: %w", errs.ErrStoreWrite, err)
		}
		result["volume"] = a.Volume
		result["arc"] = a.Arc
		result["chapters"] = len(chapters)

	case "append_volume":
		if p, _ := t.store.Progress.Load(); p != nil && p.Phase == domain.PhaseComplete {
			return nil, fmt.Errorf(i18n.T("error.tool.foundation.book_complete"), errs.ErrToolPrecondition)
		}
		var vol domain.VolumeOutline
		if err := decode("append_volume", &vol); err != nil {
			return nil, err
		}
		if err := t.store.AppendVolume(vol); err != nil {
			return nil, fmt.Errorf("append volume: %w: %w", errs.ErrStoreWrite, err)
		}
		result["volume"] = vol.Index
		result["arcs"] = len(vol.Arcs)
		chCount := 0
		for _, arc := range vol.Arcs {
			chCount += len(arc.Chapters)
		}
		if chCount > 0 {
			result["chapters"] = chCount
		}

	case "complete_book":
		// Lối vào duy nhất để kết thúc toàn bộ sách: đẩy thẳng Phase=Complete.
		// Chỉ cho phép ở giai đoạn Writing, tránh giai đoạn lập kế hoạch gọi nhầm bỏ qua việc viết cả cuốn.
		// Từ chối khi gọi lúc còn hàng đợi làm lại — đảm bảo PendingRewrites chạy xong mới được kết thúc.
		progress, perr := t.store.Progress.Load()
		if perr != nil {
			return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, perr)
		}
		if progress == nil {
			return nil, fmt.Errorf(i18n.T("error.tool.foundation.no_progress"), errs.ErrToolPrecondition)
		}
		if progress.Phase != domain.PhaseWriting {
			return nil, fmt.Errorf(i18n.T("error.tool.foundation.not_writing"), progress.Phase, errs.ErrToolPrecondition)
		}
		if len(progress.PendingRewrites) > 0 {
			return nil, fmt.Errorf(i18n.T("error.tool.foundation.rework_pending"), len(progress.PendingRewrites), errs.ErrToolPrecondition)
		}
		if err := t.store.Progress.MarkComplete(); err != nil {
			return nil, fmt.Errorf("mark complete: %w: %w", errs.ErrStoreWrite, err)
		}
		result["book_complete"] = true
		result["phase"] = string(domain.PhaseComplete)

	case "update_compass":
		var compass domain.StoryCompass
		if err := decode("compass", &compass); err != nil {
			return nil, err
		}
		// Tầng tool cưỡng chế ghi đè LastUpdated bằng số chương đã hoàn thành hiện tại, không tin LLM tự điền.
		// LLM thường quên điền hoặc để 0, sẽ khiến diag.CompassDrift báo nhầm, Router định tuyến sai lệch.
		if p, _ := t.store.Progress.Load(); p != nil {
			compass.LastUpdated = p.LatestCompleted()
		}
		if err := t.store.Outline.SaveCompass(compass); err != nil {
			return nil, fmt.Errorf("save compass: %w: %w", errs.ErrStoreWrite, err)
		}
		result["ending_direction"] = compass.EndingDirection
		result["last_updated"] = compass.LastUpdated

	default:
		return nil, fmt.Errorf("unknown type %q, expected premise/outline/layered_outline/characters/world_rules/expand_arc/append_volume/update_compass/complete_book: %w", a.Type, errs.ErrToolArgs)
	}

	// checkpoint
	scope := domain.GlobalScope()
	if a.Type == "expand_arc" {
		scope = domain.ArcScope(a.Volume, a.Arc)
	} else if a.Type == "append_volume" {
		scope = domain.GlobalScope()
	}
	if _, err := t.store.Checkpoints.AppendArtifact(scope, a.Type, foundationArtifact(a.Type)); err != nil {
		return nil, fmt.Errorf("checkpoint foundation %s: %w: %w", a.Type, errs.ErrStoreWrite, err)
	}

	// Trả về các mục chưa hoàn thành còn lại, dẫn dắt Architect tiếp tục hoặc kết thúc;
	// khi đầy đủ thì đẩy phase tiến thẳng sang writing một lần, tránh Coordinator quay lại giao việc.
	remaining := t.store.FoundationMissing()
	ready := len(remaining) == 0
	result["remaining"] = remaining
	result["foundation_ready"] = ready
	if ready {
		if p, _ := t.store.Progress.Load(); p != nil &&
			p.Phase != domain.PhaseWriting && p.Phase != domain.PhaseComplete {
			_ = t.store.Progress.UpdatePhase(domain.PhaseWriting)
			result["phase"] = string(domain.PhaseWriting)
		}
	}
	return json.Marshal(result)
}

func foundationArtifact(t string) string {
	switch t {
	case "premise":
		return "premise.md"
	case "outline":
		return "outline.json"
	case "layered_outline", "expand_arc", "append_volume":
		return "layered_outline.json"
	case "complete_book":
		return "meta/progress.json"
	case "characters":
		return "characters.json"
	case "world_rules":
		return "world_rules.json"
	case "update_compass":
		return "meta/compass.json"
	default:
		return ""
	}
}

// decodeFoundationJSON phân tích trường content của save_foundation, khi thất bại kèm theo vị trí dòng/cột
// và gợi ý sửa thường gặp nhất, để LLM lần thử lại tiếp theo định vị được ngay thay vì đoán mò.
func decodeFoundationJSON(typeName, content string, out any) error {
	err := json.Unmarshal([]byte(content), out)
	if err == nil {
		return nil
	}
	hint := contentlang.Pick(
		`常见原因：字符串值中的双引号未转义为 \", 换行未转义为 \n, 或对象字段间漏了逗号。请整段重新生成一次。`,
		`Nguyên nhân thường gặp: dấu nháy kép trong giá trị chuỗi chưa escape thành \", xuống dòng chưa escape thành \n, hoặc thiếu dấu phẩy giữa các trường đối tượng. Hãy tạo lại toàn bộ một lần.`,
	)
	if se, ok := err.(*json.SyntaxError); ok {
		line, col := offsetToLineCol(content, int(se.Offset))
		return fmt.Errorf("parse %s JSON (line %d col %d): %w — %s", typeName, line, col, err, hint)
	}
	return fmt.Errorf("parse %s JSON: %w — %s", typeName, err, hint)
}

func offsetToLineCol(s string, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(s) {
		offset = len(s)
	}
	line, col := 1, 1
	for i := 0; i < offset; i++ {
		if s[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func normalizeFoundationContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("content is required: %w", errs.ErrToolArgs)
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	if !json.Valid(raw) {
		return "", fmt.Errorf("invalid content: expected Markdown string or valid JSON value: %w", errs.ErrToolArgs)
	}
	return string(raw), nil
}

func (t *SaveFoundationTool) isWriting() bool {
	p, _ := t.store.Progress.Load()
	return p != nil && p.Phase == domain.PhaseWriting
}
