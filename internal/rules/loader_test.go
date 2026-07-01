package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// TestLoad_ThreeLayers kiểm tra ba lớp Default + Global + Project đứng đúng chỗ trong thứ tự tăng dần.
func TestLoad_ThreeLayers(t *testing.T) {
	rulesFS := fstest.MapFS{
		"default.md": {Data: []byte("---\nchapter_words: 3000-6000\n---\n")},
	}
	tmp := t.TempDir()
	globalDir := filepath.Join(tmp, "rules")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(tmp, "project-rules")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "global.md"), []byte("---\nforbidden_chars:\n  - \"——\"\n---\n# 全局偏好\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.md"), []byte("---\nchapter_words: 4000-8000\n---\n# 项目偏好\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	layers := Load(LoadOptions{
		RulesFS:         rulesFS,
		HomeRulesDir:    globalDir,
		ProjectRulesDir: projectDir,
	})

	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d: %+v", len(layers), layers)
	}
	expectKinds := []SourceKind{SourceDefault, SourceGlobal, SourceProject}
	for i, want := range expectKinds {
		if layers[i].Kind != want {
			t.Errorf("layer[%d].Kind=%v, want %v", i, layers[i].Kind, want)
		}
	}
	// sau Merge thì chapter_words của project phải thắng
	b := Merge(layers)
	if b.Structured.ChapterWords == nil || b.Structured.ChapterWords.Min != 4000 {
		t.Errorf("project chapter_words should win, got %+v", b.Structured.ChapterWords)
	}
	// forbidden_chars do global đóng góp được giữ lại khi project chưa khai báo
	if len(b.Structured.ForbiddenChars) != 1 || b.Structured.ForbiddenChars[0] != "——" {
		t.Errorf("global forbidden_chars should propagate, got %v", b.Structured.ForbiddenChars)
	}
	if !strings.Contains(b.Preferences, "全局偏好") || !strings.Contains(b.Preferences, "项目偏好") {
		t.Errorf("merged preferences missing body: %q", b.Preferences)
	}
}

func TestLoad_GenreFieldIsPassThrough(t *testing.T) {
	// Phase 1.1: genre chỉ truyền thẳng như một trường, không còn kích hoạt load assets/rules/genres/.
	// Dù trong fs có đặt genres/xianxia.md thì cũng không được đọc ra.
	rulesFS := fstest.MapFS{
		"default.md":        {Data: []byte("")},
		"genres/xianxia.md": {Data: []byte("---\nforbidden_chars:\n  - \"——\"\n---\n")},
	}
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project-rules")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "project.md"), []byte("---\ngenre: xianxia\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	layers := Load(LoadOptions{
		RulesFS:         rulesFS,
		ProjectRulesDir: projectDir,
	})

	// kỳ vọng chỉ có default + project, không có lớp genre
	if len(layers) != 2 {
		t.Fatalf("expected 2 layers (no genre loading), got %d: %+v", len(layers), layers)
	}
	b := Merge(layers)
	if b.Structured.Genre != "xianxia" {
		t.Errorf("genre field should be passed through, got %q", b.Structured.Genre)
	}
	// file genre không được load → không được có "——" đến từ file thể loại
	if len(b.Structured.ForbiddenChars) != 0 {
		t.Errorf("genres/*.md must not be auto-loaded in Phase 1.1, got %v", b.Structured.ForbiddenChars)
	}
}

func TestLoad_NilFSDoesNotPanic(t *testing.T) {
	// tham số toàn rỗng: không crash, trả về layers rỗng
	layers := Load(LoadOptions{})
	if len(layers) != 0 {
		t.Errorf("expected 0 layers, got %d", len(layers))
	}
}

func TestLoad_OnlyDefault(t *testing.T) {
	// chỉ rule mặc định tích hợp của project khả dụng, người dùng thiếu cả hai file
	rulesFS := fstest.MapFS{
		"default.md": {Data: []byte("---\nchapter_words: 3000-6000\n---\n")},
	}
	layers := Load(LoadOptions{RulesFS: rulesFS})
	if len(layers) != 1 || layers[0].Kind != SourceDefault {
		t.Errorf("expected only default layer, got %+v", layers)
	}
}

// TestLoad_GlobalDirScansAllMarkdown kiểm tra nhiều .md dưới thư mục global đều được load,
// merge theo thứ tự từ điển tên file (cái sau ghi đè cái trước), file không phải .md bị bỏ qua.
func TestLoad_GlobalDirScansAllMarkdown(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nchapter_words: 1000-2000\n---\n# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nchapter_words: 3000-4000\n---\n# B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// file không phải .md phải bị bỏ qua
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("not a rule"), 0o644); err != nil {
		t.Fatal(err)
	}

	layers := Load(LoadOptions{HomeRulesDir: dir})
	if len(layers) != 2 {
		t.Fatalf("expected 2 global layers (a.md, b.md), got %d", len(layers))
	}
	for _, p := range layers {
		if p.Kind != SourceGlobal {
			t.Errorf("dir files should be SourceGlobal, got %v", p.Kind)
		}
	}
	// thứ tự từ điển a trước b sau, sau merge thì b ghi đè a
	b := Merge(layers)
	if b.Structured.ChapterWords == nil || b.Structured.ChapterWords.Min != 3000 {
		t.Errorf("later file (b.md) should win on chapter_words, got %+v", b.Structured.ChapterWords)
	}
	if !strings.Contains(b.Preferences, "# A") || !strings.Contains(b.Preferences, "# B") {
		t.Errorf("both files' preferences should be merged, got %q", b.Preferences)
	}
}

