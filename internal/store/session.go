package store

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/voocel/agentcore"
)

// SessionStore ghi nối lịch sử hội thoại LLM vào file JSONL.
// Nội dung kích thước lớn (phần thân tiểu thuyết, context đầy đủ) được thay bằng dấu placeholder [session_compact: ...].
type SessionStore struct {
	io      *IO
	mu      sync.Mutex
	seq     map[string]int    // số thứ tự chạy của agent (dùng khi không trích được số chương)
	taskKey map[string]string // "agentName|task" → suffix, cùng một run tái dùng cùng một file
}

func NewSessionStore(io *IO) *SessionStore {
	return &SessionStore{io: io, seq: make(map[string]int), taskKey: make(map[string]string)}
}

// ModelLookup tra provider/model "đang hiệu lực lúc đó" theo tên agent khi logger ghi.
// Dùng kiểu func thay vì interface, để bên gọi tiện inject quy tắc chuẩn hóa bằng closure (như architect_short → architect).
// Trả về chuỗi rỗng nghĩa là không xác định, bên gọi vẫn ghi như thường nhưng không kèm _meta, lúc replay lùi về ModelSet fallback.
type ModelLookup func(agentName string) (provider, model string)

// CoordinatorLogger trả về callback OnMessage của Coordinator.
// lookup có thể là nil, khi đó ghi không kèm _meta (tương thích các kịch bản không có vai trò như cocreate).
func (s *SessionStore) CoordinatorLogger(lookup ModelLookup) func(agentcore.AgentMessage) {
	return func(msg agentcore.AgentMessage) {
		var meta *sessionLogMeta
		if lookup != nil {
			meta = lookupMeta(lookup, "coordinator")
		}
		if err := s.logEntry("meta/sessions/coordinator.jsonl", msg, meta); err != nil {
			slog.Warn("session log failed", "agent", "coordinator", "err", err)
		}
	}
}

// SubAgentLogger trả về callback OnMessage của subagent.
func (s *SessionStore) SubAgentLogger(lookup ModelLookup) func(agentName, task string, msg agentcore.AgentMessage) {
	return func(agentName, task string, msg agentcore.AgentMessage) {
		rel := s.subAgentPath(agentName, task)
		var meta *sessionLogMeta
		if lookup != nil {
			meta = lookupMeta(lookup, agentName)
		}
		if err := s.logEntry(rel, msg, meta); err != nil {
			slog.Warn("session log failed", "agent", agentName, "err", err)
		}
	}
}

func lookupMeta(lookup ModelLookup, agentName string) *sessionLogMeta {
	provider, model := lookup(agentName)
	if provider == "" && model == "" {
		return nil
	}
	return &sessionLogMeta{Provider: provider, Model: model}
}

// LogCoCreate thêm một log hội thoại đồng sáng tạo vào meta/sessions/cocreate.jsonl.
// Giai đoạn đồng sáng tạo chưa gắn với tiểu thuyết cụ thể, đều rơi xuống gốc mặc định của OutputDir (output/novel),
// đặt cùng vị trí với coordinator.jsonl / agents/* của sáng tác chính thức, tiện cho việc tra soát.
func (s *SessionStore) LogCoCreate(entry any) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal cocreate session: %w", err)
	}
	data = append(data, '\n')
	return s.io.AppendLine("meta/sessions/cocreate.jsonl", data)
}

// Log thêm một tin nhắn vào đường dẫn chỉ định, tự động nén nội dung lớn.
// Không kèm _meta (lối vào tương thích ngược; chỉ dùng cho các đường không có vai trò như cocreate).
func (s *SessionStore) Log(rel string, msg agentcore.AgentMessage) error {
	return s.logEntry(rel, msg, nil)
}

// sessionLogEntry nhúng agentcore.Message + _meta tùy chọn.
// agentcore.Message là plain struct (không có MarshalJSON), sau khi nhúng thì json marshal
// tự động trải lên tầng đỉnh; _meta được kiểm soát qua omitempty — chỉ khi assistant + Usage != nil
// mới inject, tin nhắn user/tool không kèm _meta, khi phân tích jsonl cũ thì _meta=nil là noop.
type sessionLogEntry struct {
	agentcore.Message
	Meta *sessionLogMeta `json:"_meta,omitempty"`
}

type sessionLogMeta struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// logEntry tuần tự hóa tin nhắn và gắn _meta khi cần. lookupMeta truyền vào meta đã tính sẵn;
// bên trong hàm quyết định chỉ ghi meta cho tin nhắn "đã phát sinh dùng lượng LLM" (assistant + Usage != nil),
// các tin nhắn khác giữ nguyên dạng tuần tự hóa agentcore.Message thuần túy.
func (s *SessionStore) logEntry(rel string, msg agentcore.AgentMessage, meta *sessionLogMeta) error {
	m, ok := msg.(agentcore.Message)
	if !ok {
		return nil // tin nhắn không phải LLM (như kiểu tùy chỉnh) bỏ qua
	}
	compacted := compactMessage(m)
	entry := sessionLogEntry{Message: compacted}
	if compacted.Role == agentcore.RoleAssistant && compacted.Usage != nil {
		entry.Meta = usageMeta(compacted.Usage)
		if entry.Meta == nil {
			entry.Meta = meta
		}
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal session message: %w", err)
	}
	data = append(data, '\n')
	return s.io.AppendLine(rel, data)
}

func usageMeta(usage *agentcore.Usage) *sessionLogMeta {
	if usage == nil || (usage.Provider == "" && usage.Model == "") {
		return nil
	}
	return &sessionLogMeta{
		Provider: usage.Provider,
		Model:    usage.Model,
	}
}

