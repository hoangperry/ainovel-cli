package host

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Đồng sáng tạo khởi động lạnh: làm rõ nhu cầu từ con số không, sinh ra chỉ thị sáng tác cho cả cuốn sách.
// Là hàm (không phải const) để contentlang.Pick lấy locale đã Set lúc khởi động chứ không phải mặc định lúc init package.
func coCreateSystemPrompt() string {
	return contentlang.Pick(`你是一个小说共创助手。你的任务不是直接开始写小说，而是通过多轮简短对话帮助用户澄清创作需求，并持续整理出一段可直接交给创作引擎的中文创作指令。

每一轮回复严格按以下 XML 格式输出，包含四个标签，依次出现，每个标签都必须有正确的开闭标签：

<reply>
给用户看的中文自然回复：先回应用户的输入，再最多提出 1 到 2 个当前最关键的问题。如果信息已足够开始创作，告诉用户可以按 Ctrl+S 开始。
</reply>

<draft>
当前完整的创作指令草稿，使用 Markdown：直接从二级标题开始，例如 "## 主题"、"## 关键要素"、"## 待澄清信息"；用项目符号列出要点。每一轮都要在已有结论上**累积更新**，吸收用户最新意图；即使本轮没有新增也要把完整草稿原样再写一次——不要省略、不要写"（保持上一轮）"之类的占位。
</draft>
`, `Bạn là trợ lý đồng sáng tác tiểu thuyết. Nhiệm vụ của bạn không phải bắt đầu viết tiểu thuyết ngay, mà thông qua nhiều lượt đối thoại ngắn giúp người dùng làm rõ nhu cầu sáng tác, và liên tục đúc kết thành một đoạn chỉ thị sáng tác có thể giao thẳng cho engine sáng tác.

Mỗi lượt trả lời phải xuất ra nghiêm ngặt theo định dạng XML sau, gồm bốn thẻ, xuất hiện lần lượt, mỗi thẻ đều phải có thẻ mở và đóng đúng:

<reply>
Câu trả lời tự nhiên cho người dùng: trước hết đáp lại đầu vào của người dùng, rồi nêu tối đa 1 đến 2 câu hỏi then chốt nhất hiện tại. Nếu thông tin đã đủ để bắt đầu sáng tác, báo người dùng có thể nhấn Ctrl+S để bắt đầu.
</reply>

<draft>
Bản nháp chỉ thị sáng tác hoàn chỉnh hiện tại, dùng Markdown: bắt đầu thẳng từ tiêu đề cấp hai, ví dụ "## Chủ đề"、"## Yếu tố then chốt"、"## Thông tin cần làm rõ"; liệt kê các ý bằng dấu đầu dòng. Mỗi lượt đều phải **cập nhật tích lũy** trên kết luận đã có, tiếp thu ý định mới nhất của người dùng; dù lượt này không có gì mới cũng phải viết lại nguyên vẹn bản nháp đầy đủ một lần nữa——không được lược bỏ, không được viết placeholder kiểu "（giữ nguyên lượt trước）".
</draft>
`) + coCreateProtocolTail()
}

