package store

import (
	"os"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// UsageStore lưu bền vững lượng dùng tích lũy token / cost vào meta/usage.json.
// Ghi qua atomic write của IO (tmp + rename), đường Save mỗi lần ghi đè hoàn toàn cả state.
type UsageStore struct{ io *IO }

func NewUsageStore(io *IO) *UsageStore { return &UsageStore{io: io} }

// Load đọc usage.json. Trả về (nil, nil) khi file không tồn tại hoặc phiên bản schema không khớp,
// để bên gọi quyết định có chạy session replay để nạp bù một lần hay không.
func (s *UsageStore) Load() (*domain.UsageState, error) {
	var state domain.UsageState
	if err := s.io.ReadJSON("meta/usage.json", &state); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if state.Schema != domain.UsageSchemaVersion {
		return nil, nil
	}
	return &state, nil
}

// Save ghi đè hoàn toàn state xuống đĩa. Bên gọi chịu trách nhiệm debounce / điều tiết.
func (s *UsageStore) Save(state domain.UsageState) error {
	state.Schema = domain.UsageSchemaVersion
	return s.io.WriteJSON("meta/usage.json", state)
}
