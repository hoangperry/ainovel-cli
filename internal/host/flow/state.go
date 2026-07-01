package flow

import (
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// LoadState đọc toàn bộ sự kiện mà Route cần từ Store.
// Đây là "biên IO" của routing: mọi thao tác đọc tập trung ở đây, Route giữ thuần.
// Đọc thất bại thì điền theo mặc định bảo thủ (has*=false, boundary=nil), để Router thiên về phái lại thay vì bỏ qua.
func LoadState(store *storepkg.Store) State {
	s := State{
		FoundationMissing: store.FoundationMissing(),
	}
	progress, err := store.Progress.Load()
	if err != nil || progress == nil {
		return s
	}
	s.Progress = progress

	if n := len(progress.CompletedChapters); n > 0 {
		s.LastCompleted = progress.CompletedChapters[n-1]
	}

	// Biên cung truyện chỉ tính khi ở chế độ phân tầng và đã có chương hoàn thành
	if progress.Layered && s.LastCompleted > 0 {
		if boundary, berr := store.Outline.CheckArcBoundary(s.LastCompleted); berr == nil && boundary != nil {
			s.ArcBoundary = boundary
			if boundary.IsArcEnd {
				s.HasArcReview = store.World.HasArcReview(s.LastCompleted)
				s.HasArcSummary = store.Summaries.HasArcSummary(boundary.Volume, boundary.Arc)
				if boundary.IsVolumeEnd {
					s.HasVolumeSummary = store.Summaries.HasVolumeSummary(boundary.Volume)
				}
			}
		}
	}

	return s
}
