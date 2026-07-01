package startup

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// CoCreateSession đảm nhận trạng thái phi UI của chế độ đồng sáng tác.
type CoCreateSession struct {
	history        []host.CoCreateMessage
	draftPrompt    string
	ready          bool
	streamReply    string
	streamThinking string
	suggestions    []string
}

func NewCoCreateSession(initial string) *CoCreateSession {
	return &CoCreateSession{
		history: []host.CoCreateMessage{
			{Role: "user", Content: strings.TrimSpace(initial)},
		},
	}
}

func (s *CoCreateSession) History() []host.CoCreateMessage {
	if s == nil {
		return nil
	}
	return append([]host.CoCreateMessage(nil), s.history...)
}

func (s *CoCreateSession) ApplyReply(reply host.CoCreateReply) {
	if s == nil {
		return
	}
	s.streamReply = ""
	s.streamThinking = ""
	// Trong history, assistant lưu đầy đủ ba đoạn Raw (gồm [DRAFT]), vòng sau model mới
	// thấy được bản nháp mình viết vòng trước, cập nhật tích lũy dựa trên nó; chỉ lưu
	// Message sẽ làm [DRAFT] hoàn toàn không vào context, mỗi vòng model chỉ có thể quy
	// nạp lại từ hội thoại, chi tiết ban đầu dễ mất. Trong đường suy giảm Raw == Message, tương đương.
	text := strings.TrimSpace(reply.Raw)
	if text == "" {
		text = strings.TrimSpace(reply.Message)
	}
	if text != "" {
		s.history = append(s.history, host.CoCreateMessage{Role: "assistant", Content: text})
	}
	// Chỉ ghi đè draft khi Prompt khác rỗng: đường suy giảm parse sẽ trả Prompt="", lúc đó
	// phải giữ draft vòng trước, nếu không "chỉ thị sáng tác hiện tại" mà người dùng đã tích lũy sẽ bị reply bị cắt cụt xóa sạch.
	if prompt := strings.TrimSpace(reply.Prompt); prompt != "" {
		s.draftPrompt = prompt
	}
	s.ready = reply.Ready
	// suggestions ghi đè trực tiếp (kể cả ghi đè thành rỗng): gợi ý mỗi vòng chỉ có ý nghĩa cho hiện tại.
	s.suggestions = append(s.suggestions[:0], reply.Suggestions...)
}

func (s *CoCreateSession) AppendUser(text string) {
	if s == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	// Người dùng đã quyết định câu kế nói gì, suggestions lập tức vô hiệu, tránh khi AI chưa reply
	// mà gợi ý cũ treo trên ô nhập gây hiểu nhầm.
	s.suggestions = nil
	s.history = append(s.history, host.CoCreateMessage{Role: "user", Content: text})
}

// ApplyDelta nhận tích lũy stream; kind="thinking" ghi vào luồng suy luận, "reply" ghi vào xem trước reply.
// Hai luồng tích lũy riêng, UI có thể tô màu hiển thị theo khối, để người dùng ở giai đoạn thinking cũng thấy LLM đang làm việc.
func (s *CoCreateSession) ApplyDelta(kind, text string) {
	if s == nil {
		return
	}
	text = strings.TrimSpace(text)
	switch kind {
	case host.CoCreateProgressThinking:
		s.streamThinking = text
	case host.CoCreateProgressReply:
		s.streamReply = text
	}
}

func (s *CoCreateSession) StreamReply() string {
	if s == nil {
		return ""
	}
	return s.streamReply
}

func (s *CoCreateSession) StreamThinking() string {
	if s == nil {
		return ""
	}
	return s.streamThinking
}

func (s *CoCreateSession) DraftPrompt() string {
	if s == nil {
		return ""
	}
	return s.draftPrompt
}

func (s *CoCreateSession) Suggestions() []string {
	if s == nil {
		return nil
	}
	return s.suggestions
}

func (s *CoCreateSession) Ready() bool {
	if s == nil {
		return false
	}
	return s.ready
}

func (s *CoCreateSession) CanStart() bool {
	return strings.TrimSpace(s.DraftPrompt()) != ""
}

func (s *CoCreateSession) InitialInput() string {
	if s == nil || len(s.history) == 0 {
		return ""
	}
	return strings.TrimSpace(s.history[0].Content)
}

func (s *CoCreateSession) BuildPlan() (Plan, error) {
	if s == nil || !s.CanStart() {
		return Plan{}, fmt.Errorf("cocreate draft prompt is required")
	}
	return Plan{
		Mode:        ModeCoCreate,
		DisplayName: i18n.T("startup.mode.cocreate"),
		StartPrompt: host.BuildStartPrompt(s.DraftPrompt()),
	}, nil
}
