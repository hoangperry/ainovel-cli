package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestCommitChapterRejectsNonPendingRewrite(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Progress.Init("test", 10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := store.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := store.Progress.SetPendingRewrites([]int{2}, "测试重写"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := store.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}
	if err := store.Drafts.SaveDraft(3, "这是错误章节的正文。"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := NewCommitChapterTool(store)
	args, err := json.Marshal(map[string]any{
		"chapter":         3,
		"summary":         "错误提交",
		"characters":      []string{"主角"},
		"key_events":      []string{"误提交"},
		"timeline_events": []any{},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("expected commit to be rejected during rewrite flow")
	}

	if _, err := os.Stat(dir + "/chapters/03.md"); !os.IsNotExist(err) {
		t.Fatalf("chapter should not be persisted, stat err=%v", err)
	}

	progress, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if len(progress.CompletedChapters) != 1 || progress.CompletedChapters[0] != 2 {
		t.Fatalf("completed chapters should only contain original chapter 2, got %v", progress.CompletedChapters)
	}
	if progress.CurrentChapter != 3 {
		t.Fatalf("current chapter should not advance beyond original progress, got %d", progress.CurrentChapter)
	}
}

func TestCommitChapterAllowsPendingRewrite(t *testing.T) {
	dir := t.TempDir()
	store := store.NewStore(dir)
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := store.Progress.Init("test", 10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	if err := store.Progress.MarkChapterComplete(2, 3000, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}
	if err := store.Progress.SetPendingRewrites([]int{2}, "测试重写"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := store.Progress.SetFlow(domain.FlowRewriting); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}
	if err := store.Drafts.SaveDraft(2, "这是正确待重写章节的正文。"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := NewCommitChapterTool(store)
	args, err := json.Marshal(map[string]any{
		"chapter":         2,
		"summary":         "正确提交",
		"characters":      []string{"主角"},
		"key_events":      []string{"完成重写"},
		"timeline_events": []any{},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, err := os.Stat(dir + "/chapters/02.md"); err != nil {
		t.Fatalf("chapter should be persisted: %v", err)
	}

	progress, err := store.Progress.Load()
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if len(progress.CompletedChapters) != 1 || progress.CompletedChapters[0] != 2 {
		t.Fatalf("unexpected completed chapters: %v", progress.CompletedChapters)
	}
	pending, err := store.Signals.LoadPendingCommit()
	if err != nil {
		t.Fatalf("LoadPendingCommit: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected pending commit cleared, got %+v", pending)
	}
}

// TestCommitChapterUpdatesCastLedger kiểm chứng: commit_chapter cộng dồn characters của chương này vào cast_ledger,
// brief_role do cast_intros cung cấp được áp dụng, và nhân vật cốt lõi trong characters.json không vào ledger.
func TestCommitChapterUpdatesCastLedger(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}
	// Thiết lập hồ sơ nhân vật cốt lõi (những cái này không nên vào cast_ledger)
	if err := s.Characters.Save([]domain.Character{
		{Name: "林墨", Role: "主角", Tier: "core"},
		{Name: "李清砚", Role: "导师", Tier: "important"},
	}); err != nil {
		t.Fatalf("Save core characters: %v", err)
	}
	if err := s.Drafts.SaveDraft(1, "第一章正文，林墨遇到客栈老板老周与小厮阿云。"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    1,
		"summary":    "林墨入住客栈",
		"characters": []string{"林墨", "李清砚", "老周", "阿云"},
		"key_events": []string{"入住"},
		"cast_intros": []any{
			map[string]any{"name": "老周", "brief_role": "客栈老板"},
			map[string]any{"name": "阿云", "brief_role": "客栈小厮"},
		},
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entries, err := s.Cast.Load()
	if err != nil {
		t.Fatalf("Cast.Load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries (老周/阿云), got %d: %+v", len(entries), entries)
	}
	byName := map[string]domain.CastEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if e, ok := byName["老周"]; !ok || e.BriefRole != "客栈老板" || e.FirstSeenChapter != 1 {
		t.Errorf("老周 entry wrong: %+v", e)
	}
	if e, ok := byName["阿云"]; !ok || e.BriefRole != "客栈小厮" || e.AppearanceCount != 1 {
		t.Errorf("阿云 entry wrong: %+v", e)
	}
	if _, ok := byName["林墨"]; ok {
		t.Errorf("核心角色 林墨 不应进 ledger")
	}
	if _, ok := byName["李清砚"]; ok {
		t.Errorf("核心角色 李清砚 不应进 ledger")
	}
}

// TestCommitChapterRejectsPolishWithoutDraftChange kiểm chứng: sau khi chương đã hoàn thành vào hàng đợi mài giũa/viết lại,
// nếu writer bỏ qua draft_chapter mà commit thẳng (drafts và chapters nội dung giống hệt),
// commit_chapter phải từ chối, buộc writer gọi draft_chapter ghi phiên bản mới trước.
// TestCommitChapterNonLayeredRecompletesAfterRework kiểm chứng sách phi phân tầng sau khi hoàn thành rồi reopen làm lại,
// sửa xong chương commit, khi hàng đợi sạch có thể tự động quay lại complete (nhánh phi phân tầng phán kết thúc sau khi bù drain).
func TestCommitChapterNonLayeredRecompletesAfterRework(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 2); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// Viết xong hai chương và kết thúc. Chương 2 chuẩn bị đủ drafts/chapters, để commit làm lại.
	ch2 := "第二章原始正文，用于模拟已提交终稿。"
	if err := s.Drafts.SaveDraft(2, ch2); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(2, ch2); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	if err := s.Progress.MarkChapterComplete(1, 100, "", ""); err != nil {
		t.Fatalf("MarkChapterComplete(1): %v", err)
	}
	if err := s.Progress.MarkChapterComplete(2, len([]rune(ch2)), "", ""); err != nil {
		t.Fatalf("MarkChapterComplete(2): %v", err)
	}
	if err := s.Progress.MarkComplete(); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}

	// reopen chương 2 → phase về writing, PendingRewrites=[2], flow=rewriting
	if err := s.Progress.Reopen([]int{2}, "返工"); err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	// Commit làm lại (bản nháp phải khác bản chốt mới được thông qua)
	if err := s.Drafts.SaveDraft(2, ch2+"\n\n返工新增段落。"); err != nil {
		t.Fatalf("SaveDraft (reworked): %v", err)
	}
	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"summary":    "返工后摘要",
		"characters": []string{"主角"},
		"key_events": []string{"清理"},
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute rework commit: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload["book_complete"] != true {
		t.Errorf("book_complete = %v, want true", payload["book_complete"])
	}

	p, _ := s.Progress.Load()
	if p.Phase != domain.PhaseComplete {
		t.Errorf("phase = %s, want complete (应自动重新收尾)", p.Phase)
	}
	if len(p.PendingRewrites) != 0 {
		t.Errorf("PendingRewrites = %v, want empty", p.PendingRewrites)
	}
}

// TestCommitChapterLayeredReopenRecompletesDespiteOpenThread kiểm chứng việc thu xếp: sách phân tầng sau khi reopen
// làm lại, dù compass vẫn còn sợi truyện dài chưa thu lại (làm lại có thể gây xáo trộn), sau khi sạch vẫn theo "cấu trúc đầy đủ" kết thúc lại —
// không kẹt ở writing, dứt điểm vòng lặp viết tiếp vượt biên ở cuối quyển cuối (§6.5 / họ known_outline_exhaustion).
// Phản chứng: nếu đường reopen vẫn dùng layeredBookComplete cấp chất lượng, open thread trong ví dụ này sẽ khiến nó trả false,
// book_complete là giả, test sẽ thất bại.
func TestCommitChapterLayeredReopenRecompletesDespiteOpenThread(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// Một quyển một cung truyện hai chương, tất cả đều mở rộng
	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "卷一", "theme": "主题",
			"arcs": []map[string]any{{
				"index": 1, "title": "弧一", "goal": "目标",
				"chapters": []map[string]any{
					{"title": "首章", "core_event": "起", "hook": "续"},
					{"title": "次章", "core_event": "承", "hook": "终"},
				},
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}

	// Viết xong hai chương, lưu đĩa và kết thúc
	ch2 := "第二章原始正文，模拟已提交终稿。"
	for ch, body := range map[int]string{1: "第一章正文。", 2: ch2} {
		if err := s.Drafts.SaveDraft(ch, body); err != nil {
			t.Fatalf("SaveDraft %d: %v", ch, err)
		}
		if err := s.Drafts.SaveFinalChapter(ch, body); err != nil {
			t.Fatalf("SaveFinalChapter %d: %v", ch, err)
		}
		if err := s.Progress.MarkChapterComplete(ch, len([]rune(body)), "", ""); err != nil {
			t.Fatalf("MarkChapterComplete %d: %v", ch, err)
		}
	}
	if err := s.Progress.MarkComplete(); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}

	// Mô phỏng "làm lại gây xáo trộn sợi truyện dài": compass vẫn còn open thread chưa thu lại
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "主角归乡", OpenThreads: []string{"宿敌未除"}}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}

	// reopen chương 2 → commit làm lại (bản nháp phải khác bản chốt mới được thông qua)
	if err := s.Progress.Reopen([]int{2}, "返工"); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if err := s.Drafts.SaveDraft(2, ch2+"\n\n返工新增段落。"); err != nil {
		t.Fatalf("SaveDraft reworked: %v", err)
	}
	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter": 2, "summary": "返工摘要", "characters": []string{"主角"}, "key_events": []string{"清理"},
	})
	raw, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute rework commit: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if bc, _ := out["book_complete"].(bool); !bc {
		t.Error("reopen 返工排空后应按结构完整重新完结（即便长线未收束）")
	}
	p, _ := s.Progress.Load()
	if p.Phase != domain.PhaseComplete {
		t.Errorf("phase = %s, want complete", p.Phase)
	}
	if p.ReopenedFromComplete {
		t.Error("重新完结后 ReopenedFromComplete 应被清除")
	}
}

func TestCommitChapterRejectsPolishWithoutDraftChange(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 10); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// Mô phỏng chương 2 đã hoàn thành bình thường: drafts và chapters nội dung giống nhau.
	original := "第二章原始正文内容，用于模拟已提交终稿。"
	if err := s.Drafts.SaveDraft(2, original); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.Drafts.SaveFinalChapter(2, original); err != nil {
		t.Fatalf("SaveFinalChapter: %v", err)
	}
	if err := s.Progress.MarkChapterComplete(2, len([]rune(original)), "mystery", "quest"); err != nil {
		t.Fatalf("MarkChapterComplete: %v", err)
	}

	// Vào hàng đợi mài giũa: Flow=Polishing, PendingRewrites=[2]
	if err := s.Progress.SetPendingRewrites([]int{2}, "测试打磨"); err != nil {
		t.Fatalf("SetPendingRewrites: %v", err)
	}
	if err := s.Progress.SetFlow(domain.FlowPolishing); err != nil {
		t.Fatalf("SetFlow: %v", err)
	}

	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"summary":    "假装打磨了",
		"characters": []string{"主角"},
		"key_events": []string{"无改动"},
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected commit to be rejected when drafts equals final content")
	}

	// Viết thêm một bản nháp khác → phải thông qua
	polished := original + "\n\n打磨后新增段落。"
	if err := s.Drafts.SaveDraft(2, polished); err != nil {
		t.Fatalf("SaveDraft (polished): %v", err)
	}
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute after real polish: %v", err)
	}
}