// TestLoad_GlobalDirMissing kiểm tra khi thư mục global không tồn tại thì bỏ qua im lặng.
func TestLoad_GlobalDirMissing(t *testing.T) {
	layers := Load(LoadOptions{HomeRulesDir: filepath.Join(t.TempDir(), "does-not-exist")})
	if len(layers) != 0 {
		t.Errorf("missing global dir should yield 0 layers, got %d", len(layers))
	}
}

// TestLoad_GlobalDirIgnoresHiddenAndSubdirs chốt chặt: file ẩn/tạm của editor (bắt đầu bằng .) bị bỏ qua,
// thư mục con không đệ quy——ngăn nội dung nhị phân của file rác bị inject vào LLM như nội dung preference.
func TestLoad_GlobalDirIgnoresHiddenAndSubdirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real.md"), []byte("---\nchapter_words: 3000-6000\n---\n# real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// macOS AppleDouble / lock emacs / file ẩn thông thường —— đều phải bị bỏ qua
	for _, dirty := range []string{"._real.md", ".#lock.md", ".hidden.md"} {
		if err := os.WriteFile(filepath.Join(dir, dirty), []byte("\x00binary garbage\x00"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// .md trong thư mục con không được đệ quy
	if err := os.WriteFile(filepath.Join(dir, "sub", "nested.md"), []byte("---\nchapter_words: 1-2\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	layers := Load(LoadOptions{HomeRulesDir: dir})
	if len(layers) != 1 {
		t.Fatalf("expected only real.md loaded (hidden/dirty/subdir ignored), got %d layers", len(layers))
	}
	if layers[0].Source != filepath.Join(dir, "real.md") {
		t.Errorf("loaded wrong file: %s", layers[0].Source)
	}
	if b := Merge(layers); strings.Contains(b.Preferences, "garbage") || strings.Contains(b.Preferences, "\x00") {
		t.Errorf("dirty file content leaked into preferences: %q", b.Preferences)
	}
}

// TestLoad_GlobalDirIsFileExposesConflict kiểm tra khi đường dẫn rules bị tạo nhầm thành file (không phải thư mục) thì
// phơi ra conflict thay vì nuốt lỗi im lặng——nhất quán với hợp đồng dung lỗi của lỗi IO file đơn.
func TestLoad_GlobalDirIsFileExposesConflict(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rules")
	if err := os.WriteFile(p, []byte("oops, should be a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	layers := Load(LoadOptions{HomeRulesDir: p})
	if len(layers) != 1 || len(layers[0].Conflicts) == 0 {
		t.Fatalf("expected 1 layer carrying a conflict, got %+v", layers)
	}
	if layers[0].Conflicts[0].Kind != ConflictParseError {
		t.Errorf("expected ConflictParseError, got %v", layers[0].Conflicts[0].Kind)
	}
}

// TestEnsureRulesDirAt kiểm tra chuẩn bị sẵn thư mục + README.txt: ghi phần giải thích, luôn ghi đè thành template mới nhất,
// và README.txt (không phải .md) sẽ không bị loader coi là rule.
func TestEnsureRulesDirAt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rules")
	if err := ensureRulesDirAt(dir); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(dir, "README.txt")
	data, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("README.txt should be written: %v", err)
	}
	if !strings.Contains(string(data), "front matter") {
		t.Errorf("README.txt missing guidance, got %q", data)
	}

	// luôn ghi đè thành template mới nhất: văn bản lỗi thời do bản cũ ghi (như đường dẫn vẫn là ./rules.md) bị làm mới khi ensure lại
	if err := os.WriteFile(readme, []byte("旧版本写的 ./rules.md 文案"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureRulesDirAt(dir); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(readme); string(again) != homeRulesReadme() {
		t.Errorf("README.txt should be refreshed to latest template, got %q", again)
	}

	// README.txt không bị coi là rule (loader chỉ quét .md)
	if layers := Load(LoadOptions{HomeRulesDir: dir}); len(layers) != 0 {
		t.Errorf("README.txt must not be loaded as a rule, got %d layers", len(layers))
	}
}

// TestDefaultProjectRulesDir chốt chặt thư mục rule cấp project là gương của toàn cục: ./.ainovel/rules/.
func TestDefaultProjectRulesDir(t *testing.T) {
	proj := filepath.Join("/tmp", "demo-book")
	want := filepath.Join(proj, ".ainovel", "rules")
	if got := DefaultProjectRulesDir(proj); got != want {
		t.Errorf("DefaultProjectRulesDir=%q, want %q", got, want)
	}
	if got := DefaultProjectRulesDir(""); got != "" {
		t.Errorf("空项目根应返回空串，得到 %q", got)
	}
}

// TestDefaultOptions_LoadsProjectRulesFromDotAinovel kiểm tra đầu-cuối:
// DefaultOptions đấu ./.ainovel/rules/ dưới cwd vào nguồn SourceProject.
func TestDefaultOptions_LoadsProjectRulesFromDotAinovel(t *testing.T) {
	proj := t.TempDir()
	t.Chdir(proj)
	rulesDir := filepath.Join(proj, ".ainovel", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "book.md"), []byte("---\nchapter_words: 4000-8000\n---\n# 本书偏好\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rulesFS := fstest.MapFS{"default.md": {Data: []byte("---\nchapter_words: 3000-6000\n---\n")}}
	layers := Load(DefaultOptions(rulesFS))

	var got *Parsed
	for i := range layers {
		if layers[i].Kind == SourceProject {
			got = &layers[i]
		}
	}
	if got == nil {
		t.Fatalf("应从 ./.ainovel/rules/ 加载到项目规则层，得到 %+v", layers)
	}
	if b := Merge(layers); b.Structured.ChapterWords == nil || b.Structured.ChapterWords.Min != 4000 {
		t.Errorf("项目规则应覆盖默认 chapter_words，得到 %+v", b.Structured.ChapterWords)
	}
}
