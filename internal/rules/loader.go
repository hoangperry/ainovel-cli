package rules

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// LoadOptions là tham số đầu vào của Load.
//
// File không tồn tại không tính là lỗi, loader bỏ qua im lặng; parse thất bại không chặn, conflicts do parser ghi vào Parsed.Conflicts.
type LoadOptions struct {
	// RulesFS là cây con assets/rules. Quy ước thư mục gốc chứa trực tiếp default.md.
	// Thường lấy qua fs.Sub(embedFS, "rules"); nil nghĩa là bỏ qua rule tích hợp.
	RulesFS fs.FS

	// HomeRulesDir là thư mục ~/.ainovel/rules/; loader quét mọi .md cấp cao nhất dưới đó (merge theo thứ tự từ điển tên file). Trống nghĩa là bỏ qua.
	HomeRulesDir string

	// ProjectRulesDir là thư mục ./.ainovel/rules/ (gương của toàn cục, cũng quét mọi .md cấp cao nhất dưới đó). Trống nghĩa là bỏ qua.
	ProjectRulesDir string
}

// Load đọc theo thứ tự Default → Global → Project, trả về danh sách Parsed đã sắp tăng dần.
//
// merger nhận giá trị trả về chỉ cần merge theo thứ tự danh sách, cái sau ghi đè cái trước.
// Không đưa vào load hai giai đoạn——các lớp mở rộng như Genre / Learned chưa mở khoét trước khi thực sự có nội dung.
func Load(opts LoadOptions) []Parsed {
	var layers []Parsed
	if p, ok := readFromFS(opts.RulesFS, "default.md", SourceDefault, "assets/rules/default.md"); ok {
		layers = append(layers, p)
	}
	layers = append(layers, readDirFromDisk(opts.HomeRulesDir, SourceGlobal)...)
	layers = append(layers, readDirFromDisk(opts.ProjectRulesDir, SourceProject)...)
	return layers
}

// readFromFS đọc và parse từ fs.FS; file không tồn tại trả về (Parsed{}, false).
// displayPath dùng cho Parsed.Source (tiện hiển thị trong sources/conflicts dưới dạng "assets/rules/...").
func readFromFS(fsys fs.FS, name string, kind SourceKind, displayPath string) (Parsed, bool) {
	if fsys == nil {
		return Parsed{}, false
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		// file không tồn tại thì bỏ qua im lặng; lỗi khác cũng không chặn (loader thiết kế không báo lỗi)
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return Parsed{}, false
		}
		// số rất hiếm lỗi IO: phơi ra như parse_error, tránh im lặng
		return Parsed{
			Source: displayPath,
			Kind:   kind,
			Conflicts: []Conflict{{
				Source: displayPath,
				Kind:   ConflictParseError,
				Detail: i18n.T("rules.load.read_failed") + err.Error(),
			}},
		}, true
	}
	return Parse(displayPath, kind, data), true
}

// readFromDisk đọc và parse từ đường dẫn tuyệt đối; đường dẫn rỗng hoặc file không tồn tại trả về (Parsed{}, false).
func readFromDisk(absPath string, kind SourceKind) (Parsed, bool) {
	if strings.TrimSpace(absPath) == "" {
		return Parsed{}, false
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Parsed{}, false
		}
		return Parsed{
			Source: absPath,
			Kind:   kind,
			Conflicts: []Conflict{{
				Source: absPath,
				Kind:   ConflictParseError,
				Detail: i18n.T("rules.load.read_failed") + err.Error(),
			}},
		}, true
	}
	return Parse(absPath, kind, data), true
}

