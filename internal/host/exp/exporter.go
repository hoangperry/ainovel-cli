package exp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// Run thực thi một lần export. Trả về đồng bộ, lượng IO nhỏ (đọc/ghi file cục bộ).
//
// Ngữ nghĩa thất bại:
//   - deps/opts không hợp lệ → lỗi cấu hình trả về ngay
//   - không có chương nào đã hoàn thành → trả về lỗi (để caller biết rõ)
//   - trong phạm vi có chương chapters/{ch}.md bị thiếu → trả về lỗi (progress không khớp file system là bug ở tầng dữ liệu, nên cho người dùng thấy)
//   - đường dẫn output đã tồn tại và không chỉ định Overwrite → trả về lỗi
//
// Skipped dùng cho trường hợp "hợp lệ trong phạm vi nhưng chưa hoàn thành" (người dùng truyền to=100 nhưng mới viết tới 80).
func Run(ctx context.Context, deps Deps, opts Options) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.Store == nil {
		return nil, fmt.Errorf("exp: deps.Store is nil")
	}

	if opts.Format == "" {
		f, err := inferFormat(opts.OutPath)
		if err != nil {
			return nil, err
		}
		opts.Format = f
	}
	if opts.Format != FormatTXT && opts.Format != FormatEPUB {
		return nil, fmt.Errorf(i18n.T("error.export.format_unsupport"), opts.Format)
	}

	progress, err := deps.Store.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf(i18n.T("error.export.load_progress"), err)
	}
	if progress == nil || len(progress.CompletedChapters) == 0 {
		return nil, errors.New(i18n.T("error.export.no_completed"))
	}

	completed := make(map[int]struct{}, len(progress.CompletedChapters))
	maxCh := 0
	for _, c := range progress.CompletedChapters {
		completed[c] = struct{}{}
		if c > maxCh {
			maxCh = c
		}
	}

	from := opts.From
	if from <= 0 {
		from = 1
	}
	to := opts.To
	if to <= 0 {
		to = maxCh
	}
	if from > to {
		return nil, fmt.Errorf(i18n.T("error.export.range_invalid"), from, to)
	}

	var chapters, skipped []int
	for ch := from; ch <= to; ch++ {
		if _, ok := completed[ch]; ok {
			chapters = append(chapters, ch)
		} else {
			skipped = append(skipped, ch)
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf(i18n.T("error.export.range_empty"), from, to)
	}

	bodies := make(map[int]string, len(chapters))
	for _, ch := range chapters {
		text, err := deps.Store.Drafts.LoadChapterText(ch)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("error.export.read_chapter"), ch, err)
		}
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf(i18n.T("error.export.chapter_missing"), ch, ch)
		}
		bodies[ch] = text
	}

	outline, _ := deps.Store.Outline.LoadOutline()
	var volumes []domain.VolumeOutline
	if progress.Layered {
		volumes, _ = deps.Store.Outline.LoadLayeredOutline()
	}

	outPath := opts.OutPath
	if outPath == "" {
		name := strings.TrimSpace(progress.NovelName)
		if name == "" {
			name = filepath.Base(deps.Store.Dir())
		}
		outPath = filepath.Join(deps.Store.Dir(), sanitizeFileName(name)+"."+string(opts.Format))
	}

	if !opts.Overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return nil, fmt.Errorf(i18n.T("error.export.file_exists"), outPath)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf(i18n.T("error.export.stat_outpath"), err)
		}
	}

	titleIdx := buildTitleIndex(outline)
	var locations map[int]chapterLocation
	if len(volumes) > 0 {
		locations = buildLocations(volumes)
	}

	var data []byte
	switch opts.Format {
	case FormatTXT:
		data = []byte(renderTXT(progress.NovelName, chapters, titleIdx, locations, bodies))
	case FormatEPUB:
		buf, err := renderEPUB(progress.NovelName, chapters, titleIdx, locations, bodies)
		if err != nil {
			return nil, fmt.Errorf(i18n.T("error.export.render_epub"), err)
		}
		data = buf
	}

	if err := atomicWrite(outPath, data); err != nil {
		return nil, fmt.Errorf(i18n.T("error.export.write_failed"), err)
	}

	return &Result{
		Path:     outPath,
		Chapters: len(chapters),
		Bytes:    len(data),
		Skipped:  skipped,
	}, nil
}

// inferFormat suy ra định dạng từ phần mở rộng của đường dẫn output. Đường dẫn rỗng quay về TXT; phần mở rộng lạ thì báo lỗi (tránh lỗi âm thầm).
func inferFormat(path string) (Format, error) {
	if path == "" {
		return FormatTXT, nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case "", ".txt":
		return FormatTXT, nil
	case ".epub":
		return FormatEPUB, nil
	default:
		return "", fmt.Errorf(i18n.T("error.export.infer_extension"), filepath.Ext(path))
	}
}

// atomicWrite cùng kiểu với WriteFile của store/io.go: tmp + sync + rename.
// Không tái dùng store.IO vì đường dẫn output có thể nằm ngoài store.Dir().
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// sanitizeFileName thay thế các ký tự trong tên file mà phần lớn file system không cho phép hoặc dễ gây nhầm lẫn.
// Không chuyển mã quyết liệt, chỉ chặn các ký tự phân tách đường dẫn và ký tự điều khiển.
func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "novel"
	}
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"\x00", "_",
	)
	return replacer.Replace(name)
}
