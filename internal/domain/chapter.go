package domain

import (
	"fmt"
	"unicode/utf8"

	"github.com/voocel/ainovel-cli/internal/contentlang"
)

// ReviewInterval là khoảng rà soát toàn cục (cứ N chương kích hoạt một lần).
const ReviewInterval = 5

// ShouldReview xác định có cần rà soát toàn cục không dựa trên số chương đã hoàn thành (chế độ truyện ngắn/vừa).
func ShouldReview(completedCount int) (bool, string) {
	if completedCount > 0 && completedCount%ReviewInterval == 0 {
		return true, contentlang.Pick(
			fmt.Sprintf("已完成 %d 章，触发全局审阅", completedCount),
			fmt.Sprintf("Đã hoàn thành %d chương, kích hoạt rà soát toàn cục", completedCount),
		)
	}
	return false, ""
}

// ShouldArcReview xác định ở chế độ truyện dài có cần rà soát cấp cung truyện/cấp quyển không.
func ShouldArcReview(isArcEnd, isVolumeEnd bool, volume, arc int) (bool, string) {
	if isVolumeEnd {
		return true, contentlang.Pick(
			fmt.Sprintf("第 %d 卷第 %d 弧结束（卷结束），触发弧级+卷级评审", volume, arc),
			fmt.Sprintf("Quyển %d cung %d kết thúc (kết thúc quyển), kích hoạt rà soát cấp cung + cấp quyển", volume, arc),
		)
	}
	if isArcEnd {
		return true, contentlang.Pick(
			fmt.Sprintf("第 %d 卷第 %d 弧结束，触发弧级评审", volume, arc),
			fmt.Sprintf("Quyển %d cung %d kết thúc, kích hoạt rà soát cấp cung", volume, arc),
		)
	}
	return false, ""
}

// WordCount tính số chữ theo rune.
func WordCount(content string) int {
	return utf8.RuneCountInString(content)
}
