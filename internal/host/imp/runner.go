package imp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
	"github.com/voocel/ainovel-cli/internal/tools"
)

// Deps truyền một lần các dependency cắm-rút mà runner cần, tiện mock khi test.
type Deps struct {
	Store      *store.Store
	CommitTool *tools.CommitChapterTool
	LLM        LLMChat // cùng một model là đủ, foundation/analyzer đều là suy ngược có cấu trúc
	Prompts    Prompts
}

// Prompts là hai đoạn prompt mà luồng imp sử dụng.
type Prompts struct {
	Foundation string // suy ngược foundation
	Analyzer   string // suy ngược một chương
}

// Run thực thi toàn bộ luồng import: split → foundation → chapter loop.
// Chạy trong goroutine của chính nó; kênh Events do hàm này đóng.
//
// Đánh đổi thiết kế:
//   - Toàn bộ luồng chạy chặn (tác vụ dài của CLI), caller chịu trách nhiệm mở goroutine lắng nghe kênh;
//   - Bất kỳ bước nào thất bại đều kết thúc ngay, phát sự kiện StageError;
//   - Giai đoạn chapter âm thầm bỏ qua các chương đã hoàn thành (idempotent của commit_chapter là phương án cuối, nhưng bỏ qua LLM tiết kiệm token hơn).
func Run(ctx context.Context, deps Deps, opts Options) (<-chan Event, error) {
	if deps.Store == nil || deps.CommitTool == nil || deps.LLM == nil {
		return nil, fmt.Errorf("deps incomplete")
	}
	if strings.TrimSpace(opts.SourcePath) == "" {
		return nil, fmt.Errorf("source path is required")
	}

	events := make(chan Event, 32)

	go func() {
		defer close(events)
		emit := func(stage Stage, current, total int, msg string, err error) {
			ev := Event{Time: time.Now(), Stage: stage, Current: current, Total: total, Message: msg, Err: err}
			select {
			case events <- ev:
			case <-ctx.Done():
			}
		}

		// ── 1. Cắt chương ──
		emit(StageSplitting, 0, 0, contentlang.Pick("切分章节...", "Đang cắt chương..."), nil)
		chapters, err := SplitFile(opts.SourcePath)
		if err != nil {
			emit(StageError, 0, 0, contentlang.Pick("切分失败", "Cắt chương thất bại"), err)
			return
		}
		total := len(chapters)
		if total == 0 {
			emit(StageError, 0, 0,
				contentlang.Pick(
					"未识别到任何章节：支持「第N章/回/话/卷/节/幕」「卷N」「序章/楔子/尾声/番外/外传」"+
						"「Chapter N / Prologue」等标题，兼容 Markdown #、全角空格、【】包裹与 GBK 编码。"+
						"请确认文件确为分章小说文本。",
					"Không nhận diện được chương nào: hỗ trợ các tiêu đề như 「第N章/回/话/卷/节/幕」「卷N」「序章/楔子/尾声/番外/外传」"+
						"「Chapter N / Prologue」, tương thích Markdown #, dấu cách toàn rộng, bao 【】 và mã hóa GBK. "+
						"Hãy xác nhận tệp đúng là văn bản tiểu thuyết đã chia chương."),
				fmt.Errorf("no chapters matched"))
			return
		}
		emit(StageSplitting, 0, total, fmt.Sprintf(contentlang.Pick("切分完成：%d 章", "Cắt chương xong: %d chương"), total), nil)

		// ── 2. Suy ngược Foundation (bỏ qua khi đã đầy đủ) ──
		if needsFoundation(deps.Store, opts) {
			emit(StageFoundation, 0, total, contentlang.Pick("反推 Foundation 中（一次 LLM 调用）...", "Đang suy ngược Foundation (một lần gọi LLM)..."), nil)
			fr, err := ReverseFoundation(ctx, deps.LLM, deps.Prompts.Foundation, chapters)
			if err != nil {
				emit(StageError, 0, total, contentlang.Pick("Foundation 反推失败", "Suy ngược Foundation thất bại"), err)
				return
			}
			scale := pickScale(total)
			if err := PersistFoundation(ctx, deps.Store, scale, fr); err != nil {
				emit(StageError, 0, total, contentlang.Pick("Foundation 落盘失败", "Ghi Foundation xuống đĩa thất bại"), err)
				return
			}
			emit(StageFoundation, 0, total,
				fmt.Sprintf(contentlang.Pick("Foundation 就绪：%d 角色 / %d 规则 / %d 章大纲（第一卷）", "Foundation sẵn sàng: %d nhân vật / %d quy tắc / %d chương dàn ý (quyển một)"),
					len(fr.Characters), len(fr.WorldRules), len(domain.FlattenOutline(fr.Volumes))),
				nil)
		} else {
			emit(StageFoundation, 0, total, contentlang.Pick("Foundation 已存在，跳过反推", "Foundation đã tồn tại, bỏ qua suy ngược"), nil)
		}

		// ── 3. Vòng lặp chương ──
		premise, _ := deps.Store.Outline.LoadPremise()
		charactersBlock := loadCharactersBlock(deps.Store)

		startIdx := 0
		if opts.ResumeFrom > 1 {
			startIdx = opts.ResumeFrom - 1
		}
		for i := startIdx; i < total; i++ {
			if err := ctx.Err(); err != nil {
				emit(StageError, i+1, total, contentlang.Pick("用户取消", "Người dùng hủy"), err)
				return
			}
			chNum := i + 1
			ch := chapters[i]

			// Đã hoàn thành → bỏ qua LLM
			if deps.Store.Progress.IsChapterCompleted(chNum) {
				emit(StageChapter, chNum, total, fmt.Sprintf(contentlang.Pick("第 %d 章已完成，跳过", "Chương %d đã hoàn thành, bỏ qua"), chNum), nil)
				continue
			}

			emit(StageChapter, chNum, total, fmt.Sprintf(contentlang.Pick("分析第 %d/%d 章：%s", "Phân tích chương %d/%d: %s"), chNum, total, ch.Title), nil)

			activeHooks, _ := deps.Store.World.LoadActiveForeshadow()
			analysis, err := AnalyzeChapter(ctx, deps.LLM, deps.Prompts.Analyzer,
				chNum, ch.Title, ch.Content, premise, charactersBlock, activeHooks)
			if err != nil {
				emit(StageError, chNum, total, fmt.Sprintf(contentlang.Pick("第 %d 章分析失败", "Phân tích chương %d thất bại"), chNum), err)
				return
			}

			if err := PersistChapter(ctx, deps.Store, deps.CommitTool, chNum, ch.Title, ch.Content, analysis); err != nil {
				emit(StageError, chNum, total, fmt.Sprintf(contentlang.Pick("第 %d 章落盘失败", "Ghi chương %d xuống đĩa thất bại"), chNum), err)
				return
			}
			emit(StageChapter, chNum, total, fmt.Sprintf(contentlang.Pick("第 %d 章导入完成", "Nhập chương %d xong"), chNum), nil)
		}

		emit(StageDone, total, total, fmt.Sprintf(contentlang.Pick("导入完成：%d 章", "Nhập xong: %d chương"), total), nil)
	}()

	return events, nil
}

