package assets

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/voocel/ainovel-cli/internal/tools"
)

// Asset embed theo cây thư mục locale: mỗi loại chứa các thư mục con zh/ và vi/.
//
//go:embed prompts
var promptsFS embed.FS

//go:embed references
var referencesFS embed.FS

//go:embed styles
var stylesFS embed.FS

//go:embed rules
var rulesFS embed.FS

// AssetLocale ánh xạ ngôn ngữ output sang thư mục con asset (và locale nội dung Track C).
// "original" → cây asset tiếng Trung gốc (zh); mọi giá trị khác (vi/en/rỗng) → cây
// tiếng Việt (primary). Mode "en" dùng asset vi làm nền + languageDirective tiếng Anh,
// không nuôi riêng cây asset tiếng Anh (YAGNI). Cũng dùng để set internal/contentlang.
func AssetLocale(outputLang string) string {
	if outputLang == "original" {
		return "zh"
	}
	return "vi"
}

// Prompts là tập hợp các prompt được embed.
type Prompts struct {
	Coordinator      string
	ArchitectShort   string
	ArchitectLong    string
	Writer           string
	Editor           string
	ImportFoundation string
	ImportAnalyzer   string
	SimulationSource string
	SimulationMerge  string
}

// Bundle là tập hợp tài nguyên tĩnh cần thiết khi chạy.
type Bundle struct {
	References tools.References
	Prompts    Prompts
	Styles     map[string]string
	// RulesFS là cây con assets/rules (thư mục gốc chứa trực tiếp default.md).
	// Caller truyền cho rules.Load làm nguồn rule có sẵn.
	RulesFS fs.FS
}

// Load trả về tập tài nguyên tương ứng với style và ngôn ngữ output chỉ định.
// outputLang (vi|en|original) quyết định languageDirective chèn vào prompt của các
// role sáng tác; "original" (hoặc rỗng) giữ nguyên hành vi gốc của prompt.
func Load(style, outputLang string) Bundle {
	loc := AssetLocale(outputLang)
	return Bundle{
		References: loadReferences(style, loc),
		Prompts:    loadPrompts(loc, outputLang),
		Styles:     loadStyles(loc),
		RulesFS:    loadRulesFS(loc),
	}
}

// loadRulesFS trả về sub-filesystem của assets/rules/<loc>; thư mục gốc chứa trực tiếp default.md.
// Khi fs.Sub thất bại (về lý thuyết không nên xảy ra) trả về nil, rules.Load dựa vào đó để bỏ qua nguồn có sẵn.
func loadRulesFS(loc string) fs.FS {
	sub, err := fs.Sub(rulesFS, "rules/"+loc)
	if err != nil {
		return nil
	}
	return sub
}

func loadReferences(style, loc string) tools.References {
	if style == "" {
		style = "default"
	}
	base := "references/" + loc + "/"
	refs := tools.References{
		ChapterGuide:      mustRead(referencesFS, base+"chapter-guide.md"),
		HookTechniques:    mustRead(referencesFS, base+"hook-techniques.md"),
		QualityChecklist:  mustRead(referencesFS, base+"quality-checklist.md"),
		OutlineTemplate:   mustRead(referencesFS, base+"outline-template.md"),
		CharacterTemplate: mustRead(referencesFS, base+"character-template.md"),
		ChapterTemplate:   mustRead(referencesFS, base+"chapter-template.md"),
		Consistency:       mustRead(referencesFS, base+"consistency.md"),
		ContentExpansion:  mustRead(referencesFS, base+"content-expansion.md"),
		DialogueWriting:   mustRead(referencesFS, base+"dialogue-writing.md"),
		LongformPlanning:  mustRead(referencesFS, base+"longform-planning.md"),
		Differentiation:   mustRead(referencesFS, base+"differentiation.md"),
		AntiAITone:        mustRead(referencesFS, base+"anti-ai-tone.md"),
	}
	if style != "" && style != "default" {
		genreDir := base + "genres/" + style + "/"
		if data, err := referencesFS.ReadFile(genreDir + "style-references.md"); err == nil {
			refs.StyleReference = string(data)
		}
		if data, err := referencesFS.ReadFile(genreDir + "arc-templates.md"); err == nil {
			refs.ArcTemplates = string(data)
		}
	}
	return refs
}

func loadPrompts(loc, outputLang string) Prompts {
	base := "prompts/" + loc + "/"
	return Prompts{
		Coordinator:      assembleCreative(mustRead(promptsFS, base+"coordinator.md"), "coordinator", loc, outputLang),
		ArchitectShort:   assembleCreative(mustRead(promptsFS, base+"architect-short.md"), "architect", loc, outputLang),
		ArchitectLong:    assembleCreative(mustRead(promptsFS, base+"architect-long.md"), "architect", loc, outputLang),
		Writer:           assembleCreative(mustRead(promptsFS, base+"writer.md"), "writer", loc, outputLang),
		Editor:           assembleCreative(mustRead(promptsFS, base+"editor.md"), "editor", loc, outputLang),
		ImportFoundation: mustRead(promptsFS, base+"import-foundation.md"),
		ImportAnalyzer:   mustRead(promptsFS, base+"import-chapter-analyzer.md"),
		SimulationSource: mustRead(promptsFS, base+"simulation-source.md"),
		SimulationMerge:  mustRead(promptsFS, base+"simulation-merge.md"),
	}
}

