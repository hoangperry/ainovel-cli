package imp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// FoundationResult là sản phẩm có cấu trúc của việc suy ngược Foundation.
type FoundationResult struct {
	Premise    string                 // chuỗi Markdown
	Characters []domain.Character     // hồ sơ nhân vật
	WorldRules []domain.WorldRule     // quy tắc thế giới
	Volumes    []domain.VolumeOutline // outline phân tầng: nhập nội dung làm quyển đầu tiên (viết tiếp được, mở rộng được)
	Compass    *domain.StoryCompass   // mỏ neo hướng viết tiếp (ending_direction / open_threads / estimated_scale)
}

// LLMChat là dependency tối thiểu của package imp với ChatModel: chỉ cần một lần sinh văn bản thông thường.
// Tách ra interface độc lập để tiện inject mock khi unit test, tránh kết dính trực tiếp với client agentcore.
type LLMChat interface {
	Generate(ctx context.Context, messages []agentcore.Message, tools []agentcore.ToolSpec, opts ...agentcore.CallOption) (*agentcore.LLMResponse, error)
}

// ReverseFoundation dùng một lần gọi LLM, suy ngược foundation từ nội dung các chương đã cắt.
// Không gọi save_foundation, là hàm thuần; việc persist do caller quyết định.
func ReverseFoundation(ctx context.Context, llm LLMChat, systemPrompt string, chapters []Chapter) (*FoundationResult, error) {
	if len(chapters) == 0 {
		return nil, fmt.Errorf("no chapters to analyze")
	}
	if llm == nil {
		return nil, fmt.Errorf("llm is nil")
	}

	system := strings.ReplaceAll(systemPrompt, "${chapter_count}", fmt.Sprintf("%d", len(chapters)))
	user := buildFoundationUserPrompt(chapters)

	resp, err := llm.Generate(ctx, []agentcore.Message{
		agentcore.SystemMsg(system),
		agentcore.UserMsg(user),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("llm generate: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("llm returned nil response")
	}

	return parseFoundationOutput(resp.Message.TextContent(), len(chapters))
}

// buildFoundationUserPrompt lắp ráp prompt người dùng: ghép tất cả chương theo thứ tự, kèm mỏ neo số chương để LLM tiện tham chiếu.
func buildFoundationUserPrompt(chapters []Chapter) string {
	var sb strings.Builder
	sb.WriteString(contentlang.Pick("以下是已完成的 ", "Dưới đây là "))
	fmt.Fprintf(&sb, "%d", len(chapters))
	sb.WriteString(contentlang.Pick(" 章正文。请严格按系统提示反推 foundation，输出五个 === TAG === 段。\n\n", " chương chính văn đã hoàn thành. Hãy nghiêm ngặt suy ngược foundation theo system prompt, xuất năm đoạn === TAG ===.\n\n"))
	for i, ch := range chapters {
		fmt.Fprintf(&sb, contentlang.Pick("## 第 %d 章：%s\n\n", "## Chương %d: %s\n\n"), i+1, ch.Title)
		sb.WriteString(ch.Content)
		sb.WriteString("\n\n---\n\n")
	}
	return sb.String()
}

// parseFoundationOutput parse envelope mà LLM xuất ra và kiểm tra các ràng buộc then chốt.
func parseFoundationOutput(text string, expectChapters int) (*FoundationResult, error) {
	env := parseTaggedEnvelope(text)
	if env == nil {
		return nil, fmt.Errorf("no === TAG === envelope found in LLM output")
	}
	if err := requireTags(env, "PREMISE", "CHARACTERS", "WORLD_RULES", "LAYERED_OUTLINE", "COMPASS"); err != nil {
		return nil, err
	}

	premise := stripFences(env["PREMISE"])
	if !strings.HasPrefix(strings.TrimLeft(premise, " \t\n"), "#") {
		return nil, fmt.Errorf("premise must start with a Markdown heading line (# 书名)")
	}

	var characters []domain.Character
	if err := decodeJSON("characters", env["CHARACTERS"], &characters); err != nil {
		return nil, err
	}
	if len(characters) == 0 {
		return nil, fmt.Errorf("characters array is empty")
	}

	var worldRules []domain.WorldRule
	if err := decodeJSON("world_rules", env["WORLD_RULES"], &worldRules); err != nil {
		return nil, err
	}

	var volumes []domain.VolumeOutline
	if err := decodeJSON("layered_outline", env["LAYERED_OUTLINE"], &volumes); err != nil {
		return nil, err
	}
	// Outline import bắt buộc khai triển thực đủ toàn bộ N chương (FlattenOutline chỉ đếm chương thật, cung truyện khung xương không tính),
	// nếu không khi commit từng chương sẽ có chương rơi ra ngoài phạm vi outline và bị bộ canh vượt biên từ chối.
	if got := len(domain.FlattenOutline(volumes)); got != expectChapters {
		return nil, fmt.Errorf("layered outline chapter count mismatch: got %d, want %d", got, expectChapters)
	}

	var compass domain.StoryCompass
	if err := decodeJSON("compass", env["COMPASS"], &compass); err != nil {
		return nil, err
	}

	return &FoundationResult{
		Premise:    premise,
		Characters: characters,
		WorldRules: worldRules,
		Volumes:    volumes,
		Compass:    &compass,
	}, nil
}

// PersistFoundation ghi kết quả suy ngược vào Store, thứ tự nhất quán với prompt Architect trường thiên:
// premise → characters → world_rules → layered_outline → compass. Nhập nội dung làm quyển đầu tiên
// để dựng thành outline phân tầng, giúp cuốn sách import có thể viết tiếp, mở rộng. Mỗi bước đều kích hoạt cùng logic ghi xuống của save_foundation.
//
// Không gọi trực tiếp SaveFoundationTool vì đây là phát lại xác định, không cần qua điều phối tool của LLM.
// Nhưng giữ cùng tác dụng phụ với SaveFoundationTool: tiến phase, thêm checkpoint.
func PersistFoundation(ctx context.Context, st *store.Store, scale domain.PlanningTier, fr *FoundationResult) error {
	if fr == nil {
		return fmt.Errorf("nil foundation result")
	}
	if err := st.RunMeta.SetPlanningTier(scale); err != nil {
		return fmt.Errorf("save planning tier: %w", err)
	}

	// 1. premise
	if err := st.Outline.SavePremise(fr.Premise); err != nil {
		return fmt.Errorf("save premise: %w", err)
	}
	if name := domain.ExtractNovelNameFromPremise(fr.Premise); name != "" {
		_ = st.Progress.SetNovelName(name)
	}
	_ = st.Progress.UpdatePhase(domain.PhasePremise)
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "premise", "premise.md"); err != nil {
		return fmt.Errorf("checkpoint premise: %w", err)
	}

	// 2. characters
	if err := st.Characters.Save(fr.Characters); err != nil {
		return fmt.Errorf("save characters: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "characters", "characters.json"); err != nil {
		return fmt.Errorf("checkpoint characters: %w", err)
	}

	// 3. world_rules
	if err := st.World.SaveWorldRules(fr.WorldRules); err != nil {
		return fmt.Errorf("save world_rules: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "world_rules", "world_rules.json"); err != nil {
		return fmt.Errorf("checkpoint world_rules: %w", err)
	}

	// 4. layered outline (nhập nội dung làm quyển đầu tiên → chế độ phân tầng, viết tiếp được, mở rộng được)
	if err := st.Outline.SaveLayeredOutline(fr.Volumes); err != nil {
		return fmt.Errorf("save layered outline: %w", err)
	}
	if err := st.Outline.SaveOutline(domain.FlattenOutline(fr.Volumes)); err != nil {
		return fmt.Errorf("save flattened outline: %w", err)
	}
	_ = st.Progress.UpdatePhase(domain.PhaseOutline)
	_ = st.Progress.SetTotalChapters(domain.TotalChapters(fr.Volumes))
	_ = st.Progress.SetLayered(true)
	if len(fr.Volumes) > 0 && len(fr.Volumes[0].Arcs) > 0 {
		_ = st.Progress.UpdateVolumeArc(fr.Volumes[0].Index, fr.Volumes[0].Arcs[0].Index)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "layered_outline", "layered_outline.json"); err != nil {
		return fmt.Errorf("checkpoint layered outline: %w", err)
	}

	// 5. compass (mỏ neo hướng viết tiếp): để layeredBookComplete phán định theo open_threads,
	//    tránh import xong là bị phán hoàn kết; cũng cho hướng/dung lượng khi viết tiếp một mốc chuẩn.
	if err := st.Outline.SaveCompass(*fr.Compass); err != nil {
		return fmt.Errorf("save compass: %w", err)
	}
	if _, err := st.Checkpoints.AppendArtifact(domain.GlobalScope(), "compass", "meta/compass.json"); err != nil {
		return fmt.Errorf("checkpoint compass: %w", err)
	}

	// 6. foundation đầy đủ → tiến tới giai đoạn writing (nhất quán với logic cuối của save_foundation)
	if len(st.FoundationMissing()) == 0 {
		if p, _ := st.Progress.Load(); p != nil &&
			p.Phase != domain.PhaseWriting && p.Phase != domain.PhaseComplete {
			_ = st.Progress.UpdatePhase(domain.PhaseWriting)
		}
	}
	return nil
}

// decodeJSON parse JSON (mảng hoặc object) và đính kèm nhãn, tiện debug.
func decodeJSON(label, body string, out any) error {
	body = stripFences(body)
	if body == "" {
		return fmt.Errorf("%s body is empty", label)
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("parse %s JSON: %w", label, err)
	}
	return nil
}

// stripFences bỏ code fence ``` ở đầu cuối (kèm nhãn ngôn ngữ), LLM thỉnh thoảng tự ý bọc thêm một lớp.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}
