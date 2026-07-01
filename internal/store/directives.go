package store

import (
	"fmt"
	"os"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/i18n"
)

// maxDirectives là giới hạn dung lượng của chỉ thị dài hạn: tránh việc sau nhiều tháng chạy nền, envelope kéo theo một chuỗi dài can thiệp lịch sử
// (biến thể envelope lớn làm phình context). Khi vượt giới hạn, coordinator quyết định remove gộp các yêu cầu cũ trước.
const maxDirectives = 20

// DirectivesStore quản lý chỉ thị sáng tác dài hạn của người dùng (meta/user_directives.json).
type DirectivesStore struct{ io *IO }

func NewDirectivesStore(io *IO) *DirectivesStore { return &DirectivesStore{io: io} }

// Load đọc toàn bộ chỉ thị dài hạn. Trả về danh sách rỗng khi file không tồn tại.
func (s *DirectivesStore) Load() ([]domain.UserDirective, error) {
	s.io.mu.RLock()
	defer s.io.mu.RUnlock()
	return s.loadUnlocked()
}

// Add thêm một chỉ thị dài hạn và trả về danh sách đầy đủ sau cập nhật.
// Khi Text trùng hoàn toàn với mục đã có thì không thêm lặp (idempotent); trả về lỗi khi vượt giới hạn dung lượng.
func (s *DirectivesStore) Add(d domain.UserDirective) ([]domain.UserDirective, error) {
	var list []domain.UserDirective
	err := s.io.WithWriteLock(func() error {
		existing, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		for _, e := range existing {
			if e.Text == d.Text {
				list = existing
				return nil
			}
		}
		if len(existing) >= maxDirectives {
			return fmt.Errorf(i18n.T("store.directives.limit_reached"), maxDirectives)
		}
		list = append(existing, d)
		return s.io.WriteJSONUnlocked("meta/user_directives.json", list)
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

// Remove xóa một chỉ thị dài hạn theo số thứ tự 1-based và trả về danh sách đầy đủ sau cập nhật.
func (s *DirectivesStore) Remove(index int) ([]domain.UserDirective, error) {
	var list []domain.UserDirective
	err := s.io.WithWriteLock(func() error {
		existing, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if index < 1 || index > len(existing) {
			return fmt.Errorf(i18n.T("store.directives.index_out_of_range"), index, len(existing))
		}
		list = append(existing[:index-1], existing[index:]...)
		return s.io.WriteJSONUnlocked("meta/user_directives.json", list)
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *DirectivesStore) loadUnlocked() ([]domain.UserDirective, error) {
	var list []domain.UserDirective
	if err := s.io.ReadJSONUnlocked("meta/user_directives.json", &list); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return list, nil
}