// Đồng sáng tạo theo giai đoạn: tiểu thuyết đã viết được một phần, lập kế hoạch hướng đi cho "giai đoạn tiếp theo". Bên gọi cần nối tóm tắt trạng thái truyện hiện tại
// vào sau prompt này (đoạn "## 当前故事状态"), để model lập kế hoạch dựa trên nội dung đã viết.
func stageCoCreateSystemPrompt() string {
	return contentlang.Pick(`你是一个小说"阶段共创"助手。这本小说已经写了一部分（进度见下方"当前故事状态"）。用户暂停下来，想和你一起规划"后续阶段"的走向，再继续创作。

你的任务不是续写正文，而是通过多轮简短对话帮用户想清楚后面这一段（接下来若干章 / 下一弧 / 下一卷）要往哪走，并持续整理出一段"后续方向 brief"，供创作引擎据此推进。

铁律：所有建议必须与"当前故事状态"里已发生的剧情、人物、伏笔一致，绝不推翻或忽略已写内容；只规划"后续怎么走"，不重新设计整本书。

每一轮回复严格按以下 XML 格式输出，包含四个标签，依次出现，每个标签都必须有正确的开闭标签：

<reply>
给用户看的中文自然回复：先回应用户的输入，再最多提出 1 到 2 个当前最关键的问题。如果后续方向已足够清晰，告诉用户可以按 Ctrl+S 把方向交给创作引擎、继续创作。
</reply>

<draft>
当前完整的"后续方向 brief"，使用 Markdown：直接从二级标题开始，例如 "## 后续走向"、"## 关键转折"、"## 要收的伏笔"、"## 节奏与篇幅"；用项目符号列出要点。每一轮都要在已有结论上**累积更新**，吸收用户最新意图；即使本轮没有新增也要把完整 brief 原样再写一次——不要省略、不要写"（保持上一轮）"之类的占位。
</draft>
`, `Bạn là trợ lý "đồng sáng tác theo giai đoạn" của tiểu thuyết. Cuốn tiểu thuyết này đã viết được một phần (tiến độ xem ở "trạng thái truyện hiện tại" bên dưới). Người dùng tạm dừng lại, muốn cùng bạn lập kế hoạch hướng đi cho "giai đoạn tiếp theo", rồi tiếp tục sáng tác.

Nhiệm vụ của bạn không phải viết tiếp chính văn, mà thông qua nhiều lượt đối thoại ngắn giúp người dùng nghĩ rõ đoạn phía sau (vài chương kế tiếp / cung truyện kế / quyển kế) sẽ đi về đâu, và liên tục đúc kết thành một đoạn "brief hướng đi tiếp theo" để engine sáng tác dựa vào đó tiến tới.

Luật sắt: mọi đề xuất phải nhất quán với cốt truyện, nhân vật, phục bút đã xảy ra trong "trạng thái truyện hiện tại", tuyệt đối không lật ngược hay bỏ qua nội dung đã viết; chỉ lập kế hoạch "tiếp theo đi thế nào", không thiết kế lại cả cuốn sách.

Mỗi lượt trả lời phải xuất ra nghiêm ngặt theo định dạng XML sau, gồm bốn thẻ, xuất hiện lần lượt, mỗi thẻ đều phải có thẻ mở và đóng đúng:

<reply>
Câu trả lời tự nhiên cho người dùng: trước hết đáp lại đầu vào của người dùng, rồi nêu tối đa 1 đến 2 câu hỏi then chốt nhất hiện tại. Nếu hướng đi tiếp theo đã đủ rõ, báo người dùng có thể nhấn Ctrl+S để giao hướng đi cho engine sáng tác, tiếp tục sáng tác.
</reply>

<draft>
"brief hướng đi tiếp theo" hoàn chỉnh hiện tại, dùng Markdown: bắt đầu thẳng từ tiêu đề cấp hai, ví dụ "## Hướng đi tiếp theo"、"## Bước ngoặt then chốt"、"## Phục bút cần thu"、"## Nhịp độ và dung lượng"; liệt kê các ý bằng dấu đầu dòng. Mỗi lượt đều phải **cập nhật tích lũy** trên kết luận đã có, tiếp thu ý định mới nhất của người dùng; dù lượt này không có gì mới cũng phải viết lại nguyên vẹn brief đầy đủ một lần nữa——không được lược bỏ, không được viết placeholder kiểu "（giữ nguyên lượt trước）".
</draft>
`) + coCreateProtocolTail()
}

