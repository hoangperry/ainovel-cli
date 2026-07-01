package store

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// OutlineStore quản lý tiền đề câu chuyện, dàn ý (phẳng/phân tầng) và la bàn.
type OutlineStore struct{ io *IO }

func NewOutlineStore(io *IO) *OutlineStore { return &OutlineStore{io: io} }

// SavePremise lưu tiền đề câu chuyện vào premise.md.
func (s *OutlineStore) SavePremise(content string) error {
	return s.io.WriteMarkdown("premise.md", content)
}

// LoadPremise đọc premise.md. Trả về chuỗi rỗng khi không tồn tại.
func (s *OutlineStore) LoadPremise() (string, error) {
	data, err := s.io.ReadFile("premise.md")
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

// SaveOutline lưu đồng thời outline.json và outline.md (ghi nguyên tử).
func (s *OutlineStore) SaveOutline(entries []domain.OutlineEntry) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("outline.json", entries); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("outline.md", renderOutline(entries))
	})
}

// LoadOutline đọc dàn ý có cấu trúc từ outline.json.
func (s *OutlineStore) LoadOutline() ([]domain.OutlineEntry, error) {
	var entries []domain.OutlineEntry
	if err := s.io.ReadJSON("outline.json", &entries); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// GetChapterOutline lấy mục dàn ý của chương chỉ định.
func (s *OutlineStore) GetChapterOutline(chapter int) (*domain.OutlineEntry, error) {
	entries, err := s.LoadOutline()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Chapter == chapter {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("chapter %d not found in outline", chapter)
}

// SaveLayeredOutline lưu dàn ý phân tầng (chế độ trường thiên, ghi nguyên tử).
func (s *OutlineStore) SaveLayeredOutline(volumes []domain.VolumeOutline) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("layered_outline.json", volumes); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("layered_outline.md", renderLayeredOutline(volumes))
	})
}

// LoadLayeredOutline đọc dàn ý phân tầng.
func (s *OutlineStore) LoadLayeredOutline() ([]domain.VolumeOutline, error) {
	var volumes []domain.VolumeOutline
	if err := s.io.ReadJSON("layered_outline.json", &volumes); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return volumes, nil
}

// ClearLayeredOutline dọn file dàn ý phân tầng.
func (s *OutlineStore) ClearLayeredOutline() error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.RemoveFileUnlocked("layered_outline.json"); err != nil {
			return err
		}
		return s.io.RemoveFileUnlocked("layered_outline.md")
	})
}

// GetChapterFromLayered tìm theo số chương toàn cục trong dàn ý phân tầng.
func (s *OutlineStore) GetChapterFromLayered(chapter int) (*domain.OutlineEntry, error) {
	volumes, err := s.LoadLayeredOutline()
	if err != nil {
		return nil, err
	}
	ch := 1
	for _, v := range volumes {
		for _, a := range v.Arcs {
			for i := range a.Chapters {
				if ch == chapter {
					e := a.Chapters[i]
					e.Chapter = ch
					return &e, nil
				}
				ch++
			}
		}
	}
	return nil, fmt.Errorf("chapter %d not found in layered outline", chapter)
}

// LocateChapter định vị quyển và cung dựa theo số chương toàn cục.
func (s *OutlineStore) LocateChapter(chapter int) (volume, arc int, err error) {
	volumes, err := s.LoadLayeredOutline()
	if err != nil {
		return 0, 0, err
	}
	ch := 1
	for _, v := range volumes {
		for _, a := range v.Arcs {
			for range a.Chapters {
				if ch == chapter {
					return v.Index, a.Index, nil
				}
				ch++
			}
		}
	}
	return 0, 0, fmt.Errorf("chapter %d not found in layered outline", chapter)
}

// ArcBoundary là thông tin biên cung.
type ArcBoundary struct {
	IsArcEnd       bool
	IsVolumeEnd    bool
	Volume         int
	Arc            int
	NextVolume     int
	NextArc        int
	NeedsExpansion bool
	NeedsNewVolume bool // cuối quyển và layered_outline hiện tại không có quyển kế tiếp
}

