// Package flow hiện thực routing theo lĩnh vực: Host dựa vào sự kiện để quyết định kế tiếp gọi subagent nào làm gì.
//
// Nguyên tắc thiết kế:
//   - Route là hàm thuần: vào State, ra *Instruction. Không IO, không gọi Store, có thể unit test.
//   - State do LoadState (không thuần) dựng từ Store, đọc một lần đủ các sự kiện mà routing cần.
//   - Trả về nil là hợp lệ: nghĩa là "kịch bản để phân xử, giao cho Coordinator LLM tự quyết định".
//
// Router bao phủ các quyết định "kiểu tra bảng" (bước kế của mỗi chương, hậu xử lý cuối cung truyện, điều phối theo hàng đợi),
// không bao phủ các quyết định "kiểu hiểu ngữ nghĩa" (chọn Architect, xử lý Steer của người dùng, xuất tổng kết).
package flow

import (
	"fmt"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/domain"
	storepkg "github.com/voocel/ainovel-cli/internal/store"
)

// Instruction chỉ định subagent và nhiệm vụ mà Host yêu cầu Coordinator gọi ở bước kế.
type Instruction struct {
	Agent   string // architect_long / architect_short / writer / editor
	Task    string // mô tả nhiệm vụ cho subagent
	Reason  string // lý do cho Coordinator xem (tùy chọn, tiện debug và log)
	Chapter int    // số chương mà nhiệm vụ writer liên quan (viết tiếp/viết lại/đánh bóng); 0 nghĩa là không liên quan (nhiệm vụ editor/architect)
}

// State là đầu vào của Route: mọi sự kiện phải khai báo tường minh ở đây, cấm Route đọc Store bên trong.
type State struct {
	Progress *domain.Progress

	// Chương hoàn thành trước đó (cuối Progress.CompletedChapters); bằng 0 nghĩa là chưa bắt đầu viết.
	LastCompleted int

	// Thông tin biên cung truyện của chương trước; khi IsArcEnd=false thì các trường khác vô nghĩa.
	// Khi LastCompleted=0 hoặc không ở chế độ Layered thì nên là nil.
	ArcBoundary *storepkg.ArcBoundary

	// Ba sự kiện hậu xử lý cuối cung truyện: rà soát / tóm tắt cung truyện / tóm tắt quyển đã hoàn thành chưa.
	HasArcReview     bool
	HasArcSummary    bool
	HasVolumeSummary bool

	// Các mục thiếu trong foundation (tín hiệu bổ sung của giai đoạn lập kế hoạch).
	FoundationMissing []string
}