// subAgentPath sinh đường dẫn file dựa theo agentName+task.
func (s *SessionStore) subAgentPath(agentName, task string) string {
	suffix := extractChapter(task)
	if suffix != "" {
		return fmt.Sprintf("meta/sessions/agents/%s-%s.jsonl", agentName, suffix)
	}
	key := agentName + "|" + task
	s.mu.Lock()
	if cached, ok := s.taskKey[key]; ok {
		s.mu.Unlock()
		return fmt.Sprintf("meta/sessions/agents/%s-%s.jsonl", agentName, cached)
	}
	s.seq[agentName]++
	suffix = fmt.Sprintf("%03d", s.seq[agentName])
	s.taskKey[key] = suffix
	s.mu.Unlock()
	return fmt.Sprintf("meta/sessions/agents/%s-%s.jsonl", agentName, suffix)
}

var chapterRe = regexp.MustCompile(`第\s*(\d+)\s*章`)

func extractChapter(task string) string {
	m := chapterRe.FindStringSubmatch(task)
	if len(m) < 2 {
		return ""
	}
	n, _ := strconv.Atoi(m[1])
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("ch%02d", n)
}

// compactMessage nhân bản tin nhắn và thay thế nội dung lớn.
func compactMessage(m agentcore.Message) agentcore.Message {
	if len(m.Content) == 0 {
		return m
	}
	blocks := make([]agentcore.ContentBlock, len(m.Content))
	copy(blocks, m.Content)

	toolName := toolNameFromMeta(m.Metadata)

	for i := range blocks {
		switch blocks[i].Type {
		case agentcore.ContentText:
			blocks[i].Text = compactText(m.Role, toolName, blocks[i].Text)
		case agentcore.ContentToolCall:
			if blocks[i].ToolCall != nil {
				blocks[i].ToolCall = compactToolCall(blocks[i].ToolCall)
			}
		}
	}
	m.Content = blocks
	return m
}

func toolNameFromMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta["tool_name"].(string); ok {
		return v
	}
	return ""
}

// compactText nén text content của tool result.
func compactText(role agentcore.Role, toolName, text string) string {
	if role != agentcore.RoleTool || len(text) < 4096 {
		return text
	}
	switch toolName {
	case "novel_context":
		summary := extractJSONField(text, "_loading_summary")
		return fmt.Sprintf("[session_compact: novel_context %dB | %s]", len(text), summary)
	case "read_chapter":
		chars := utf8.RuneCountInString(text)
		return fmt.Sprintf("[session_compact: read_chapter %d字 | 见 chapters/]", chars)
	default:
		if len(text) > 8192 {
			chars := utf8.RuneCountInString(text)
			return fmt.Sprintf("[session_compact: %s %d字]", toolName, chars)
		}
		return text
	}
}

// compactToolCall nén các trường nội dung lớn trong args của tool call.
func compactToolCall(tc *agentcore.ToolCall) *agentcore.ToolCall {
	switch tc.Name {
	case "draft_chapter":
		return compactArgsContent(tc, "第N章正文", "drafts/")
	case "save_foundation":
		return compactFoundationArgs(tc)
	default:
		return tc
	}
}

func compactArgsContent(tc *agentcore.ToolCall, label, ref string) *agentcore.ToolCall {
	var args map[string]json.RawMessage
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		return tc
	}
	contentRaw, ok := args["content"]
	if !ok || len(contentRaw) < 4096 {
		return tc
	}
	var content string
	if err := json.Unmarshal(contentRaw, &content); err != nil {
		// content không phải chuỗi (có thể là object JSON), dùng số byte
		placeholder := fmt.Sprintf("[session_compact: %s %dB | 见 %s]", label, len(contentRaw), ref)
		args["content"], _ = json.Marshal(placeholder)
	} else {
		chars := utf8.RuneCountInString(content)
		ch := extractJSONFieldInt(tc.Args, "chapter")
		if ch > 0 {
			label = fmt.Sprintf("第%d章正文", ch)
			ref = fmt.Sprintf("drafts/%02d.draft.md", ch)
		}
		placeholder := fmt.Sprintf("[session_compact: %s %d字 | 见 %s]", label, chars, ref)
		args["content"], _ = json.Marshal(placeholder)
	}
	clone := *tc
	clone.Args, _ = json.Marshal(args)
	return &clone
}

func compactFoundationArgs(tc *agentcore.ToolCall) *agentcore.ToolCall {
	var args map[string]json.RawMessage
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		return tc
	}
	contentRaw, ok := args["content"]
	if !ok || len(contentRaw) < 4096 {
		return tc
	}
	typeName := "foundation"
	var t string
	if json.Unmarshal(args["type"], &t) == nil && t != "" {
		typeName = t
	}
	placeholder := fmt.Sprintf("[session_compact: %s %dB | 见 store]", typeName, len(contentRaw))
	args["content"], _ = json.Marshal(placeholder)
	clone := *tc
	clone.Args, _ = json.Marshal(args)
	return &clone
}

// extractJSONField trích giá trị chuỗi của trường chỉ định từ chuỗi JSON.
func extractJSONField(jsonStr, field string) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return ""
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var val string
	if err := json.Unmarshal(raw, &val); err != nil {
		return string(raw)
	}
	return val
}

func extractJSONFieldInt(data json.RawMessage, field string) int {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return 0
	}
	raw, ok := m[field]
	if !ok {
		return 0
	}
	var val int
	if err := json.Unmarshal(raw, &val); err != nil {
		return 0
	}
	return val
}

// CompactTag là tiền tố dấu placeholder, tiện cho tìm kiếm và phục hồi.
const CompactTag = "[session_compact:"

// IsCompacted kiểm tra văn bản đã bị nén hay chưa.
func IsCompacted(text string) bool {
	return strings.HasPrefix(text, CompactTag)
}
