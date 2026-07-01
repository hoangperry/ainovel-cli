package store

import (
	"os"
	"slices"
	"sort"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// CastStore quản lý sổ danh sách nhân vật phụ (meta/cast_ledger.json).
//
// Sổ nhân vật phụ ghi lại "các nhân vật thứ yếu có tên đã từng xuất hiện", trực giao với characters.json (hồ sơ nhân vật cốt lõi):
//   - characters.json: nhân vật chính + nhân vật phụ then chốt do Architect thiết kế tường minh, không sửa trong giai đoạn viết
//   - cast_ledger.json: tool commit_chapter tự động tích lũy, mọi nhân vật phụ không cốt lõi có tên
//
// MergeAppearances là idempotent: commit lặp lại cùng một chương sẽ không tích lũy trùng AppearanceCount.
type CastStore struct{ io *IO }

func NewCastStore(io *IO) *CastStore { return &CastStore{io: io} }

const castLedgerPath = "meta/cast_ledger.json"

// Load đọc sổ nhân vật phụ. Trả về slice rỗng khi file không tồn tại.
func (s *CastStore) Load() ([]domain.CastEntry, error) {
	var entries []domain.CastEntry
	if err := s.io.ReadJSON(castLedgerPath, &entries); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// Save lưu toàn bộ sổ nhân vật phụ (ghi nguyên tử).
func (s *CastStore) Save(entries []domain.CastEntry) error {
	return s.io.WriteJSON(castLedgerPath, entries)
}

// MergeAppearances gộp bản ghi xuất hiện của chương này vào sổ.
//
// Tham số:
//   - chapter: số của chương này
//   - characters: mảng tên xuất hiện trong chương này (từ commit_chapter.Characters)
//   - intros: giới thiệu nhân vật mới do Writer khai báo tường minh (lần đầu xuất hiện hoặc bổ sung BriefRole)
//   - knownCore: tập tên nhân vật cốt lõi đã có trong characters.json (các tên này bỏ qua ghi ledger)
//
// Hành vi:
//   - tên nằm trong knownCore: bỏ qua (hồ sơ nhân vật cốt lõi là điểm ghi duy nhất của nó)
//   - tên đã có trong ledger và chapter đã có trong AppearanceChapters: bỏ qua hoàn toàn (idempotent)
//   - tên đã có trong ledger nhưng chapter là mới: cập nhật LastSeenChapter + thêm chapter + count++
//   - tên chưa có trong ledger: thêm mục mới
//   - BriefRole trong intros chỉ được dùng khi BriefRole của mục ledger vẫn rỗng, tránh ghi đè giới thiệu trước đó
func (s *CastStore) MergeAppearances(
	chapter int,
	characters []string,
	intros []domain.CastIntro,
	knownCore map[string]bool,
) error {
	if chapter <= 0 || len(characters) == 0 {
		return nil
	}
	return s.io.WithWriteLock(func() error {
		var entries []domain.CastEntry
		if err := s.io.ReadJSONUnlocked(castLedgerPath, &entries); err != nil && !os.IsNotExist(err) {
			return err
		}

		introMap := make(map[string]string, len(intros))
		for _, in := range intros {
			if in.Name != "" {
				introMap[in.Name] = in.BriefRole
			}
		}

		index := make(map[string]int, len(entries))
		for i, e := range entries {
			index[e.Name] = i
			for _, alias := range e.Aliases {
				index[alias] = i
			}
		}

		seen := make(map[string]bool, len(characters))
		for _, name := range characters {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			if knownCore[name] {
				continue
			}
			if i, ok := index[name]; ok {
				entry := &entries[i]
				if !slices.Contains(entry.AppearanceChapters, chapter) {
					entry.AppearanceChapters = append(entry.AppearanceChapters, chapter)
					entry.AppearanceCount = len(entry.AppearanceChapters)
					if chapter > entry.LastSeenChapter {
						entry.LastSeenChapter = chapter
					}
					if chapter < entry.FirstSeenChapter || entry.FirstSeenChapter == 0 {
						entry.FirstSeenChapter = chapter
					}
				}
				if entry.BriefRole == "" {
					if br, ok := introMap[name]; ok && br != "" {
						entry.BriefRole = br
					}
				}
				continue
			}
			entries = append(entries, domain.CastEntry{
				Name:               name,
				BriefRole:          introMap[name],
				FirstSeenChapter:   chapter,
				LastSeenChapter:    chapter,
				AppearanceCount:    1,
				AppearanceChapters: []int{chapter},
			})
		}
		return s.io.WriteJSONUnlocked(castLedgerPath, entries)
	})
}

// RecentActive trả về N mục nhân vật phụ hoạt động gần nhất (theo LastSeenChapter giảm dần).
// Dùng cho novel_context để gọi lại "nhân vật phụ xuất hiện gần đây" mà Writer có thể cần khi viết chương kế.
//
// Các mục đã thăng cấp lên characters.json (Promoted=true) sẽ bị bỏ qua, tránh gọi lại trùng với hồ sơ cốt lõi.
func (s *CastStore) RecentActive(limit int) ([]domain.CastEntry, error) {
	if limit <= 0 {
		return nil, nil
	}
	entries, err := s.Load()
	if err != nil {
		return nil, err
	}
	active := entries[:0:0]
	for _, e := range entries {
		if e.Promoted {
			continue
		}
		active = append(active, e)
	}
	if len(active) == 0 {
		return nil, nil
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].LastSeenChapter != active[j].LastSeenChapter {
			return active[i].LastSeenChapter > active[j].LastSeenChapter
		}
		return active[i].AppearanceCount > active[j].AppearanceCount
	})
	if len(active) > limit {
		active = active[:limit]
	}
	return active, nil
}
