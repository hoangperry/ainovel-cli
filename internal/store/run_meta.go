package store

import (
	"os"
	"time"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// RunMetaStore quản lý metadata vận hành (model, lịch sử can thiệp, mức kế hoạch, v.v.).
type RunMetaStore struct{ io *IO }

func NewRunMetaStore(io *IO) *RunMetaStore { return &RunMetaStore{io: io} }

// Save lưu metadata vận hành vào meta/run.json.
func (s *RunMetaStore) Save(meta domain.RunMeta) error {
	s.io.mu.Lock()
	defer s.io.mu.Unlock()
	return s.saveUnlocked(meta)
}

// Load đọc metadata vận hành.
func (s *RunMetaStore) Load() (*domain.RunMeta, error) {
	s.io.mu.RLock()
	defer s.io.mu.RUnlock()
	return s.loadUnlocked()
}

func (s *RunMetaStore) loadUnlocked() (*domain.RunMeta, error) {
	var meta domain.RunMeta
	if err := s.io.ReadJSONUnlocked("meta/run.json", &meta); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &meta, nil
}

func (s *RunMetaStore) saveUnlocked(meta domain.RunMeta) error {
	return s.io.WriteJSONUnlocked("meta/run.json", meta)
}

// Init khởi tạo hoặc cập nhật metadata vận hành, giữ lại SteerHistory hiện có.
func (s *RunMetaStore) Init(style, provider, model string) error {
	return s.io.WithWriteLock(func() error {
		existing, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		meta := domain.RunMeta{
			StartedAt: time.Now().Format(time.RFC3339),
			Provider:  provider,
			Style:     style,
			Model:     model,
		}
		if existing != nil {
			meta.SteerHistory = existing.SteerHistory
			meta.PendingSteer = existing.PendingSteer
			meta.PlanningTier = existing.PlanningTier
		}
		return s.saveUnlocked(meta)
	})
}

// AppendSteerEntry thêm bản ghi can thiệp của người dùng.
func (s *RunMetaStore) AppendSteerEntry(entry domain.SteerEntry) error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil {
			meta = &domain.RunMeta{}
		}
		meta.SteerHistory = append(meta.SteerHistory, entry)
		return s.saveUnlocked(*meta)
	})
}

// SetPendingSteer ghi lại chỉ thị Steer chưa hoàn thành.
func (s *RunMetaStore) SetPendingSteer(input string) error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil {
			meta = &domain.RunMeta{}
		}
		meta.PendingSteer = input
		return s.saveUnlocked(*meta)
	})
}

// ClearPendingSteer xóa chỉ thị Steer đã xử lý.
func (s *RunMetaStore) ClearPendingSteer() error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil || meta.PendingSteer == "" {
			return nil
		}
		meta.PendingSteer = ""
		return s.saveUnlocked(*meta)
	})
}

// SetPlanningTier ghi lại mức kế hoạch của tác phẩm hiện tại.
func (s *RunMetaStore) SetPlanningTier(tier domain.PlanningTier) error {
	return s.io.WithWriteLock(func() error {
		meta, err := s.loadUnlocked()
		if err != nil {
			return err
		}
		if meta == nil {
			meta = &domain.RunMeta{}
		}
		meta.PlanningTier = tier
		return s.saveUnlocked(*meta)
	})
}
