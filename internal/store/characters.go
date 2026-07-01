package store

import (
	"fmt"
	"os"
	"strings"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// CharacterStore quản lý hồ sơ nhân vật và snapshot trạng thái.
type CharacterStore struct {
	io      *IO
	outline *OutlineStore // phụ thuộc chỉ đọc, dùng để duyệt snapshot
}

func NewCharacterStore(io *IO, outline *OutlineStore) *CharacterStore {
	return &CharacterStore{io: io, outline: outline}
}

// Save lưu đồng thời characters.json và characters.md (ghi nguyên tử).
func (s *CharacterStore) Save(chars []domain.Character) error {
	return s.io.WithWriteLock(func() error {
		if err := s.io.WriteJSONUnlocked("characters.json", chars); err != nil {
			return err
		}
		return s.io.WriteMarkdownUnlocked("characters.md", renderCharacters(chars))
	})
}

// Load đọc hồ sơ nhân vật từ characters.json.
func (s *CharacterStore) Load() ([]domain.Character, error) {
	var chars []domain.Character
	if err := s.io.ReadJSON("characters.json", &chars); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return chars, nil
}

// SaveSnapshots lưu snapshot trạng thái nhân vật vào meta/snapshots/v{vol}a{arc}.json.
func (s *CharacterStore) SaveSnapshots(volume, arc int, snapshots []domain.CharacterSnapshot) error {
	return s.io.WriteJSON(fmt.Sprintf("meta/snapshots/v%02da%02d.json", volume, arc), snapshots)
}

// LoadSnapshots đọc snapshot nhân vật của cung truyện chỉ định.
func (s *CharacterStore) LoadSnapshots(volume, arc int) ([]domain.CharacterSnapshot, error) {
	var snapshots []domain.CharacterSnapshot
	if err := s.io.ReadJSON(fmt.Sprintf("meta/snapshots/v%02da%02d.json", volume, arc), &snapshots); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return snapshots, nil
}

// LoadLatestSnapshots nạp snapshot nhân vật gần nhất (tìm theo thứ tự cung truyện giảm dần).
func (s *CharacterStore) LoadLatestSnapshots() ([]domain.CharacterSnapshot, error) {
	volumes, _ := s.outline.LoadLayeredOutline()
	if len(volumes) == 0 {
		return nil, nil
	}
	for vi := len(volumes) - 1; vi >= 0; vi-- {
		v := volumes[vi]
		for ai := len(v.Arcs) - 1; ai >= 0; ai-- {
			snaps, err := s.LoadSnapshots(v.Index, v.Arcs[ai].Index)
			if err != nil {
				return nil, err
			}
			if len(snaps) > 0 {
				return snaps, nil
			}
		}
	}
	return nil, nil
}

func renderCharacters(chars []domain.Character) string {
	var b strings.Builder
	b.WriteString("# 角色档案\n\n")
	for _, c := range chars {
		fmt.Fprintf(&b, "## %s（%s）\n\n", c.Name, c.Role)
		fmt.Fprintf(&b, "%s\n\n", c.Description)
		if c.Arc != "" {
			fmt.Fprintf(&b, "**角色弧线**：%s\n\n", c.Arc)
		}
		if len(c.Traits) > 0 {
			fmt.Fprintf(&b, "**特征**：%s\n\n", strings.Join(c.Traits, "、"))
		}
	}
	return b.String()
}