// TestCommitChapterLayeredRejectsOutOfRangeChapter kiểm chứng ở chế độ phân tầng,
// commit có số chương vượt ngoài layered_outline phải thất bại cứng, chứ không phải slog.Warn rồi cho qua.
// Đây là phanh vật lý ngăn "sau khi phán định sai writer chạy trần một mạch" (vụ "Phàm Cốt" ch204..347).
func TestCommitChapterLayeredRejectsOutOfRangeChapter(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// Dựng một layered_outline, chỉ có 1 quyển 1 cung truyện 1 chương
	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "卷一", "theme": "主题",
			"arcs": []map[string]any{{
				"index": 1, "title": "弧一", "goal": "目标",
				"chapters": []map[string]any{
					{"title": "首章", "core_event": "起", "hook": "续"},
				},
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	// commit chương 2 vượt biên phải thất bại cứng
	if err := s.Drafts.SaveDraft(2, "越界章节正文，必须被拦下。"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter":    2,
		"summary":    "越界章节",
		"characters": []string{"主角"},
		"key_events": []string{"不该被允许"},
	})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected commit to fail when chapter out of layered outline range")
	}

	// File chương không nên lưu đĩa, Progress không nên tiến lên
	if _, statErr := os.Stat(dir + "/chapters/02.md"); !os.IsNotExist(statErr) {
		t.Fatalf("chapter 2 should not be persisted, stat err=%v", statErr)
	}
	progress, _ := s.Progress.Load()
	if len(progress.CompletedChapters) != 0 {
		t.Fatalf("CompletedChapters should stay empty, got %v", progress.CompletedChapters)
	}
}

