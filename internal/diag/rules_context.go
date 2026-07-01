package diag

import (
	"fmt"
	"strings"

	"github.com/voocel/ainovel-cli/internal/contentlang"
)

// GhostCharacter phát hiện nhân vật core/important vắng mặt lâu dài.
func GhostCharacter(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Characters) == 0 || len(snap.Summaries) == 0 {
		return nil
	}
	completed := snap.CompletedCount()
	if completed < 5 {
		return nil
	}

	// Tính số chương xuất hiện cuối cùng của từng nhân vật
	lastSeen := make(map[string]int)
	for ch, s := range snap.Summaries {
		for _, name := range s.Characters {
			if ch > lastSeen[name] {
				lastSeen[name] = ch
			}
		}
	}

	threshold := completed / 3
	if threshold < 5 {
		threshold = 5
	}
	latest := snap.LatestCompleted()

	var ghosts []string
	for _, c := range snap.Characters {
		if c.Tier != "core" && c.Tier != "important" {
			continue
		}
		seen, ok := lastSeen[c.Name]
		if !ok {
			// Cũng kiểm tra bí danh
			for _, alias := range c.Aliases {
				if s, exists := lastSeen[alias]; exists && s > seen {
					seen = s
					ok = true
				}
			}
		}
		gap := latest - seen
		if !ok {
			ghosts = append(ghosts, fmt.Sprintf(contentlang.Pick("%s(从未出现在摘要中)", "%s(chưa từng xuất hiện trong tóm tắt)"), c.Name))
		} else if gap > threshold {
			ghosts = append(ghosts, fmt.Sprintf(contentlang.Pick("%s(最后出现ch%d,已缺席%d章)", "%s(xuất hiện cuối ở ch%d, đã vắng %d chương)"), c.Name, seen, gap))
		}
	}
	if len(ghosts) == 0 {
		return nil
	}
	return []Finding{{
		Rule:       "GhostCharacter",
		Category:   CatContext,
		Severity:   SevInfo,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "context.characters",
		Title:      fmt.Sprintf(contentlang.Pick("角色消失: %d 个核心角色长期缺席", "Nhân vật biến mất: %d nhân vật cốt lõi vắng mặt lâu dài"), len(ghosts)),
		Evidence:   strings.Join(ghosts, "; "),
		Suggestion: contentlang.Pick("Writer 可能丢失了该角色的追踪。考虑直接在输入框提交干预指令重新引入该角色，或在 characters.json 中降级其 tier。", "Writer có thể đã mất dấu nhân vật này. Cân nhắc gửi trực tiếp lệnh can thiệp ở ô nhập để giới thiệu lại nhân vật, hoặc hạ tier của nó trong characters.json."),
	}}
}

// TimelineGaps phát hiện chương đã hoàn thành thiếu sự kiện dòng thời gian.
func TimelineGaps(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Progress.CompletedChapters) == 0 {
		return nil
	}
	if len(snap.Timeline) == 0 && snap.CompletedCount() > 0 {
		return []Finding{{
			Rule:       "TimelineGaps",
			Category:   CatContext,
			Severity:   SevInfo,
			Confidence: ConfMedium,
			AutoLevel:  AutoNone,
			Target:     "context.timeline",
			Title:      contentlang.Pick("时间线为空", "Dòng thời gian rỗng"),
			Evidence:   fmt.Sprintf("completed=%d, timeline_events=0", snap.CompletedCount()),
			Suggestion: contentlang.Pick("commit_chapter 的时间线提取可能未生效。检查 Writer 输出是否包含 timeline 字段。", "Trích xuất dòng thời gian của commit_chapter có thể chưa có hiệu lực. Kiểm tra output của Writer có chứa trường timeline không."),
		}}
	}

	// Lập ánh xạ chương → sự kiện
	chaptersWithEvents := make(map[int]bool)
	for _, e := range snap.Timeline {
		chaptersWithEvents[e.Chapter] = true
	}

	var missing []int
	for _, ch := range snap.Progress.CompletedChapters {
		if !chaptersWithEvents[ch] {
			missing = append(missing, ch)
		}
	}
	// Cho phép thiếu một ít (vài chương chuyển tiếp có thể thực sự không có sự kiện lớn)
	if len(missing) == 0 || float64(len(missing))/float64(snap.CompletedCount()) < ThresholdTimelineGapRate {
		return nil
	}
	return []Finding{{
		Rule:       "TimelineGaps",
		Category:   CatContext,
		Severity:   SevInfo,
		Confidence: ConfMedium,
		AutoLevel:  AutoNone,
		Target:     "context.timeline",
		Title:      fmt.Sprintf(contentlang.Pick("时间线缺口: %d 章无事件记录", "Khoảng trống dòng thời gian: %d chương không có ghi nhận sự kiện"), len(missing)),
		Evidence:   fmt.Sprintf("missing=[%s]", intsToStr(missing)),
		Suggestion: contentlang.Pick("commit_chapter 的时间线提取可能部分失效。检查 Writer 输出的 timeline 字段格式。", "Trích xuất dòng thời gian của commit_chapter có thể thất bại một phần. Kiểm tra định dạng trường timeline trong output của Writer."),
	}}
}

// RelationshipStagnation phát hiện dữ liệu quan hệ ngừng cập nhật.
func RelationshipStagnation(snap *Snapshot) []Finding {
	if snap.Progress == nil || len(snap.Relationships) == 0 {
		return nil
	}
	completed := snap.CompletedCount()
	if completed < 6 {
		return nil
	}

	// Tìm chương mới nhất của dữ liệu quan hệ
	latestRelCh := 0
	for _, r := range snap.Relationships {
		if r.Chapter > latestRelCh {
			latestRelCh = r.Chapter
		}
	}

	// Nếu dữ liệu quan hệ mới nhất nằm ở 1/3 đầu, phán định là đình trệ
	cutoff := snap.LatestCompleted() - completed/3
	if latestRelCh >= cutoff {
		return nil
	}
	return []Finding{{
		Rule:       "RelationshipStagnation",
		Category:   CatContext,
		Severity:   SevInfo,
		Confidence: ConfLow,
		AutoLevel:  AutoNone,
		Target:     "context.relationships",
		Title:      fmt.Sprintf(contentlang.Pick("关系数据停滞: 最新更新在第 %d 章", "Dữ liệu quan hệ đình trệ: cập nhật mới nhất ở chương %d"), latestRelCh),
		Evidence:   fmt.Sprintf("relationship_entries=%d, latest_update=ch%d, latest_completed=ch%d", len(snap.Relationships), latestRelCh, snap.LatestCompleted()),
		Suggestion: contentlang.Pick("commit_chapter 的关系更新可能停止工作，或故事关系确实无变化。检查 Writer 输出的 relationships 字段。", "Cập nhật quan hệ của commit_chapter có thể đã ngừng hoạt động, hoặc quan hệ trong truyện thực sự không thay đổi. Kiểm tra trường relationships trong output của Writer."),
	}}
}