// readDirFromDisk quét mọi file .md cấp cao nhất dưới thư mục (theo thứ tự từ điển tên file), parse từng cái thành Parsed.
// Thứ tự từ điển bảo đảm thứ tự merge của nhiều file cùng lớp ổn định, dự đoán được (cái sau ghi đè cái trước).
// Bỏ qua thư mục con và file ẩn/tạm của editor bắt đầu bằng . (như macOS ._x.md, emacs .#x.md),
// tránh inject nội dung nhị phân của file rác vào LLM như nội dung preference.
// Đường dẫn rỗng hoặc thư mục không tồn tại trả về nil (bỏ qua im lặng, nhất quán với việc thiếu file đơn);
// thư mục tồn tại nhưng đọc thất bại (quyền / đường dẫn thực ra là file) phơi ra ConflictParseError, không nuốt lỗi im lặng——
// giữ nhất quán với hợp đồng dung lỗi của readFromFS / readFromDisk.
// Không đệ quy thư mục con——giữ phẳng, tránh đưa vào phân cấp ngầm.
func readDirFromDisk(dir string, kind SourceKind) []Parsed {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []Parsed{{
			Source: dir,
			Kind:   kind,
			Conflicts: []Conflict{{
				Source: dir,
				Kind:   ConflictParseError,
				Detail: i18n.T("rules.load.dir_read_failed") + err.Error(),
			}},
		}}
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var out []Parsed
	for _, name := range names {
		if p, ok := readFromDisk(filepath.Join(dir, name), kind); ok {
			out = append(out, p)
		}
	}
	return out
}

// ainovelDirName là tên dotdir mà ainovel dùng chung ở cả hai cấp user / project.
// Toàn cục ~/.ainovel/rules/ và project ./.ainovel/rules/ đối xứng nhờ đó.
const ainovelDirName = ".ainovel"

// DefaultProjectRulesDir ghép ra đường dẫn tuyệt đối của ./.ainovel/rules/ (dựa trên thư mục project cho trước).
// Caller truyền vào thư mục gốc project, tránh để loader phụ thuộc cwd bên trong; gương của DefaultHomeRulesDir.
func DefaultProjectRulesDir(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	return filepath.Join(projectDir, ainovelDirName, "rules")
}

// DefaultHomeRulesDir ghép ra đường dẫn tuyệt đối của thư mục ~/.ainovel/rules/.
// home phân giải thất bại trả về chuỗi rỗng (caller dựa vào đó để bỏ qua nguồn này).
func DefaultHomeRulesDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ainovelDirName, "rules")
}

// homeRulesReadme là phần giải thích ghi vào ~/.ainovel/rules/README.txt khi dẫn nhập lần đầu.
// Cố ý dùng đuôi .txt thay vì .md——loader chỉ quét .md, phần giải thích này sẽ không bị inject vào LLM như rule.
// Là hàm (không phải var) để contentlang.Pick được phân giải lúc gọi, sau khi Set chạy lúc khởi động;
// var mức package sẽ đóng băng locale ở thời điểm init (trước Set).
func homeRulesReadme() string {
	return contentlang.Pick(`这里放全局写作偏好，跨所有书生效。

最简单：新建一个 .md 文件（如 my-style.md），用大白话写偏好就行——
不需要任何格式、不需要 YAML：

    # 角色
    - 主角林尘别写成圣母，外冷内热即可
    # 风格
    - 多用身体感知（指节发白）替代情绪标签（紧张）
    - 对话别太书面

这些会原样交给 editor 按语义审阅。多个 .md 按文件名字典序合并；
点开头的隐藏文件、非 .md 文件都会被忽略（所以这份 README.txt 不会被当成规则）。

进阶（可选）：想要"字数 / 禁词"这类硬性、确定的机械检查，
可在文件顶部加一段 YAML front matter——commit_chapter 会逐字计数、强制报错：

    ---
    chapter_words: 3000-6000          # 章节字数范围
    forbidden_phrases: ["某种程度上"]  # 禁用短语，出现即报错
    fatigue_words: {不禁: 1}           # 疲劳词，每章超阈值告警
    ---
    （下面照常写大白话偏好）

不写也没关系：常见 AI 套句、疲劳词的机械基线已内置，开箱即用。

加载优先级（高 → 低）：./.ainovel/rules/*.md（本书） > ~/.ainovel/rules/*.md（这里） > 内置默认
`, `Đây là nơi đặt preference viết toàn cục, có hiệu lực với mọi sách.

Đơn giản nhất: tạo một file .md (ví dụ my-style.md), viết preference bằng lời thường——
không cần bất kỳ định dạng nào, không cần YAML:

    # 角色
    - 主角林尘别写成圣母，外冷内热即可
    # 风格
    - 多用身体感知（指节发白）替代情绪标签（紧张）
    - 对话别太书面

Những nội dung này được giao nguyên văn cho editor để duyệt theo ngữ nghĩa. Nhiều file .md hợp nhất theo thứ tự từ điển của tên file;
file ẩn bắt đầu bằng dấu chấm và file không phải .md đều bị bỏ qua (nên README.txt này không bị coi là rule).

Nâng cao (tùy chọn): nếu muốn kiểm tra máy móc kiểu cứng, xác định như "số chữ / từ cấm",
có thể thêm một đoạn YAML front matter ở đầu file——commit_chapter sẽ đếm từng chữ và bắt buộc báo lỗi:

    ---
    chapter_words: 3000-6000          # phạm vi số chữ chương
    forbidden_phrases: ["某种程度上"]  # cụm từ cấm, xuất hiện là báo lỗi
    fatigue_words: {不禁: 1}           # từ gây mệt, mỗi chương vượt ngưỡng sẽ cảnh báo
    ---
    （下面照常写大白话偏好）

Không viết cũng không sao: baseline máy móc cho các câu sáo AI thường gặp và từ gây mệt đã được tích hợp sẵn, dùng ngay.

Thứ tự ưu tiên nạp (cao → thấp): ./.ainovel/rules/*.md (sách này) > ~/.ainovel/rules/*.md (ở đây) > mặc định tích hợp
`)
}

