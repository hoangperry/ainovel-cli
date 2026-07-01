package diag

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/store"
)

// sentinel là một đoạn "chính văn tiểu thuyết" tuyệt đối không được xuất hiện trong bản export.
const sentinel = "雪夜里主角揭穿了反派的惊天阴谋这是机密正文"

// writeSession ghi một số message theo định dạng sessions/*.jsonl vào thư mục output tạm.
func writeSession(t *testing.T, rel string, msgs []agentcore.Message) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "meta", "sessions", rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var b strings.Builder
	for _, m := range msgs {
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func commitCall(chapterRaw string) agentcore.Message {
	args := json.RawMessage(`{"chapter":` + chapterRaw + `,"content":"` + sentinel + sentinel + `"}`)
	return agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.ToolCallBlock(agentcore.ToolCall{Name: "commit_chapter", Args: args})},
	}
}

func errResult(msg string) agentcore.Message {
	return agentcore.Message{
		Role:     agentcore.RoleTool,
		Content:  []agentcore.ContentBlock{agentcore.TextBlock(msg)},
		Metadata: map[string]any{"is_error": true},
	}
}

// TestExport_DeathLoopShape tái hiện end-to-end #34: model chuỗi-hoá tham số chapter của commit_chapter
// gây vòng lặp kiểm tra. Khẳng định bản export định vị được, và chính văn tiểu thuyết không ra gói.
func TestExport_DeathLoopShape(t *testing.T) {
	// Khẳng định ở dưới so khớp tiêu đề Finding zh nguyên gốc nên đặt nội dung về "zh".
	contentlang.Set("zh")
	defer contentlang.Set("vi")

	var msgs []agentcore.Message
	// Một đoạn chính văn kế hoạch coordinator trần (<4KB, né session_compact), bắt buộc bị che.
	msgs = append(msgs, agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(sentinel)},
	})
	// 14 vòng commit_chapter(chapter:"7") + InputValidationError.
	for range 14 {
		msgs = append(msgs, commitCall(`"7"`))
		msgs = append(msgs, errResult("InputValidationError: chapter must be int"))
	}

	dir := writeSession(t, "coordinator.jsonl", msgs)
	s := store.NewStore(dir)
	rep, rc := Diagnose(s)
	out := string(RenderExport(rep, rc))

	if strings.Contains(out, sentinel) {
		t.Fatalf("小说正文出包了！导出包含 sentinel:\n%s", out)
	}
	if !strings.Contains(out, `chapter: "7"`) {
		t.Errorf("缺类型异常信号 chapter: \"7\"（#34 根因）\n%s", out)
	}
	if !strings.Contains(out, "InputValidationError") {
		t.Errorf("错误串未保留\n%s", out)
	}
	if !strings.Contains(out, "×14") {
		t.Errorf("重复聚合未列出 ×14\n%s", out)
	}
	// Phase 2: phát hiện trạng thái chạy phải phán vòng lặp này thành RepeatedToolError mức critical.
	if !strings.Contains(out, "工具反复报同一错误") {
		t.Errorf("运行时检测未产出 RepeatedToolError\n%s", out)
	}
	if !strings.Contains(out, "[critical]") {
		t.Errorf("14 次重复应升为 critical\n%s", out)
	}
}

// TestExport_NumberVsStringArg chứng minh phép chiếu scalar và chuỗi phân biệt được kiểu:
// chapter:7 (số) giữ thành 7, chapter:"7" (chuỗi) giữ thành "7".
func TestExport_NumberVsStringArg(t *testing.T) {
	intDir := writeSession(t, "coordinator.jsonl", []agentcore.Message{commitCall(`7`)})
	si := store.NewStore(intDir)
	repInt, rcInt := Diagnose(si)
	outInt := string(RenderExport(repInt, rcInt))
	if !strings.Contains(outInt, "chapter: 7") || strings.Contains(outInt, `chapter: "7"`) {
		t.Errorf("数字参数应渲染为 chapter: 7（不带引号）\n%s", outInt)
	}
}

// TestProjectValue_ProseArgRedacted canh giữ biên khử nhạy cảm: giá trị ngắn kiểu định danh được giữ,
// giá trị ngắn tiếng Trung/có khoảng trắng (như dispatch task, chapter title) đều bị che.
func TestProjectValue_ProseArgRedacted(t *testing.T) {
	keep := map[string]string{
		`"7"`:       `"7"`,       // Số bị chuỗi-hoá (tín hiệu #34)
		`"premise"`: `"premise"`, // enum
		`"writer"`:  `"writer"`,  // Tên role
		`7`:         `7`,         // Scalar số
		`true`:      `true`,      // Scalar bool
	}
	for in, want := range keep {
		if got := projectValue([]byte(in)); got != want {
			t.Errorf("应保留 %s：got %q want %q", in, got, want)
		}
	}
	// Chứa tiếng Trung / khoảng trắng → bắt buộc bị che, và không chứa nguyên văn.
	prose := []string{`"第7章 雪夜的真相"`, `"雪夜杀机"`, `"主角揭穿阴谋"`}
	for _, in := range prose {
		got := projectValue([]byte(in))
		if !strings.HasPrefix(got, "<redacted") {
			t.Errorf("中文/带空格短值应打码：%s → %q", in, got)
		}
		if strings.Contains(got, "雪夜") || strings.Contains(got, "主角") {
			t.Errorf("打码后仍含正文：%s → %q", in, got)
		}
	}
}

// TestWriteExport_WritesFile chứng minh đường thuần hàm: không phụ thuộc TUI, ghi ra đường dẫn tương đối cố định.
func TestWriteExport_WritesFile(t *testing.T) {
	dir := writeSession(t, "coordinator.jsonl", []agentcore.Message{commitCall(`"7"`), errResult("boom")})
	s := store.NewStore(dir)

	rep, rc := Diagnose(s)
	path, err := WriteExport(s, rep, rc)
	if err != nil {
		t.Fatalf("WriteExport: %v", err)
	}
	if want := filepath.Join(dir, filepath.FromSlash(ExportRelPath)); path != want {
		t.Errorf("路径不对：got %s want %s", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(data), "diag-export") {
		t.Errorf("文件内容异常\n%s", data)
	}
	if strings.Contains(string(data), sentinel) {
		t.Errorf("写出的文件夹带正文")
	}
}

// TestRedactMessage_DupSha chứng minh cùng một đoạn văn bản xuất hiện lặp lại sinh ra cùng sha (tín hiệu vòng lặp).
func TestRedactMessage_DupSha(t *testing.T) {
	a := redactMessage("coordinator", agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(sentinel)},
	})
	b := redactMessage("coordinator", agentcore.Message{
		Role:    agentcore.RoleAssistant,
		Content: []agentcore.ContentBlock{agentcore.TextBlock(sentinel)},
	})
	if a.TextSha == "" || a.TextSha != b.TextSha {
		t.Errorf("相同正文应得相同 sha：%q vs %q", a.TextSha, b.TextSha)
	}
	if a.Redacted != 1 {
		t.Errorf("应打码 1 个文本块，got %d", a.Redacted)
	}
}