// assembleCreative ghép prompt gốc của một role sáng tác với simulationGuidance (theo
// locale asset) và languageDirective (theo outputLang). Mọi role sáng tác đều đi qua
// đây nên chỉ thị áp dụng đồng nhất cho coordinator/architect/writer/editor.
func assembleCreative(prompt, role, loc, outputLang string) string {
	out := prompt + "\n\n" + strings.ReplaceAll(simulationGuidance(loc), "{{role}}", role)
	if d := languageDirective(outputLang); d != "" {
		out += "\n\n" + d
	}
	return out
}

// languageDirective trả về chỉ thị ngôn ngữ output chèn vào prompt sáng tác.
// "original" (hoặc rỗng/không nhận diện) = no-op → giữ ngôn ngữ gốc của prompt (tiếng Trung).
func languageDirective(lang string) string {
	switch lang {
	case "vi":
		return viLanguageDirective
	case "en":
		return enLanguageDirective
	default:
		return ""
	}
}

const viLanguageDirective = `## Ngôn ngữ và văn phong

Viết TOÀN BỘ nội dung truyện bằng tiếng Việt: chính văn, dàn ý, tóm tắt, đối thoại, tên nhân vật và địa danh, cùng mọi văn bản sáng tác. Dùng tiếng Việt tự nhiên, đúng chính tả và dấu thanh. Token kỹ thuật của công cụ (tên tool, tên trường JSON, nhãn cấu trúc) giữ nguyên — chỉ nội dung truyện mới chuyển sang tiếng Việt.

Viết như một TÁC GIẢ VIỆT viết cho độc giả Việt — KHÔNG phải dịch một truyện tiếng Trung:
- Nhịp câu tự nhiên của tiếng Việt: đan xen câu dài câu ngắn, tránh lối biền ngẫu và câu đối xứng đều đặn kiểu dịch máy.
- Ưu tiên hình ảnh, hành động, chi tiết cụ thể bằng lời Việt đời thường; tránh nhồi thành ngữ Hán-Việt bốn chữ để thay miêu tả và các sáo ngữ mòn ("khóe môi khẽ nhếch", "trong mắt lóe tia lạnh").
- Đối thoại nói như người Việt thật sự nói: ngữ khí, chỗ bỏ lửng, xưng hô hợp bối cảnh, không khách sáo cứng nhắc.
- Ví von, phong tục, chi tiết đời sống hợp bối cảnh truyện.

**Chất thể loại là ưu tiên; "giọng Việt" chỉ trị chất DỊCH MÁY — không xóa chất thể loại.** Thể loại và bối cảnh quyết định thanh điệu:
- Tu tiên / cổ trang / kiếm hiệp: GIỮ đúng lớp từ Hán-Việt của thể loại (cảnh giới, tu luyện, tông môn, kim đan...) và nhịp trang trọng — đó là register đúng, không phải "mùi dịch".
- Sảng văn: giữ nhịp gãy gọn dồn dập, cảm giác "đã tay", tiết tấu vả-mặt/lật kèo nhanh.
- Ngôn tình: giữ chất tình cảm ngọt/kịch, nội tâm giằng xé.
- Đời thường/hiện đại Việt: lời ăn tiếng nói, xưng hô, ví von thuần Việt.

Trong MỌI thể loại, đích duy nhất là câu chữ ĐỌC RA tiếng Việt tự nhiên chứ không phải bản dịch máy sượng. Nếu prompt/tài liệu tham chiếu nêu ví dụ theo mỹ học tiếng Trung, CHUYỂN HÓA sang cách nói tiếng Việt thay vì sao chép nguyên văn.`

const enLanguageDirective = `## Writing language

Write ALL novel content in English: prose, outline, summaries, dialogue, character and place names, and every creative text. Tool/protocol tokens (tool names, JSON field names, structural labels) stay as-is — only story content switches to English.`

// simulationGuidance trả về chỉ dẫn "hồ sơ mô phỏng" theo locale asset (zh gốc / vi).
func simulationGuidance(loc string) string {
	if loc == "zh" {
		return simulationGuidanceZH
	}
	return simulationGuidanceVI
}

const simulationGuidanceZH = `## 仿写画像

当 novel_context 返回 simulation_profile 时，必须把它视为当前作品的仿写方向约束。{{role}} 应读取其中的 style、lexicon、plot_design、hook_design、pacing_density、reader_engagement 和 role_guidance。

使用原则：借鉴结构、节奏、钩子、信息释放和吸引读者的手法；不要复制原文句子、人物、地名、专有设定或固定桥段。若 simulation_profile 与用户显式要求冲突，优先服从用户要求。`

const simulationGuidanceVI = `## Hồ sơ mô phỏng

Khi novel_context trả về simulation_profile, phải coi đó là ràng buộc hướng mô phỏng cho tác phẩm hiện tại. {{role}} cần đọc các trường style, lexicon, plot_design, hook_design, pacing_density, reader_engagement và role_guidance trong đó.

Nguyên tắc sử dụng: học hỏi cấu trúc, nhịp điệu, hook, cách giải phóng thông tin và thủ pháp lôi cuốn người đọc; KHÔNG sao chép câu văn, nhân vật, địa danh, thiết lập riêng hay tình tiết cố định của nguyên tác. Nếu simulation_profile xung đột với yêu cầu rõ ràng của người dùng, ưu tiên tuân theo người dùng.`

func loadStyles(loc string) map[string]string {
	styles := make(map[string]string)
	dir := "styles/" + loc
	entries, err := stylesFS.ReadDir(dir)
	if err != nil {
		return styles
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		data, err := stylesFS.ReadFile(dir + "/" + e.Name())
		if err != nil {
			continue
		}
		styles[name] = string(data)
	}
	return styles
}

func mustRead(fs embed.FS, path string) string {
	data, err := fs.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("embed read %s: %v", path, err))
	}
	return string(data)
}