// Route dựa vào sự kiện trả về chỉ thị bước kế; trả về nil nghĩa là giao cho Coordinator LLM tự phân xử.
//
// Thứ tự ưu tiên quyết định (loại trừ lẫn nhau, từ trên xuống khớp cái đầu tiên):
//  1. Phase=Complete        → nil (LLM xuất tổng kết)
//  2. Phase!=Writing        → nil (LLM phân xử chọn Architect / bổ sung kế hoạch)
//  3. PendingRewrites khác rỗng → writer viết lại/đánh bóng theo hàng đợi
//  4. Flow=Reviewing        → nil (editor vừa lưu review, rẽ nhánh verdict do tầng tool xử lý)
//  5. Flow=Steering         → nil (đang xử lý can thiệp của người dùng)
//  6. Thiếu rà soát cuối cung truyện       → editor(arc review)
//  7. Có rà soát nhưng thiếu tóm tắt cung truyện → editor(arc summary)
//  8. Cuối quyển có tóm tắt cung truyện nhưng thiếu tóm tắt quyển → editor(volume summary)
//  9. Cung truyện kế là khung xương         → architect_long(expand_arc)
//
// 10. Cuối quyển cần quyết định quyển kế   → architect_long(append_volume / complete_book)
// 11. Còn lại                  → writer(viết next_chapter)
func Route(s State) *Instruction {
	p := s.Progress
	if p == nil {
		return nil
	}

	// 1. Trạng thái cuối: để LLM xuất tổng kết
	if p.Phase == domain.PhaseComplete {
		return nil
	}

	// 2. Giai đoạn lập kế hoạch do Coordinator phân xử (chọn architect_long/short + vòng bổ sung)
	if p.Phase != domain.PhaseWriting {
		return nil
	}

	// 3. Ưu tiên hàng đợi viết lại/đánh bóng (sự kiện đã ghi xuống ở tầng tool, Router chỉ phái theo đơn)
	if len(p.PendingRewrites) > 0 {
		ch := p.PendingRewrites[0]
		verb := contentlang.Pick("重写", "viết lại")
		if p.Flow == domain.FlowPolishing {
			verb = contentlang.Pick("打磨", "trau chuốt")
		}
		return &Instruction{
			Agent:   "writer",
			Task:    fmt.Sprintf(contentlang.Pick("%s第 %d 章", "%s chương %d"), verb, ch),
			Reason:  fmt.Sprintf(contentlang.Pick("PendingRewrites 队列剩余 %d 章", "PendingRewrites hàng đợi còn %d chương"), len(p.PendingRewrites)),
			Chapter: ch,
		}
	}

	// 4. Đang rà soát: save_review vừa ghi xuống, nâng/hạ cấp verdict do tầng tool xử lý, routing không can dự
	if p.Flow == domain.FlowReviewing {
		return nil
	}

	// 5. Đang xử lý can thiệp của người dùng: Coordinator đang phân xử, Host không tranh quyền
	if p.Flow == domain.FlowSteering {
		return nil
	}

	// 6-10. Hậu xử lý cuối cung truyện ở chế độ phân tầng
	if p.Layered && s.ArcBoundary != nil && s.ArcBoundary.IsArcEnd {
		b := s.ArcBoundary
		switch {
		case !s.HasArcReview:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf(contentlang.Pick("对第 %d 卷第 %d 弧做弧级评审（scope=arc）", "rà soát cấp cung cho quyển %d cung %d (scope=arc)"), b.Volume, b.Arc),
				Reason: contentlang.Pick("弧末评审未完成", "Rà soát cuối cung chưa hoàn thành"),
			}
		case !s.HasArcSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf(contentlang.Pick("生成第 %d 卷第 %d 弧摘要（save_arc_summary）", "tạo tóm tắt cung cho quyển %d cung %d (save_arc_summary)"), b.Volume, b.Arc),
				Reason: contentlang.Pick("弧摘要未完成", "Tóm tắt cung chưa hoàn thành"),
			}
		case b.IsVolumeEnd && !s.HasVolumeSummary:
			return &Instruction{
				Agent:  "editor",
				Task:   fmt.Sprintf(contentlang.Pick("生成第 %d 卷卷摘要（save_volume_summary）", "tạo tóm tắt quyển cho quyển %d (save_volume_summary)"), b.Volume),
				Reason: contentlang.Pick("卷摘要未完成", "Tóm tắt quyển chưa hoàn thành"),
			}
		case b.NeedsExpansion && b.NextArc > 0:
			return &Instruction{
				Agent:  "architect_long",
				Task:   fmt.Sprintf(contentlang.Pick("展开第 %d 卷第 %d 弧（save_foundation type=expand_arc）", "mở rộng quyển %d cung %d (save_foundation type=expand_arc)"), b.NextVolume, b.NextArc),
				Reason: contentlang.Pick("下一弧骨架待展开", "Khung xương cung kế chờ mở rộng"),
			}
		case b.NeedsNewVolume:
			return &Instruction{
				Agent:  "architect_long",
				Task:   contentlang.Pick("评估后调用 save_foundation type=append_volume（继续写）或 type=complete_book（全书结束）", "sau khi đánh giá hãy gọi save_foundation type=append_volume (viết tiếp) hoặc type=complete_book (kết thúc toàn sách)"),
				Reason: contentlang.Pick("卷末需决定追加新卷或结束全书", "Cuối quyển cần quyết định thêm quyển mới hay kết thúc toàn sách"),
			}
		}
	}

	// 12. Viết tiếp bình thường
	next := p.NextChapter()
	if next <= 0 {
		return nil
	}
	return &Instruction{
		Agent:   "writer",
		Task:    fmt.Sprintf(contentlang.Pick("写第 %d 章", "viết chương %d"), next),
		Reason:  contentlang.Pick("续写下一章", "Viết tiếp chương kế"),
		Chapter: next,
	}
}

// FormatMessage định dạng Instruction thành tin nhắn người dùng gửi cho Coordinator.
// Định dạng cố định, tiện cho prompt của Coordinator nhận diện và LLM phản hồi trực tiếp.
func FormatMessage(i *Instruction) string {
	return fmt.Sprintf(
		contentlang.Pick(
			"[Host 下达指令] 下一步：调用 subagent(%s, %q)\n理由：%s\n这是流程层的明确指令，请立即执行，不要先调 novel_context，不要先输出推理。",
			"[Host 下达指令] Bước kế: gọi subagent(%s, %q)\nLý do: %s\nĐây là chỉ thị rõ ràng của tầng quy trình, hãy thực thi ngay, không tra novel_context trước, không xuất suy luận trước.",
		),
		i.Agent, i.Task, i.Reason,
	)
}