// coCreateProtocolTail là phần đuôi protocol output dùng chung cho cả hai chế độ đồng sáng tạo (<ready> / <suggestions> + quy phạm output).
// Hai chế độ chỉ khác ở ngữ cảnh mở đầu và ngữ nghĩa <draft>, protocol hoàn toàn giống nhau.
func coCreateProtocolTail() string {
	return contentlang.Pick(`
<ready>false</ready>

<suggestions>
1-3 条"用户接下来可能想说的话"，每行一条以 "- " 开头。这是用户卡壳时的引导，
按数字键填入输入框，用户可再编辑后发送。

要求：
- 站在用户口吻，像用户对你说的话，不要写成助手反问。
- 每条不超过 25 字，多样化句式，避免千篇一律。
- 给倾向 / 选择 / 补充意图，不要一句话替用户写完整设定。
</suggestions>

输出规范：
- 必须使用四个 XML 标签：<reply> / <draft> / <ready> / <suggestions>，每个都必须完整开闭。
- 标签名只能小写英文，不要改写成 <REPLY> / <REWRITE> / <回复> 等任何变体。
- 标签外不要添加任何说明、思考或代码围栏。
- <draft> 内允许多行 Markdown，直接换行书写，不需要任何转义。
- <ready> 只写 true 或 false。信息已足够时填 true。
- <ready>true</ready> 时 <suggestions> 可以为空（保留空标签 <suggestions></suggestions> 即可）。`, `
<ready>false</ready>

<suggestions>
1-3 "câu mà người dùng có thể muốn nói tiếp theo", mỗi dòng một câu bắt đầu bằng "- ". Đây là gợi ý khi người dùng bí,
nhấn phím số để điền vào ô nhập, người dùng có thể sửa lại rồi gửi.

Yêu cầu:
- Đứng ở giọng điệu người dùng, như lời người dùng nói với bạn, không viết thành câu hỏi ngược của trợ lý.
- Mỗi câu không quá 25 chữ, đa dạng cú pháp, tránh rập khuôn.
- Đưa ra xu hướng / lựa chọn / ý định bổ sung, không viết hết nguyên cả thiết lập thay người dùng trong một câu.
</suggestions>

Quy phạm đầu ra:
- Bắt buộc dùng bốn thẻ XML: <reply> / <draft> / <ready> / <suggestions>, mỗi thẻ đều phải mở đóng hoàn chỉnh.
- Tên thẻ chỉ được dùng chữ thường tiếng Anh, không viết lại thành <REPLY> / <REWRITE> / <回复> hay bất kỳ biến thể nào.
- Ngoài thẻ không thêm bất kỳ giải thích, suy nghĩ hay rào mã nào.
- Trong <draft> cho phép Markdown nhiều dòng, viết xuống dòng trực tiếp, không cần thoát ký tự.
- <ready> chỉ viết true hoặc false. Khi thông tin đã đủ thì điền true.
- Khi <ready>true</ready> thì <suggestions> có thể để trống (giữ thẻ rỗng <suggestions></suggestions> là được).`)
}

// CoCreateProgressKind xác định loại nội dung của callback stream.
const (
	CoCreateProgressThinking = "thinking"
	CoCreateProgressReply    = "reply"
)

// Output theo bốn đoạn dạng tag XML. Kiểu XML bền hơn marker dấu ngoặc vuông — trong dữ liệu huấn luyện của Claude/GPT
// có rất nhiều định dạng kiểu <thinking>...</thinking>, model hầu như không đổi <reply> thành <REWRITE>
// hay biến thể khác; tag đóng cũng giúp cắt giữa chừng khi stream chính xác hơn (không phải dựa vào việc tìm marker kế tiếp để kết đuôi).
const (
	tagReply       = "reply"
	tagDraft       = "draft"
	tagReady       = "ready"
	tagSuggestions = "suggestions"
)

func coCreateStream(ctx context.Context, models *bootstrap.ModelSet, sessions *store.SessionStore, sysPrompt string, history []CoCreateMessage, onProgress func(kind, text string)) (reply CoCreateReply, err error) {
	if len(history) == 0 {
		return CoCreateReply{}, fmt.Errorf("cocreate history is empty")
	}

	model := models.ForRole("thinking")
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	msgs := []agentcore.Message{agentcore.SystemMsg(sysPrompt)}
	for _, item := range history {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Role)) {
		case "assistant":
			msgs = append(msgs, assistantMsg(content))
		default:
			msgs = append(msgs, agentcore.UserMsg(content))
		}
	}

	var raw, thinking strings.Builder

	// Để rà soát các vấn đề lẻ tẻ như "cocreate empty response" cần thấy model thực tế trả về gì.
	// Mỗi lượt ghi toàn bộ xuống đĩa tại <output>/meta/sessions/cocreate.jsonl, cùng vị trí với log session của sáng tác chính thức.
	start := time.Now()
	defer func() {
		if sessions == nil {
			return
		}
		_ = sessions.LogCoCreate(coCreateLogEntry{
			Time:         time.Now(),
			DurationMS:   time.Since(start).Milliseconds(),
			InputHistory: history,
			RawResponse:  raw.String(),
			RawLen:       len([]rune(raw.String())),
			Thinking:     thinking.String(),
			ParsedReply:  reply.Message,
			ParsedDraft:  reply.Prompt,
			ParsedReady:  reply.Ready,
			ParsedSugs:   reply.Suggestions,
			Error:        errString(err),
		})
	}()

	streamCh, err := model.GenerateStream(ctx, msgs, nil, agentcore.WithMaxTokens(2048))
	if err != nil {
		return CoCreateReply{}, fmt.Errorf("cocreate generate: %w", err)
	}

	var streamed bool
	for ev := range streamCh {
		switch ev.Type {
		case agentcore.StreamEventThinkingDelta:
			thinking.WriteString(ev.Delta)
			if onProgress != nil {
				onProgress(CoCreateProgressThinking, thinking.String())
			}
		case agentcore.StreamEventTextDelta:
			streamed = true
			raw.WriteString(ev.Delta)
			if onProgress != nil {
				onProgress(CoCreateProgressReply, extractReplyPreview(raw.String()))
			}
		case agentcore.StreamEventDone:
			if !streamed {
				raw.WriteString(ev.Message.TextContent())
			}
		case agentcore.StreamEventError:
			if ev.Err != nil {
				return CoCreateReply{}, fmt.Errorf("cocreate generate: %w", ev.Err)
			}
			return CoCreateReply{}, fmt.Errorf("cocreate generate failed")
		}
	}

	// Channel fallback: model loại tư duy (R1/GLM-Z1/QwQ ...) thỉnh thoảng viết toàn bộ câu trả lời vào
	// reasoning_content rồi không chuyển về kênh final answer, khiến raw rỗng nhưng thinking chứa
	// đủ bốn đoạn. Thực nghiệm xem meta/sessions/cocreate.jsonl — lấy thẳng thinking làm raw để parse,
	// tầng protocol đã có xử lý hạ cấp (khi không có marker [REPLY] thì coi cả đoạn là reply), sau khi cứu vãn trải nghiệm UI không khác biệt.
	rawText := raw.String()
	if strings.TrimSpace(rawText) == "" {
		if t := strings.TrimSpace(thinking.String()); t != "" {
			rawText = t
		}
	}
	reply, err = parseCoCreateResponse(rawText)
	return reply, err
}