// TestCommitChapterLayeredAutoCompletesWhenDone kiểm chứng dự phòng kết thúc xác định ở chế độ phân tầng:
// khi dàn ý đã mở rộng và viết xong hết + không còn cung truyện khung + không làm lại + phục bút đang hoạt động bằng không + sợi truyện dài của compass đã thu lại,
// commit chương cuối tự động đẩy Phase=Complete, không phụ thuộc Architect chủ động gọi complete_book.
// Đây là bản sửa cho livelock được đưa vào sau khi 9bf26a5 xóa tự động kết thúc phân tầng (cuối quyển cuối model không append
// cũng không complete → writer chạy trần vượt biên vòng lặp).
func TestCommitChapterLayeredAutoCompletesWhenDone(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	// Một quyển một cung truyện hai chương, tất cả đều mở rộng (không có cung truyện khung)
	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "卷一", "theme": "主题",
			"arcs": []map[string]any{{
				"index": 1, "title": "弧一", "goal": "目标",
				"chapters": []map[string]any{
					{"title": "首章", "core_event": "起", "hook": "续"},
					{"title": "次章", "core_event": "承", "hook": "终"},
				},
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}
	// Sợi truyện dài của compass đã thu lại (OpenThreads rỗng)
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "主角归乡"}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	tool := NewCommitChapterTool(s)
	commit := func(ch int) map[string]any {
		if err := s.Drafts.SaveDraft(ch, fmt.Sprintf("第 %d 章正文内容，用于测试确定性完结。", ch)); err != nil {
			t.Fatalf("SaveDraft %d: %v", ch, err)
		}
		args, _ := json.Marshal(map[string]any{
			"chapter": ch, "summary": "摘要", "characters": []string{"主角"}, "key_events": []string{"事件"},
		})
		raw, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute ch%d: %v", ch, err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("Unmarshal ch%d: %v", ch, err)
		}
		return out
	}

	// Chương 1: chưa viết xong, không nên kết thúc
	if bc, _ := commit(1)["book_complete"].(bool); bc {
		t.Fatal("写完第 1 章不应触发完结")
	}
	if p, _ := s.Progress.Load(); p.Phase == domain.PhaseComplete {
		t.Fatal("写完第 1 章 phase 不应为 complete")
	}

	// Chương 2 (chương cuối): phải tự động kết thúc
	if bc, _ := commit(2)["book_complete"].(bool); !bc {
		t.Fatal("写完最后一章应自动完结")
	}
	if p, _ := s.Progress.Load(); p.Phase != domain.PhaseComplete {
		t.Fatalf("expected phase=complete, got %s", p.Phase)
	}
}