// HasNextArc cho biết còn cung tiếp theo hay không.
func (b *ArcBoundary) HasNextArc() bool {
	return b.NextVolume > 0 || b.NextArc > 0
}

// CheckArcBoundary kiểm tra một chương có phải chương cuối của cung/quyển hay không.
func (s *OutlineStore) CheckArcBoundary(chapter int) (*ArcBoundary, error) {
	volumes, err := s.LoadLayeredOutline()
	if err != nil || len(volumes) == 0 {
		return nil, err
	}

	type arcPos struct {
		volIdx, arcIdx int
		volume, arc    int
		chInArc        int
		arcLen         int
	}

	ch := 1
	var cur *arcPos
	for vi, v := range volumes {
		for ai, a := range v.Arcs {
			for ci := range a.Chapters {
				if ch == chapter {
					cur = &arcPos{
						volIdx:  vi,
						arcIdx:  ai,
						volume:  v.Index,
						arc:     a.Index,
						chInArc: ci,
						arcLen:  len(a.Chapters),
					}
				}
				ch++
			}
		}
	}
	if cur == nil {
		return nil, nil
	}

	b := &ArcBoundary{
		Volume: cur.volume,
		Arc:    cur.arc,
	}

	isLastChInArc := cur.chInArc == cur.arcLen-1
	isLastArcInVol := cur.arcIdx == len(volumes[cur.volIdx].Arcs)-1

	// Next*/NeedsExpansion/NeedsNewVolume chỉ có ý nghĩa ở cuối cung, nếu không sẽ khiến Coordinator hiểu nhầm là phải mở rộng cung kế tiếp sớm.
	if !isLastChInArc {
		return b, nil
	}

	b.IsArcEnd = true
	if isLastArcInVol {
		b.IsVolumeEnd = true
	}

	found := false
	for vi := cur.volIdx; vi < len(volumes); vi++ {
		startArc := 0
		if vi == cur.volIdx {
			startArc = cur.arcIdx + 1
		}
		for ai := startArc; ai < len(volumes[vi].Arcs); ai++ {
			b.NextVolume = volumes[vi].Index
			b.NextArc = volumes[vi].Arcs[ai].Index
			b.NeedsExpansion = !volumes[vi].Arcs[ai].IsExpanded()
			found = true
			break
		}
		if found {
			break
		}
	}

	if b.IsVolumeEnd && !found {
		b.NeedsNewVolume = true
	}

	return b, nil
}