// coCreateLogEntry là cấu trúc một dòng ghi vào meta/sessions/cocreate.jsonl.
// Tên field bám sát thói quen tra trực tiếp jsonl (snake_case), tiện cho việc lọc bằng jq.
type coCreateLogEntry struct {
	Time         time.Time         `json:"time"`
	DurationMS   int64             `json:"duration_ms"`
	InputHistory []CoCreateMessage `json:"input_history"`
	RawResponse  string            `json:"raw_response"`
	RawLen       int               `json:"raw_len"`
	Thinking     string            `json:"thinking,omitempty"`
	ParsedReply  string            `json:"parsed_reply"`
	ParsedDraft  string            `json:"parsed_draft"`
	ParsedReady  bool              `json:"parsed_ready"`
	ParsedSugs   []string          `json:"parsed_sugs,omitempty"`
	Error        string            `json:"error,omitempty"`
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func assistantMsg(text string) agentcore.Message {
	return agentcore.Message{
		Role:      agentcore.RoleAssistant,
		Content:   []agentcore.ContentBlock{agentcore.TextBlock(text)},
		Timestamp: time.Now(),
	}
}

// parseCoCreateResponse parse output dạng tag XML. Nếu model không tuân thủ protocol (nói thẳng bằng ngôn ngữ tự nhiên),
// cả đoạn được hiển thị làm reply, draft để trống cho session giữ lại lượt trước.
func parseCoCreateResponse(raw string) (CoCreateReply, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CoCreateReply{}, fmt.Errorf("cocreate empty response")
	}

	reply, draft, ready, suggestions := splitCoCreateMarkers(raw)
	if reply == "" {
		// Model không tuân thủ protocol XML: cả đoạn làm reply.
		return CoCreateReply{Message: raw, Prompt: "", Ready: false, Raw: raw}, nil
	}
	return CoCreateReply{
		Message:     reply,
		Prompt:      draft,
		Ready:       ready,
		Suggestions: suggestions,
		Raw:         raw,
	}, nil
}

// splitCoCreateMarkers cắt văn bản theo bốn tag XML.
// Tag có thể thiếu (giữa chừng stream hoặc model bỏ sót), phần thiếu thì field tương ứng là rỗng / false / nil.
// Khi thiếu tag đóng, extractTagContent sẽ lấy tới cuối chuỗi, vẫn cố gắng parse hết sức.
func splitCoCreateMarkers(s string) (reply, draft string, ready bool, suggestions []string) {
	reply = extractTagContent(s, tagReply)
	draft = extractTagContent(s, tagDraft)
	readyStr := strings.ToLower(extractTagContent(s, tagReady))
	ready = readyStr == "true" || readyStr == "yes"
	suggestions = parseSuggestions(extractTagContent(s, tagSuggestions))
	return
}