// EnsureHomeRulesDir cố gắng tạo thư mục ~/.ainovel/rules/ và ghi README.txt dẫn nhập,
// để người dùng phát hiện điểm mở rộng preference toàn cục này và biết cách viết.
// nice-to-have, không phải đường tới hạn: home phân giải thất bại hay ghi lỗi đều nuốt im lặng, tuyệt đối không chặn khởi động.
func EnsureHomeRulesDir() {
	if dir := DefaultHomeRulesDir(); dir != "" {
		_ = ensureRulesDirAt(dir)
	}
}

// ensureRulesDirAt tạo thư mục và ghi README.txt thành template dẫn nhập hiện tại, là nhân có thể test của EnsureHomeRulesDir.
// README.txt là file dẫn nhập do hệ thống sinh ra (preference người dùng viết trong *.md, nó không được loader load), mỗi lần đều ghi đè thành
// template mới nhất——không giữ nội dung cũ, nên cũng không cần bất kỳ logic tương thích phiên bản nào.
func ensureRulesDirAt(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "README.txt"), []byte(homeRulesReadme()), 0o644)
}

// DefaultOptions dựng LoadOptions thông dụng dựa trên thư mục làm việc hiện tại.
//
// Thích hợp gọi một lần khi Host khởi động, để ContextTool / CommitChapterTool tái dùng cùng một cấu hình.
// Khi phân giải cwd thất bại thì ProjectRulesDir để trống (loader sẽ bỏ qua nguồn này).
//
// Ngữ nghĩa đường dẫn: ProjectRulesDir gắn với **thư mục làm việc hiện tại (cwd)** chứ không phải outputDir.
// Người dùng cd sang thư mục khác để khởi động viết quyển khác, ./.ainovel/rules/ tự nhiên đi theo cwd; nếu cần dùng chung xuyên quyển,
// đặt vào thư mục toàn cục ~/.ainovel/rules/ là được (mọi .md dưới đó đều được load).
func DefaultOptions(rulesFS fs.FS) LoadOptions {
	cwd, _ := os.Getwd()
	return LoadOptions{
		RulesFS:         rulesFS,
		HomeRulesDir:    DefaultHomeRulesDir(),
		ProjectRulesDir: DefaultProjectRulesDir(cwd),
	}
}
