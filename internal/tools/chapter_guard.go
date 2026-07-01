package tools

import (
	"fmt"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/errs"
	"github.com/voocel/ainovel-cli/internal/store"
)

// EnsureChapterExpanded verifies that a chapter is inside the currently
// expanded layered outline. Non-layered books and non-writing phases have no
// layered range constraint.
func EnsureChapterExpanded(st *store.Store, chapter int) error {
	if st == nil || chapter <= 0 {
		return nil
	}
	progress, err := st.Progress.Load()
	if err != nil {
		return fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	if progress == nil || !progress.Layered || progress.Phase != domain.PhaseWriting {
		return nil
	}
	boundary, err := st.Outline.CheckArcBoundary(chapter)
	if err != nil {
		return fmt.Errorf("check layered outline: %w: %w", errs.ErrStoreRead, err)
	}
	if boundary != nil {
		return nil
	}
	return fmt.Errorf("%s: %w", contentlang.Pick(
		fmt.Sprintf("第 %d 章不在分层大纲范围内：写作必须先 expand_arc 扩展弧或 append_volume 追加卷；若全书已完结请调 save_foundation type=complete_book", chapter),
		fmt.Sprintf("Chương %d không nằm trong phạm vi dàn ý phân tầng: viết phải expand_arc mở rộng cung hoặc append_volume thêm quyển trước; nếu toàn bộ truyện đã hoàn kết hãy gọi save_foundation type=complete_book", chapter),
	), errs.ErrToolPrecondition)
}