// extractTagContent moi văn bản giữa <tag>...</tag> ra khỏi s.
// Có lưới đỡ cho ba kịch bản lỗi lẻ tẻ, tránh đi thẳng vào hạ cấp làm mất field:
//  1. Có mở không đóng (giữa chừng stream) → cắt tới trước tag mở đã biết kế tiếp
//  2. Không mở có đóng (model typo, ví dụ <suggestions> viết thành <uggestions>) → bắt đầu từ vị trí kết thúc của
//     tag đóng hoàn chỉnh đã biết gần nhất, tới trước </tag>
//  3. reply hoàn toàn không có tag mở (model mở đầu thẳng bằng ngôn ngữ tự nhiên, cuối dán </reply>) → từ đầu tới </reply>
func extractTagContent(s, tag string) string {
	open := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	oIdx := strings.Index(s, open)
	if oIdx >= 0 {
		rest := s[oIdx+len(open):]
		if cIdx := strings.Index(rest, closeTag); cIdx >= 0 {
			return strings.TrimSpace(rest[:cIdx])
		}
		// Có mở không đóng → cắt tới trước tag mở đã biết kế tiếp
		for _, other := range []string{"<reply>", "<draft>", "<ready>", "<suggestions>"} {
			if other == open {
				continue
			}
			if idx := strings.Index(rest, other); idx >= 0 {
				rest = rest[:idx]
			}
		}
		return strings.TrimSpace(rest)
	}

	// Không mở có đóng → bắt đầu từ vị trí kết thúc của tag đóng hoàn chỉnh đã biết gần nhất, tới </tag>.
	if cIdx := strings.Index(s, closeTag); cIdx >= 0 {
		prefix := s[:cIdx]
		start := 0
		for _, t := range []string{"</reply>", "</draft>", "</ready>", "</suggestions>"} {
			if t == closeTag {
				continue
			}
			if i := strings.LastIndex(prefix, t); i >= 0 {
				if end := i + len(t); end > start {
					start = end
				}
			}
		}
		return strings.TrimSpace(prefix[start:])
	}
	return ""
}

// parseSuggestions moi từng dòng của đoạn <suggestions> ra, bỏ các prefix danh sách như "- " / "* " / "1. ".
// Giữ tối đa 3 mục; bỏ qua dòng trống, quá ngắn (<2 ký tự), hoặc cả dòng trông như tag XML (tàn dư lưới đỡ tag mở typo,
// ví dụ <uggestions>).
func parseSuggestions(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Cả dòng trông như tag XML → bỏ qua (chống ô nhiễm do tag mở typo)
		if strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">") {
			continue
		}
		// Bóc prefix danh sách
		switch {
		case strings.HasPrefix(line, "- "):
			line = strings.TrimSpace(line[2:])
		case strings.HasPrefix(line, "* "):
			line = strings.TrimSpace(line[2:])
		case isOrderedSuggestion(line):
			line = stripOrderedPrefix(line)
		}
		if len([]rune(line)) < 2 {
			continue
		}
		out = append(out, line)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

// isOrderedSuggestion xét đầu dòng có dạng "1. " / "12. " hay không (số + dấu chấm + dấu cách).
func isOrderedSuggestion(line string) bool {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(line) && line[i] == '.' && line[i+1] == ' '
}

func stripOrderedPrefix(line string) string {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(line) {
		return line
	}
	return strings.TrimSpace(line[i+2:])
}

// extractReplyPreview xem trước theo stream: khi raw còn đang lớn dần thì đưa cho UI một đoạn văn bản có thể hiển thị.
// Tìm nội dung sau <reply>, cắt tới trước </reply> hoặc tag mở kế tiếp <draft>.
// Khi model tuân thủ nửa vời (sót tag mở <reply>), từ đầu tới </reply> hoặc <draft> đều tính là reply.
func extractReplyPreview(raw string) string {
	trimmed := strings.TrimSpace(raw)
	open := "<" + tagReply + ">"
	closeTag := "</" + tagReply + ">"
	draftOpen := "<" + tagDraft + ">"

	rest := trimmed
	if rIdx := strings.Index(trimmed, open); rIdx >= 0 {
		rest = trimmed[rIdx+len(open):]
	}
	if cIdx := strings.Index(rest, closeTag); cIdx >= 0 {
		return strings.TrimSpace(rest[:cIdx])
	}
	if dIdx := strings.Index(rest, draftOpen); dIdx >= 0 {
		rest = rest[:dIdx]
	}
	return strings.TrimSpace(rest)
}
