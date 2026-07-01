package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/contentlang"
	"github.com/voocel/ainovel-cli/internal/store"
)

// ExportRelPath là vị trí cố định của file chẩn đoán đã khử nhạy cảm, tương đối thư mục output (một bản ghi đè).
const ExportRelPath = "meta/diag-export.md"

// Export chẩn đoán đầy đủ + render + ghi xuống đĩa, trả về đường dẫn tuyệt đối đã ghi. Dùng cho headless / gọi từ ngoài.
func Export(s *store.Store) (string, error) {
	rep, rc := Diagnose(s)
	return WriteExport(s, rep, rc)
}

// WriteExport render Report + RuntimeCapture đã tính sẵn rồi ghi xuống đĩa, không bắt lại lần nữa.
// Dùng để lệnh /diag tái dùng kết quả của Diagnose.
func WriteExport(s *store.Store, rep Report, rc RuntimeCapture) (string, error) {
	data := RenderExport(rep, rc)
	abs := filepath.Join(s.Dir(), filepath.FromSlash(ExportRelPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", err
	}
	return abs, nil
}

// RenderExport ghép Report sáng tác + capture trạng thái chạy thành Markdown đã khử nhạy cảm.
func RenderExport(rep Report, rc RuntimeCapture) []byte {
	var b strings.Builder
	st := rep.Stats

	b.WriteString("# diag-export\n\n")
	fmt.Fprintf(&b, contentlang.Pick("> 生成时间 %s · %s/%s\n", "> Thời gian tạo %s · %s/%s\n"), time.Now().Format("2006-01-02 15:04:05"), rc.GoOS, rc.GoArch)
	b.WriteString(contentlang.Pick("> ⚠️ 已脱敏：小说正文 / prompt / 思考已移除，仅保留行为骨架。可直接贴到 issue。\n\n", "> ⚠️ Đã khử nhạy cảm: chính văn tiểu thuyết / prompt / suy nghĩ đã bị xóa, chỉ giữ khung hành vi. Có thể dán thẳng vào issue.\n\n"))

	// 1. Môi trường
	b.WriteString(contentlang.Pick("## 1. 环境\n\n", "## 1. Môi trường\n\n"))
	fmt.Fprintf(&b, contentlang.Pick("- 阶段 `%s`", "- Giai đoạn `%s`"), orDash(st.Phase))
	if st.Flow != "" {
		fmt.Fprintf(&b, " / flow `%s`", st.Flow)
	}
	fmt.Fprintf(&b, contentlang.Pick(" · 章节 %d/%d · 字数 %d\n", " · Chương %d/%d · Số chữ %d\n"), st.CompletedChapters, st.TotalChapters, st.TotalWords)
	if st.PlanningTier != "" {
		fmt.Fprintf(&b, contentlang.Pick("- 规划 `%s`\n", "- Quy hoạch `%s`\n"), st.PlanningTier)
	}
	for _, m := range rc.Models {
		fmt.Fprintf(&b, "- %s → `%s` / `%s`\n", m.Agent, orDash(m.Provider), orDash(m.Model))
	}

	// 2. Phát hiện chẩn đoán (chỉ trạng thái chạy; chẩn đoán loại sáng tác chứa cốt truyện/phục bút, để báo cáo trên màn hình /diag, không vào bản export có thể chia sẻ)
	b.WriteString(contentlang.Pick("\n## 2. 诊断发现（运行时）\n\n", "\n## 2. Phát hiện chẩn đoán (trạng thái chạy)\n\n"))
	rf := runtimeFindings(&rc)
	sortFindings(rf)
	if len(rf) == 0 {
		b.WriteString(contentlang.Pick("未发现运行时异常。\n", "Không phát hiện bất thường trạng thái chạy.\n"))
	} else {
		for _, f := range rf {
			fmt.Fprintf(&b, "- [%s] %s\n", f.Severity, f.Title)
			if f.Evidence != "" {
				fmt.Fprintf(&b, contentlang.Pick("  - 证据：%s\n", "  - Bằng chứng: %s\n"), f.Evidence)
			}
			if f.Suggestion != "" {
				fmt.Fprintf(&b, "  - → %s\n", f.Suggestion)
			}
		}
	}

	// 3. Tín hiệu trạng thái chạy (tổng hợp thô)
	b.WriteString(contentlang.Pick("\n## 3. 运行时信号\n\n", "\n## 3. Tín hiệu trạng thái chạy\n\n"))
	wrote := false
	if rc.CurrentStep != "" {
		fmt.Fprintf(&b, contentlang.Pick("- 当前 step `%s`\n", "- step hiện tại `%s`\n"), rc.CurrentStep)
		wrote = true
	}
	if rc.StuckStep != "" {
		fmt.Fprintf(&b, contentlang.Pick("- ⚠️ 卡住：连续停在 `%s` ×%d\n", "- ⚠️ Kẹt: liên tục dừng ở `%s` ×%d\n"), rc.StuckStep, rc.StuckCount)
		wrote = true
	}
	if len(rc.Repeats) > 0 {
		b.WriteString(contentlang.Pick("- 高频签名（近端窗口 ≥3 次，含正常重复工具，仅供参考）：\n", "- Chữ ký tần suất cao (cửa sổ cận điểm ≥3 lần, gồm cả công cụ lặp bình thường, chỉ để tham khảo):\n"))
		for _, r := range rc.Repeats {
			fmt.Fprintf(&b, "  - `%s` ×%d\n", r.Sig, r.Count)
		}
		wrote = true
	}
	if len(rc.DupContent) > 0 {
		b.WriteString(contentlang.Pick("- 反复生成同段文本（同 sha）：\n", "- Liên tục sinh cùng một đoạn văn (cùng sha):\n"))
		for _, d := range rc.DupContent {
			fmt.Fprintf(&b, "  - sha=%s ×%d\n", d.Sha, d.Count)
		}
		wrote = true
	}
	if len(rc.LogKinds) > 0 {
		b.WriteString(contentlang.Pick("- 日志错误分类：", "- Phân loại lỗi log: "))
		b.WriteString(joinKinds(rc.LogKinds))
		b.WriteString("\n")
		wrote = true
	}
	if rc.LogErrors > 0 || rc.LogWarns > 0 {
		fmt.Fprintf(&b, contentlang.Pick("- 日志 error ×%d · warn ×%d\n", "- log error ×%d · warn ×%d\n"), rc.LogErrors, rc.LogWarns)
		wrote = true
	}
	if rc.StopGuard > 0 {
		fmt.Fprintf(&b, contentlang.Pick("- StopGuard 拦截 ×%d\n", "- StopGuard chặn ×%d\n"), rc.StopGuard)
		wrote = true
	}
	if !wrote {
		b.WriteString(contentlang.Pick("- 无明显运行时异常信号。\n", "- Không có tín hiệu bất thường trạng thái chạy rõ rệt.\n"))
	}

	// 4. Phần đuôi khung hành vi
	fmt.Fprintf(&b, contentlang.Pick("\n## 4. 行为骨架尾巴（末 %d 条）\n\n", "\n## 4. Phần đuôi khung hành vi (%d mục cuối)\n\n"), len(rc.Tail))
	if len(rc.Tail) == 0 {
		b.WriteString(contentlang.Pick("（无会话记录）\n", "(Không có ghi nhận hội thoại)\n"))
	} else {
		b.WriteString("```\n")
		for _, ev := range rc.Tail {
			b.WriteString(formatSkel(ev))
			b.WriteString("\n")
		}
		b.WriteString("```\n")
	}

	// 5. Tự kiểm khử nhạy cảm
	b.WriteString(contentlang.Pick("\n## 5. 脱敏自检\n\n", "\n## 5. Tự kiểm khử nhạy cảm\n\n"))
	fmt.Fprintf(&b, contentlang.Pick("- 打码文本块 %d 处 · 正文出包 0 处\n", "- Khối văn bản bị che %d chỗ · chính văn ra gói 0 chỗ\n"), rc.RedactedTexts)
	if len(rc.Sources) > 0 {
		fmt.Fprintf(&b, contentlang.Pick("- 数据源：%s\n", "- Nguồn dữ liệu: %s\n"), strings.Join(rc.Sources, " · "))
	}

	return []byte(b.String())
}

// formatSkel render một khung thành một dòng đơn, để xem thứ tự trước sau của việc điều phối.
func formatSkel(ev SkelEvent) string {
	var parts []string
	parts = append(parts, "["+ev.Agent+"/"+ev.Role+"]")
	for _, t := range ev.Tools {
		parts = append(parts, t.Name+formatArgs(t.Args)+invalidTag(t))
	}
	if ev.ErrClass != "" {
		parts = append(parts, "err: "+ev.ErrClass)
	}
	if len(ev.Tools) == 0 && ev.ErrClass == "" && ev.TextSha != "" {
		parts = append(parts, "text<sha="+ev.TextSha+">")
	}
	return strings.Join(parts, " ")
}

func formatArgs(args map[string]string) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+": "+args[k])
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

func invalidTag(t SkelTool) string {
	if !t.Invalid {
		return ""
	}
	if t.ParseErr != "" {
		return " ⚠️args-invalid(" + firstLine(t.ParseErr, 80) + ")"
	}
	return " ⚠️args-invalid"
}

func joinKinds(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s ×%d", k, m[k]))
	}
	return strings.Join(parts, " · ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