// needsFoundation phán định có cần suy ngược lại foundation hay không.
// Người dùng đặt ResumeFrom > 1 tường minh thì coi là "import tiếp", bỏ qua suy ngược; nếu không thì phán định theo trạng thái Store.
func needsFoundation(st *store.Store, opts Options) bool {
	if opts.ResumeFrom > 1 {
		return false
	}
	return len(st.FoundationMissing()) > 0
}

// pickScale dựa vào số chương cho cấp lập kế hoạch một giá trị khởi tạo hợp lý; short ≤25, mid ≤80, còn lại long.
// Không ảnh hưởng tới bản thân import, chỉ ảnh hưởng việc Coordinator chọn prompt architect khi viết tiếp sau này.
func pickScale(total int) domain.PlanningTier {
	switch {
	case total <= 25:
		return domain.PlanningTierShort
	case total <= 80:
		return domain.PlanningTierMid
	default:
		return domain.PlanningTierLong
	}
}

// loadCharactersBlock render hồ sơ nhân vật thành khối văn bản ngắn (name/role + một câu mô tả),
// chỉ để tham chiếu trong context của LLM, không cần cấu trúc chặt.
func loadCharactersBlock(st *store.Store) string {
	chars, err := st.Characters.Load()
	if err != nil || len(chars) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, c := range chars {
		fmt.Fprintf(&sb, "- **%s**（%s）：%s\n", c.Name, c.Role, oneLine(c.Description))
	}
	return sb.String()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