// TestCommitChapterLayeredNoAutoCompleteWithOpenThreads kiểm chứng tính bảo thủ: khi vẫn còn sợi truyện dài đang hoạt động
// thì dù chương viết đủ cũng không tự động kết thúc, để quyền phán quyết "có tiếp tục không" cho Architect.
func TestCommitChapterLayeredNoAutoCompleteWithOpenThreads(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Progress.Init("test", 0); err != nil {
		t.Fatalf("InitProgress: %v", err)
	}

	foundation := NewSaveFoundationTool(s)
	layeredArgs, _ := json.Marshal(map[string]any{
		"type": "layered_outline",
		"content": []map[string]any{{
			"index": 1, "title": "卷一", "theme": "主题",
			"arcs": []map[string]any{{
				"index": 1, "title": "弧一", "goal": "目标",
				"chapters": []map[string]any{{"title": "首章", "core_event": "起", "hook": "续"}},
			}},
		}},
		"scale": "long",
	})
	if _, err := foundation.Execute(context.Background(), layeredArgs); err != nil {
		t.Fatalf("Execute layered: %v", err)
	}
	// Vẫn còn sợi truyện dài đang hoạt động chưa thu lại
	if err := s.Outline.SaveCompass(domain.StoryCompass{EndingDirection: "主角归乡", OpenThreads: []string{"宿敌未除"}}); err != nil {
		t.Fatalf("SaveCompass: %v", err)
	}
	_ = s.Progress.UpdatePhase(domain.PhaseWriting)

	if err := s.Drafts.SaveDraft(1, "唯一一章的正文，但长线未收束。"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	tool := NewCommitChapterTool(s)
	args, _ := json.Marshal(map[string]any{
		"chapter": 1, "summary": "摘要", "characters": []string{"主角"}, "key_events": []string{"事件"},
	})
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if p, _ := s.Progress.Load(); p.Phase == domain.PhaseComplete {
		t.Fatal("活跃长线未收束时不应自动完结")
	}
}
