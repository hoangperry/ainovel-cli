package tui

import "github.com/charmbracelet/lipgloss"

// Bảng màu chủ đề — tông ấm kiểu sách vở
// AdaptiveColor: Light = giá trị màu nền sáng, Dark = giá trị màu nền tối
//
// Nguyên tắc thiết kế: nấc Light giữ ổn định không đổi (nền sáng đã chỉnh ra hiệu
// quả ưng ý); nấc Dark đồng loạt sáng hơn Light ~25% lightness, tăng nhẹ độ bão
// hòa, đảm bảo nền tối đủ tương phản (colorDim trước đây #6b6355 trên nền đen
// #1c1c1c gần như không thấy, đường phân cách / chữ phụ trợ biến mất hết).
//
// colorAccent2 ở nền tối đổi từ #7a9e7e sang xanh lục lam #5fb8a3, tách khỏi
// "xanh khỏe mạnh" của colorSuccess — trước đây hai màu hoàn toàn giống nhau,
// làm lẫn lộn nhãn màu của architect agent với cảm giác vui mừng "command hit cao".
// bodyTextColor là chiến lược màu nền chữ cho "nội dung chính trung tính":
//   - terminal màu tối → NoColor, kế thừa màu nền chữ mặc định của terminal, tránh
//     việc ta nhét cứng #e8e0d0 màu trắng ngà đụng màu trên chủ đề nền ấm/nền lạnh
//     do người dùng tự cấu hình (người dùng thực tế thấy màu mặc định nền tối dễ đọc hơn).
//   - terminal màu sáng → dùng nấc Light của colorText (nâu đậm #3d3529), giữ tông
//     ấm thương hiệu; nền sáng màu đen mặc định tương phản quá gắt, nâu đậm đã chỉnh
//     trông mềm mại hơn trên nền sáng.
//
// AdaptiveColor cả hai đầu đều buộc phải có giá trị màu, không có nấc "không màu",
// nên ở đây lúc khởi động phán đoán nền một lần, sau đó mọi giá trị tổng quan /
// nội dung chương / mô tả lệnh ("nội dung chính trung tính") đều dùng bodyTextColor.
var bodyTextColor lipgloss.TerminalColor = func() lipgloss.TerminalColor {
	if lipgloss.HasDarkBackground() {
		return lipgloss.NoColor{}
	}
	return lipgloss.Color("#3d3529")
}()

var (
	colorText    = lipgloss.AdaptiveColor{Light: "#3d3529", Dark: "#e8e0d0"}
	colorDim     = lipgloss.AdaptiveColor{Light: "#8a7e6b", Dark: "#8a8175"}
	colorMuted   = lipgloss.AdaptiveColor{Light: "#7a7060", Dark: "#b8b09c"}
	colorAccent  = lipgloss.AdaptiveColor{Light: "#b8860b", Dark: "#e5b449"}
	colorAccent2 = lipgloss.AdaptiveColor{Light: "#3d7a42", Dark: "#5fb8a3"}
	colorRunning = lipgloss.AdaptiveColor{Light: "#6f8641", Dark: "#b5d075"}
	colorSuccess = lipgloss.AdaptiveColor{Light: "#3d7a42", Dark: "#7ec488"}
	colorError   = lipgloss.AdaptiveColor{Light: "#b5433a", Dark: "#e07060"}
	colorReview  = lipgloss.AdaptiveColor{Light: "#b07530", Dark: "#e09b5a"}
	colorContext = lipgloss.AdaptiveColor{Light: "#6b5a9e", Dark: "#a890d8"}
	colorTool    = lipgloss.AdaptiveColor{Light: "#3a7a8a", Dark: "#7ec5d8"}
)

// Ánh xạ màu nhãn trạng thái
var statusColors = map[string]lipgloss.AdaptiveColor{
	"READY":    colorDim,
	"PAUSING":  colorAccent,
	"PAUSED":   colorAccent,
	"RUNNING":  colorRunning,
	"REVIEW":   colorReview,
	"REWRITE":  colorReview,
	"COMPLETE": colorSuccess,
	"ERROR":    colorError,
}

// Hiển thị trạng thái: icon + label key. Trường label giữ i18n key, resolve lúc render
// (xem renderTopBar) để tôn trọng locale chọn lúc khởi động.
// icon của RUNNING để trống, do spinner frame lấp động, để cảm giác động hòa vào chính chỉ báo trạng thái.
var statusDisplay = map[string]struct {
	icon     string
	labelKey string
}{
	"READY":    {"○", "ui.status.ready"},
	"RUNNING":  {"", "ui.status.running"},
	"REVIEW":   {"◆", "ui.status.review"},
	"REWRITE":  {"◆", "ui.status.rewrite"},
	"COMPLETE": {"●", "ui.status.complete"},
	"PAUSED":   {"⏸", "ui.status.paused"},
	"PAUSING":  {"⏸", "ui.status.pausing"},
	"ERROR":    {"✕", "ui.status.error"},
}

// Ánh xạ màu phân loại sự kiện
var categoryColors = map[string]lipgloss.AdaptiveColor{
	"DISPATCH": colorAccent,
	"DONE":     colorSuccess,
	"TOOL":     colorTool,
	"SYSTEM":   colorAccent,
	"USER":     colorAccent2,
	"REVIEW":   colorReview,
	"CHECK":    colorSuccess,
	"ERROR":    colorError,
	"AGENT":    colorMuted,
	"CONTEXT":  colorContext,
	"COMPACT":  colorContext,
}

// Style cơ bản
var (
	baseBorder = lipgloss.RoundedBorder()

	topBarStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 1)

	statusIconStyle = lipgloss.NewStyle().
			Bold(true)

	statusLabelStyle = lipgloss.NewStyle().
				Foreground(colorText)

	panelTitleStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	fieldLabelStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(10)

	// fieldValueStyle / cardContentStyle dùng bodyTextColor —— các giá trị vùng tổng
	// quan (trạng thái chạy, số chương đã hoàn thành, số chữ...), mục outline, danh sách
	// nhân vật, tóm tắt chương... ("nội dung chính trung tính") ở nền tối theo màu nền
	// chữ mặc định của terminal (tránh nhét cứng màu trắng ngà đụng chủ đề), nền sáng
	// dùng nâu đậm giữ tông ấm. Các phần tử ngữ nghĩa mạnh (tiêu đề, giá trị nổi bật,
	// trạng thái, lỗi, tô màu tỷ lệ hit...) vẫn dùng màu chủ đề như colorAccent / colorError.
	fieldValueStyle = lipgloss.NewStyle().Foreground(bodyTextColor)

	highlightValueStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	contextUsageMetaStyle = lipgloss.NewStyle().
				Foreground(colorDim)

	cardTitleStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	cardContentStyle = lipgloss.NewStyle().Foreground(bodyTextColor)
)
