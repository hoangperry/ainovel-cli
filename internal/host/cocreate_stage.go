package host

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/store"
)

// buildStoryStateSummary ráp một đoạn tóm tắt hiện trạng truyện tinh gọn, cho trợ lý đồng sáng tạo theo giai đoạn hiểu "đã viết những gì".
// Tái dùng các điểm truy cập store, chỉ lấy những sự thật ở mức cao cần cho việc lập kế hoạch hướng đi (tiến độ / la bàn / quyển gần nhất / nhân vật chính / phục bút đang hoạt động);
// không kéo phần chính văn, không nạp toàn bộ JSON của novel_context — đồng sáng tạo là đối thoại, cần một bản tổng quan dễ đọc, không phải context viết.
// Thiếu mục nào thì bỏ qua (best-effort), trả về chuỗi rỗng nghĩa là chưa có tiến độ khả dụng.
func buildStoryStateSummary(s *store.Store) string {
	if s == nil {
		return ""
	}
	var b strings.Builder

	if progress, _ := s.Progress.Load(); progress != nil {
		if name := strings.TrimSpace(progress.NovelName); name != "" {
			fmt.Fprintf(&b, contentlang.Pick("- 书名：《%s》\n", "- Tên sách: 《%s》\n"), name)
		}
		fmt.Fprintf(&b, contentlang.Pick("- 进度：已完成 %d 章", "- Tiến độ: đã xong %d chương"), len(progress.CompletedChapters))
		if progress.TotalChapters > 0 {
			fmt.Fprintf(&b, contentlang.Pick(" / 规划 %d 章", " / dự kiến %d chương"), progress.TotalChapters)
		}
		fmt.Fprintf(&b, contentlang.Pick("，约 %d 字，下一章为第 %d 章\n", ", khoảng %d chữ, chương kế là chương %d\n"), progress.TotalWordCount, progress.NextChapter())
		if progress.Layered && progress.CurrentVolume > 0 {
			fmt.Fprintf(&b, contentlang.Pick("- 当前位置：第 %d 卷 第 %d 弧\n", "- Vị trí hiện tại: quyển %d cung truyện %d\n"), progress.CurrentVolume, progress.CurrentArc)
		}
	}

	if compass, _ := s.Outline.LoadCompass(); compass != nil {
		if dir := strings.TrimSpace(compass.EndingDirection); dir != "" {
			fmt.Fprintf(&b, contentlang.Pick("- 终局方向：%s\n", "- Hướng kết cục: %s\n"), dir)
		}
		if compass.EstimatedScale != "" {
			fmt.Fprintf(&b, contentlang.Pick("- 预估规模：%s\n", "- Quy mô dự kiến: %s\n"), compass.EstimatedScale)
		}
		if len(compass.OpenThreads) > 0 {
			fmt.Fprintf(&b, contentlang.Pick("- 活跃长线：%s\n", "- Tuyến dài đang hoạt động: %s\n"), strings.Join(compass.OpenThreads, contentlang.Pick("；", "; ")))
		}
	}

	// Tóm tắt quyển gần nhất, cho trợ lý biết truyện vừa đi tới đâu
	if vols, _ := s.Summaries.LoadAllVolumeSummaries(); len(vols) > 0 {
		last := vols[len(vols)-1]
		fmt.Fprintf(&b, contentlang.Pick("- 最近《%s》：%s\n", "- Gần nhất 《%s》: %s\n"), last.Title, truncate(last.Summary, 200))
	}

	// Nhân vật chính (core/important), tối đa 8
	if chars, _ := s.Characters.Load(); len(chars) > 0 {
		var names []string
		for _, c := range chars {
			if c.Tier == "secondary" || c.Tier == "decorative" {
				continue
			}
			line := c.Name
			if role := strings.TrimSpace(c.Role); role != "" {
				line += contentlang.Pick("（", "(") + role + contentlang.Pick("）", ")")
			}
			names = append(names, line)
			if len(names) >= 8 {
				break
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, contentlang.Pick("- 主要人物：%s\n", "- Nhân vật chính: %s\n"), strings.Join(names, contentlang.Pick("、", ", ")))
		}
	}

	// Phục bút chưa thu, tối đa 6
	if fs, _ := s.World.LoadActiveForeshadow(); len(fs) > 0 {
		var items []string
		for _, f := range fs {
			items = append(items, truncate(f.Description, 40))
			if len(items) >= 6 {
				break
			}
		}
		fmt.Fprintf(&b, contentlang.Pick("- 未收伏笔：%s\n", "- Phục bút chưa thu: %s\n"), strings.Join(items, contentlang.Pick("；", "; ")))
	}

	return strings.TrimSpace(b.String())
}

// stageSystemPrompt ráp system prompt đầy đủ cho đồng sáng tạo theo giai đoạn: prompt giai đoạn + tóm tắt trạng thái truyện hiện tại.
// Tóm tắt được gắn ở cuối như phụ lục dữ liệu (ngăn cách bằng đường kẻ và quy phạm định dạng), hô ứng với chỉ dẫn "tiến độ xem bên dưới" trong prompt.
func stageSystemPrompt(s *store.Store) string {
	prompt := stageCoCreateSystemPrompt()
	if summary := buildStoryStateSummary(s); summary != "" {
		prompt += contentlang.Pick(
			"\n\n---\n## 当前故事状态\n（以下是已写内容的客观摘要，供你规划后续时参照，不要在 <draft> 里照抄原文）\n",
			"\n\n---\n## Trạng thái truyện hiện tại\n(Dưới đây là bản tóm tắt khách quan nội dung đã viết, dùng để bạn tham chiếu khi lập kế hoạch tiếp theo, đừng chép nguyên văn vào <draft>)\n",
		) + summary
	}
	return prompt
}
