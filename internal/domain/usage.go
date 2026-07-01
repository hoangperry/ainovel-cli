package domain

import "time"

// UsageSchemaVersion là số phiên bản tương thích của meta/usage.json.
// Sau này nếu ngữ nghĩa các field AgentUsageTotals thay đổi thì tăng giá trị này; UsageStore.Load thấy phiên bản khác nên bỏ qua và kích hoạt replay để dựng lại.
const UsageSchemaVersion = 2

// UsageState là snapshot có thể persist của lượng dùng token / cost tích lũy.
// Trong bộ nhớ do UsageTracker duy trì, định kỳ debounce ghi xuống meta/usage.json.
//
// Lưu ý: cửa sổ trượt samples ("tỷ lệ trúng N lần gần nhất") bên trong UsageTracker **không được persist**——
// nó chỉ phục vụ chẩn đoán ngắn hạn cho UI, tiến trình khởi động lại bắt đầu từ rỗng tích lũy vài vòng là khôi phục được ngữ nghĩa.
// MissingAssistantUsage vẫn được persist, tích lũy qua các lần khởi động lại có giá trị chẩn đoán hơn.
type UsageState struct {
	Schema       int                         `json:"schema"`
	UpdatedAt    time.Time                   `json:"updated_at"`
	Overall      AgentUsageTotals            `json:"overall"`
	PerAgent     map[string]AgentUsageTotals `json:"per_agent"`
	PerModel     map[string]AgentUsageTotals `json:"per_model,omitempty"`
	MissingUsage int                         `json:"missing_assistant_usage"`
}

// AgentUsageTotals là dạng có thể persist của số đếm tích lũy cho một role (hoặc overall).
type AgentUsageTotals struct {
	Input        int     `json:"input"`
	Output       int     `json:"output"`
	CacheRead    int     `json:"cache_read"`
	CacheWrite   int     `json:"cache_write"`
	Cost         float64 `json:"cost_usd"`
	Saved        float64 `json:"saved_usd"`
	CacheCapable bool    `json:"cache_capable"`
}
