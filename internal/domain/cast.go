package domain

// CastEntry là một bản ghi nhân vật phụ trong danh sách nhân vật phụ.
//
// Tách rời với Character (characters.json, hồ sơ cốt lõi do Architect duy trì):
//   - CastEntry do tool commit_chapter tự tích lũy, ghi lại "nhân vật thứ yếu có tên đã từng xuất hiện"
//   - Character do Architect thiết kế tường minh, ghi lại nhân vật chính và nhân vật phụ then chốt cùng cung tính cách/đặc điểm/tier
//
// Khi trùng tên thì lấy Character làm chuẩn (nhân vật cốt lõi không vào cast_ledger), tránh trùng lặp.
type CastEntry struct {
	Name string `json:"name"`
	// Aliases hiện chưa có kênh ghi vào; để dành cho tool "người dùng steer gộp bí danh" tương lai
	// (ví dụ khai báo 'Lý chưởng quầy' và 'lão Lý' là cùng một người). MergeAppearances đã hỗ trợ tra cứu bí danh.
	Aliases          []string `json:"aliases,omitempty"`
	BriefRole        string   `json:"brief_role,omitempty"` // định vị một câu (lần đầu xuất hiện do Writer điền, có thể bổ sung sau; không bị ghi đè)
	FirstSeenChapter int      `json:"first_seen_chapter"`
	LastSeenChapter  int      `json:"last_seen_chapter"`
	// AppearanceCount dẫn xuất từ len(AppearanceChapters), giữ đồng bộ khi merge.
	// Giữ field tường minh để UI/JSON đọc trực tiếp, khỏi tính lại mỗi lần.
	AppearanceCount    int   `json:"appearance_count"`
	AppearanceChapters []int `json:"appearance_chapters"`
	// Promoted đánh dấu mục này đã được thăng cấp lên characters.json. RecentActive sẽ bỏ qua các mục này,
	// tránh gọi lại trùng với hồ sơ cốt lõi. Kênh thăng cấp hiện chưa hiện thực, field là hook để dành.
	Promoted bool `json:"promoted,omitempty"`
}

// CastIntro là khai báo giới thiệu của Writer về nhân vật mới xuất hiện khi commit_chapter.
// Chỉ được dùng khi tên này lần đầu xuất hiện hoặc BriefRole trong ledger vẫn còn rỗng.
type CastIntro struct {
	Name      string `json:"name"`
	BriefRole string `json:"brief_role"`
}