// expandArcUnlocked là phương thức nội bộ, được gọi trong điều phối xuyên domain Store.ExpandArc.
func (s *OutlineStore) expandArcUnlocked(volumeIdx, arcIdx int, chapters []domain.OutlineEntry) ([]domain.VolumeOutline, error) {
	var volumes []domain.VolumeOutline
	if err := s.io.ReadJSONUnlocked("layered_outline.json", &volumes); err != nil {
		return nil, fmt.Errorf("load layered_outline: %w", err)
	}
	found := false
	for vi := range volumes {
		if volumes[vi].Index != volumeIdx {
			continue
		}
		for ai := range volumes[vi].Arcs {
			if volumes[vi].Arcs[ai].Index != arcIdx {
				continue
			}
			volumes[vi].Arcs[ai].Chapters = chapters
			volumes[vi].Arcs[ai].EstimatedChapters = 0
			found = true
			break
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("arc not found: volume=%d, arc=%d", volumeIdx, arcIdx)
	}
	if err := s.io.WriteJSONUnlocked("layered_outline.json", volumes); err != nil {
		return nil, err
	}
	if err := s.io.WriteMarkdownUnlocked("layered_outline.md", renderLayeredOutline(volumes)); err != nil {
		return nil, err
	}
	flat := domain.FlattenOutline(volumes)
	if err := s.io.WriteJSONUnlocked("outline.json", flat); err != nil {
		return nil, err
	}
	if err := s.io.WriteMarkdownUnlocked("outline.md", renderOutline(flat)); err != nil {
		return nil, err
	}
	return volumes, nil
}

// appendVolumeUnlocked là phương thức nội bộ, được gọi trong điều phối xuyên domain Store.AppendVolume.
func (s *OutlineStore) appendVolumeUnlocked(vol domain.VolumeOutline) ([]domain.VolumeOutline, error) {
	var volumes []domain.VolumeOutline
	if err := s.io.ReadJSONUnlocked("layered_outline.json", &volumes); err != nil {
		return nil, fmt.Errorf("load layered_outline: %w", err)
	}
	if err := validateAppendVolume(volumes, vol); err != nil {
		return nil, err
	}
	volumes = append(volumes, vol)
	if err := s.io.WriteJSONUnlocked("layered_outline.json", volumes); err != nil {
		return nil, err
	}
	if err := s.io.WriteMarkdownUnlocked("layered_outline.md", renderLayeredOutline(volumes)); err != nil {
		return nil, err
	}
	flat := domain.FlattenOutline(volumes)
	if err := s.io.WriteJSONUnlocked("outline.json", flat); err != nil {
		return nil, err
	}
	if err := s.io.WriteMarkdownUnlocked("outline.md", renderOutline(flat)); err != nil {
		return nil, err
	}
	return volumes, nil
}

func validateAppendVolume(existing []domain.VolumeOutline, vol domain.VolumeOutline) error {
	if len(existing) > 0 {
		maxIdx := existing[len(existing)-1].Index
		if vol.Index <= maxIdx {
			return fmt.Errorf(i18n.T("store.outline.volume_index_order"), vol.Index, maxIdx)
		}
	}
	if len(vol.Arcs) == 0 {
		return errors.New(i18n.T("store.outline.volume_needs_arc"))
	}
	if !vol.Arcs[0].IsExpanded() {
		return errors.New(i18n.T("store.outline.first_arc_detail"))
	}
	return nil
}

// SaveCompass lưu la bàn định hướng kết cục.
func (s *OutlineStore) SaveCompass(compass domain.StoryCompass) error {
	if compass.EndingDirection == "" {
		return errors.New(i18n.T("store.outline.ending_required"))
	}
	return s.io.WriteJSON("meta/compass.json", compass)
}

// LoadCompass đọc la bàn định hướng kết cục.
func (s *OutlineStore) LoadCompass() (*domain.StoryCompass, error) {
	var c domain.StoryCompass
	if err := s.io.ReadJSON("meta/compass.json", &c); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func renderLayeredOutline(volumes []domain.VolumeOutline) string {
	var b strings.Builder
	b.WriteString("# 分层大纲\n\n")
	ch := 1
	for _, v := range volumes {
		fmt.Fprintf(&b, "## 第 %d 卷：%s\n\n", v.Index, v.Title)
		fmt.Fprintf(&b, "**主题**：%s\n\n", v.Theme)
		for _, a := range v.Arcs {
			fmt.Fprintf(&b, "### 第 %d 弧：%s\n\n", a.Index, a.Title)
			fmt.Fprintf(&b, "**目标**：%s\n\n", a.Goal)
			if !a.IsExpanded() {
				fmt.Fprintf(&b, "*（待展开，预估 %d 章）*\n\n", a.EstimatedChapters)
				continue
			}
			for _, e := range a.Chapters {
				fmt.Fprintf(&b, "#### 第 %d 章：%s\n\n", ch, e.Title)
				fmt.Fprintf(&b, "**核心事件**：%s\n\n", e.CoreEvent)
				if e.Hook != "" {
					fmt.Fprintf(&b, "**钩子**：%s\n\n", e.Hook)
				}
				ch++
			}
		}
	}
	return b.String()
}

func renderOutline(entries []domain.OutlineEntry) string {
	var b strings.Builder
	b.WriteString("# 大纲\n\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "## 第 %d 章：%s\n\n", e.Chapter, e.Title)
		fmt.Fprintf(&b, "**核心事件**：%s\n\n", e.CoreEvent)
		if e.Hook != "" {
			fmt.Fprintf(&b, "**钩子**：%s\n\n", e.Hook)
		}
		if len(e.Scenes) > 0 {
			b.WriteString("**场景**：\n")
			for i, sc := range e.Scenes {
				fmt.Fprintf(&b, "%d. %s\n", i+1, sc)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
