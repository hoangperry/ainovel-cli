# Kiến trúc runtime của ainovel-cli

> [English](architecture.en.md)

> Để LLM viết xong cả một cuốn tiểu thuyết trong một lần Run, Host chỉ làm khởi động / khôi phục / định tuyến / quan sát, quyền quyết định để dành tối đa cho mô hình.

---

## 1. Mục tiêu (theo thứ tự ưu tiên)

1. **Ổn định**: một câu đầu vào, viết xong ổn định cả cuốn tiểu thuyết (200~500 chương). Giữa chừng không tự ngắt vì vấn đề kiến trúc.
2. **Chất lượng có thể lặp**: prompt / tài liệu tham khảo / chiều rà soát / chiến lược context có thể điều chỉnh độc lập, không kéo theo kiến trúc.
3. **Khôi phục được**: sau khi sập, mất mạng, tạm dừng có thể tiếp tục từ checkpoint gần nhất.
4. **Quan sát được**: tiến độ, sản phẩm, thời gian dùng của mỗi chương mỗi step đều tra được.

"Ổn định" là tiền đề, "chất lượng" là tầng trên. Mỗi quyết định kiến trúc ưu tiên phục vụ tính ổn định.

---

## 2. Nguyên tắc cốt lõi

### 2.1 LLM lo sáng tác và phán định, Host lo định tuyến quy trình

Không gian quyết định của agent chuyên ngành dọc là đóng kín: lưu đồ cố định, nhánh hữu hạn, dẫn dắt bằng sự kiện. Hai loại quyết định đi qua hai phương tiện khác nhau:

- **Sáng tác và phán định** (hiểu ngữ nghĩa/chất lượng/ý đồ) → LLM. Năng lực phán định của Writer/Editor/Architect/Coordinator hưởng lợi tuyến tính theo việc nâng cấp mô hình
- **Định tuyến quy trình** (đọc sự kiện tra bảng) → mã. `flow.Router` là hàm thuần + unit test, tỉ lệ lỗi tiệm cận 0

Host không gọi trực tiếp SubAgent, mà tại mỗi TurnEnd của Coordinator để Flow Router tính ra chỉ thị, rồi hạ lệnh qua `coordinator.FollowUp("[Host hạ chỉ thị]…")`.

### 2.2 Tool là giao diện duy nhất của tầng sự kiện

Mọi tương tác với hệ thống tệp, Progress, Checkpoint đều do tool thực hiện. **Tool loại ghi bắt buộc bộ ba nguyên tử**: artifact ghi đĩa + Progress tiến tới + Checkpoint ghi thêm, hoàn tất trong khóa loại trừ lẫn nhau. Chạy lại cùng một tool cho kết quả y hệt hoặc bỏ qua thẳng (idempotent theo digest).

### 2.3 Tầng quan sát chỉ quan sát

UI, chẩn đoán, log sự kiện đều là người tiêu thụ bị động được chiếu ra từ luồng sự kiện / artifact chỉ-đọc. Đọc sự kiện, không sinh ra sự kiện, không ảnh hưởng luồng điều khiển.

**`internal/diag` là phân hệ quan sát duy nhất của engine** — cơ sở hỗ trợ hạng nhất, nhưng không phải lõi sản phẩm (lõi là engine sáng tác ở §6; mất diag thì vẫn viết tiểu thuyết được). Nó đọc chéo gần như mọi artifact + session + log + checkpoint, đảm nhiệm hai vai: ① **chẩn đoán chất lượng sáng tác** (rule → Finding, báo cáo trên màn hình `/diag`); ② **gỡ lỗi runtime + xuất khử nhạy cảm** (lột chính văn để lấy khung hành vi + gộp vòng lặp → ghi đè `meta/diag-export.md`, để người dùng dán vào issue; người bảo trì không có output cục bộ vẫn định vị được các vấn đề loại lặp vô hạn/đứt giữa chừng).

**Kỷ luật của người quan sát (không được nới lỏng)**: diag có thể chẩn đoán, có thể gợi ý, nhưng **không bao giờ tự ra tay** — không tự sửa, không tự chạy tiếp, không đổi quy trình. Nó càng mạnh càng có người muốn nó "tiện tay sửa giùm", càng phải giữ chặt điều này, nếu không sẽ đâm lại đúng những hố như idleResume / StallDetector đã xóa (xem §10.5, §10.14). Cấu trúc đối ngoại (như `RuntimeCapture`) hãy bảo trì như hợp đồng hạ tầng, đừng tùy tiện đổi trường.

### 2.4 Tầng sự kiện phẳng

Chỉ có ba loại sự kiện:

- **Progress** — chỉ mục tiến độ (đã viết tới chương mấy, danh sách chờ viết lại)
- **Checkpoint** — bản ghi tiến tới cấp step (plan / draft / commit / review / arc_summary)
- **Artifact** — chính văn chương, dàn ý, nhân vật, tóm tắt và các sản phẩm khác

Không đưa vào các trừu tượng như WorkflowInstance / TaskInstance / Command / Dispatcher.

### 2.5 Ba luật thép

**Luật thép một: tool chỉ trả về sự kiện, không trả về chỉ thị điều phối liên cuộc gọi**. `commit_chapter` trả về các trường có cấu trúc như `arc_end_reached` / `next_skeleton_arc`; không kẹp theo chuỗi chỉ thị loại `[hệ thống]`. Trường `next_step` bên trong subagent là chỉ dẫn nội tuyến mang tính trần thuật sự kiện ("tôi vừa lưu plan, bước tiếp theo là draft"), không tính là vi phạm — xem §6.4.

**Luật thép hai: định tuyến quy trình do Flow Router đảm nhiệm**. `Route(state) → *Instruction` trong `internal/host/flow/router.go` là hàm thuần, sau khi đăng ký `EventToolExecEnd` thì hạ lệnh qua `FollowUp`. Trả về nil nghĩa là "tình huống cần phán định, để LLM tự chủ". **Kênh chỉ thị không im lặng**: khi Route tính ra cùng một chỉ thị liên tiếp (cho thấy sau lần phái lệnh trước trạng thái chưa tiến tới), Dispatcher đính kèm sự kiện "hạ lệnh lần thứ N" để phát lại thay vì nuốt im lặng — "kết quả định tuyến trùng lặp" là sự kiện chỉ Host quan sát được, im lặng sẽ khiến Coordinator rơi vào mâu thuẫn kép "không có chỉ thị thì không được hành động / StopGuard không cho dừng". Không đặt ngưỡng, không ngắt mạch, thoát hiểm thế nào do LLM phán định.

**Luật thép ba: Coordinator không được phép vật lý end_turn, trừ khi Phase=Complete**. StopGuard chặn `end_turn` ở tầng agentcore và tiêm user message; chặn liên tiếp 5 lần không nổi thì leo thang terminate. Ba subagent (architect / writer / editor) có `CheckpointDeltaGuard` riêng.

---

## 3. Toàn cảnh kiến trúc

```
[Entry: TUI / headless]
        │ prompt / steer
[Host vỏ mỏng]
   ├── observer        sự kiện → chiếu UI/log
   ├── flow.Dispatcher đăng ký ToolExecEnd → Route(state) → FollowUp
   └── usage / quản lý mô hình
        │
[Coordinator (LLM, MaxTurns=100_000)]
   ├── lúc khởi động phán định architect_short / long
   ├── nhận [Host hạ chỉ thị] → sinh subagent tool_call
   └── nhận [Người dùng can thiệp] → tự chủ phán định
        │
[architect / writer / editor SubAgent (mỗi cái run + context + mô hình độc lập)]
        │ gọi tool
[Tools]  novel_context · read_chapter · plan_chapter · draft_chapter · edit_chapter
         check_consistency · commit_chapter · save_review · save_arc_summary
         save_volume_summary · save_foundation
        │ bộ ba nguyên tử
[Store: hệ thống tệp (tmp + rename)]
   Progress · Checkpoints · Outline · Drafts · Summaries · Characters · World · Signals
```

| Tầng | Làm gì | Không làm gì |
|---|---|---|
| Entry | Hiển thị, nhận đầu vào | Quyết định nghiệp vụ |
| Host | Khởi động/khôi phục/can thiệp/chiếu sự kiện/định tuyến Flow | Vượt qua Coordinator gọi thẳng SubAgent; ghi trạng thái |
| Coordinator | Thực thi chỉ thị Host, phán định Steer của người dùng, lúc khởi động chọn nhà quy hoạch | Tự quyết bước tiếp theo mỗi chương; ghi tệp |
| Agents | Suy nghĩ, viết, rà soát | Đọc/ghi Store trực tiếp |
| Tools | IO nguyên tử + checkpoint + idempotent | Chỉ thị điều phối liên subagent |
| Store | Ghi đĩa hệ thống tệp | Logic nghiệp vụ |

Phụ thuộc một chiều: `entry → host → agents → tools → store → domain`. `tools/` không tham chiếu `agents/host/`, `host/` không tham chiếu thẳng `tools/store/`. Module độc lập theo phương ngang: `errs/` có thể được mọi tầng tham chiếu, `diag/` đăng ký luồng sự kiện host + đọc-chỉ `store/`.

---

## 4. Mô hình dữ liệu

### 4.1 Progress (`internal/domain/runtime.go`)

```go
type Progress struct {
    NovelName         string
    Phase             Phase           // init / premise / outline / writing / complete
    CurrentChapter    int
    TotalChapters     int
    CompletedChapters []int
    TotalWordCount    int
    ChapterWordCounts map[int]int
    InProgressChapter int             // chương đang viết
    Flow              FlowState       // writing / reviewing / rewriting / polishing / steering
    PendingRewrites   []int
    StrandHistory     []string        // chuỗi dominant_strand
    HookHistory       []string        // chuỗi hook_type
    CurrentVolume, CurrentArc int     // phân tầng truyện dài
    Layered           bool
}
```

Logic điều khiển chỉ đọc các trường sự kiện trên, không phụ thuộc bất kỳ "dấu thời gian cập nhật" nào — thông tin thời gian do `OccurredAt` của checkpoint mang theo.

### 4.2 Checkpoint (`internal/domain/checkpoint.go`)

```go
type Scope      struct { Kind ScopeKind; Chapter, Volume, Arc int }
type Checkpoint struct {
    Seq        int64       // tăng đơn điệu
    Scope      Scope       // chapter / arc / volume / global
    Step       string      // plan / draft / commit / review / arc_summary / ...
    Artifact   string
    Digest     string
    OccurredAt time.Time
}
```

Lưu trữ: `meta/checkpoints.jsonl`, chỉ ghi thêm. Ghi lặp cùng `Scope+Step+Digest` được coi là idempotent, không sinh dòng mới.

### 4.3 Artifact và Signals

Artifact nằm ở `store/outline.go` `drafts.go` `summaries.go` `characters.go` `world.go` — mỗi loại sản phẩm đều có thể được checkpoint tham chiếu.

Signals: `PendingCommit` (khôi phục khi commit bị đứt) / `PendingSteer` (người dùng can thiệp trong lúc dừng máy). Đọc lúc khởi động/khôi phục, lúc runtime không đọc.

---

## 5. Quy ước tool

Tool là điểm tương tác duy nhất giữa tầng sự kiện và Agent.

### 5.1 Tool loại đọc

`novel_context(scope)` / `read_chapter(n)` — gọi được bất cứ lúc nào, không phụ thuộc trạng thái tiền đề, trả về dữ liệu đủ để LLM tự quyết định độc lập.

### 5.2 Tool loại ghi (bộ ba nguyên tử)

Mỗi lần gọi thành công bắt buộc: artifact ghi đĩa → Progress tiến tới → checkpoint ghi thêm. Ba bước hoàn tất trong khóa loại trừ lẫn nhau.

| Tool | Artifact | Step |
|---|---|---|
| `plan_chapter` | drafts/chXX.plan.json | plan |
| `draft_chapter` | drafts/chXX.draft.md | draft |
| `edit_chapter` | drafts/chXX.draft.md | edit |
| `check_consistency` | không (chỉ đọc, trả về inline) | consistency_check |
| `commit_chapter` | chapters/chXX.md + Progress | commit |
| `save_review` | reviews/chXX.json (global là chXX-global.json) | review |
| `save_arc_summary` | summaries/arc-vNNaNN.json | arc_summary |
| `save_volume_summary` | summaries/vol-vNN.json | volume_summary |
| `save_foundation` | foundation/*.json | premise / outline / layered_outline / characters / world_rules / expand_arc / append_volume / update_compass / complete_book |

`commit_chapter` đảm nhiệm phát hiện hoàn thành cung truyện/quyển/cả sách, trả về 19 trường sự kiện (`arc_end` / `needs_expansion` / `book_complete`…; khi bật kiểm tra rule cơ học thì đính thêm `rule_violations`). `save_review` đảm nhiệm leo thang verdict (môn chốt thẻ chấm điểm, hợp đồng missed → rewrite). Những logic trước đây rải rác ở tầng policy nay được cố định bên trong tool.

`edit_chapter` là lớp bọc mỏng của `agentcore.EditTool`, kiểm tra quyền sở hữu bảo đảm chương đã hoàn thành phải nằm trong `PendingRewrites` mới được sửa.

### 5.3 Phân tầng lỗi

| Loại lỗi | Tầng xử lý | Hành động |
|---|---|---|
| Timeout mạng / EOF streaming | Tools | Thử lại 3 lần |
| provider 429/503 | litellm | failover sang provider dự phòng |
| Xác thực / mô hình không tồn tại | Tools | ném terminal |
| Thiếu artifact tiền đề | Tools | ném conflict, LLM gọi `novel_context` rồi thử lại |
| Tham số tool không hợp lệ | Tools | ném validation, LLM sửa tham số |
| Cạn MaxTurns | agentcore | run kết thúc, Host phát done |
| Thông điệp LLM không hợp lệ (thinking-only stop…) | agentcore (`llm/litellm.go` `convertMessages`) | đỡ khi vào ngăn xếp + lọc khi ra; Host không cảm nhận |
| Phản hồi rỗng streaming / suy nghĩ dài | litellm (`StreamIdleTimeout=5min`) | watchdog kích hoạt thử lại |

### 5.4 Idempotent

Mỗi tool loại ghi trước khi thực thi đều kiểm tra checkpoint: nếu `Step+Digest` của checkpoint mới nhất ở scope hiện tại trùng với lần này thì trả về thẳng sản phẩm đã có. LLM có thể yên tâm thử lại, sẽ không sinh chương trùng hay tiến độ lệch.

---

## 6. Lắp ráp Agent

> Một Prompt siêu lớn duy nhất + một Agent duy nhất chạy hết cả cuốn về lý thuyết là khả thi, nhưng ba thứ sẽ chặn tính ổn định: **bùng nổ context** (200 chương dù nén mạnh đến đâu cũng suy biến), **nhiễu chức trách** (quy hoạch nghiêm cẩn / viết lách giàu trí tưởng / rà soát phê phán cùng một prompt làm loãng lẫn nhau), **mất lợi tức mô hình dị cấu** (quy hoạch dùng Opus, viết dùng Sonnet, rà soát dùng Pro, chọn mô hình độc lập là không gian tối ưu chi phí/chất lượng đáng kể với truyện dài). Tô-pô đa agent do đó là cần thiết.

### 6.1 Coordinator

Người duy nhất dẫn dắt vòng lặp chính. Lắp ráp tại `internal/agents/build.go`:

```go
agent := agentcore.NewAgent(
    agentcore.WithModel(coordinatorModel),
    agentcore.WithSystemPrompt(bundle.Prompts.Coordinator),
    agentcore.WithTools(subagentTool, contextTool),
    agentcore.WithMaxTurns(100_000),
    agentcore.WithToolsAreIdempotent(true),
    agentcore.WithMaxToolErrors(0),  // subagent không ngắt mạch
    agentcore.WithMaxRetries(subagentMaxRetries),
    agentcore.WithContextManager(...),
    agentcore.WithStopGuard(reminder.NewStopGuard(store, nil)),
    agentcore.WithToolGate(completePhaseGate(store)),  // phase=complete chặn cứng việc phái subagent
)
```

Chức trách: lúc khởi động chọn nhà quy hoạch → vòng lặp bổ sung quy hoạch → nhận `[Host hạ chỉ thị]` thì lập tức sinh `subagent` tool_call tương ứng → xử lý `[Người dùng can thiệp]` tự chủ phán định → sau `book_complete=true` thì xuất bản tổng kết.

Không làm: ghi tệp, đọc thẳng Progress (dùng novel_context), tự quyết bước tiếp theo khi chỉ thị Host tới.

> **Tại sao không bỏ Coordinator để Host gọi thẳng subagent?** Nhìn thì "sạch" hơn, nhưng sẽ mất bốn thứ: (1) quyết định "làm gì tiếp theo" giữ lại ở tầng LLM, mô hình nâng cấp là hưởng lợi ngay; (2) phán đoán mềm của verdict rà soát (accept/polish/rewrite + phạm vi ảnh hưởng) chuyển ra khỏi mã Go; (3) đánh giá ảnh hưởng của Steer người dùng giao cho mô hình — một câu "động cơ vai phụ phải rõ hơn" thì nên viết lại mấy chương nào, Coordinator phán được, Host hardcode thì không; (4) các nhánh ngoại lệ (writer phản hồi dàn ý, editor phát hiện lỗ hổng thế giới quan) do mô hình tự xử lý, tránh phải viết máy trạng thái Go cho từng nhánh. **Bỏ Coordinator tức là đổi cược từ "mô hình ngày càng mạnh" sang "mã Go của tôi ngày càng mạnh" — đây không phải vụ cược tốt**.

### 6.2 Tô-pô subagent và mô hình dị cấu

```
Coordinator (1 agent run, MaxTurns=100_000)
    ↓ subagent()
architect_short/long  ·  writer  ·  editor
    ↓ gọi tool
Store (môi trường cộng tác, các subagent không liên lạc trực tiếp)
```

Bộ đếm turn của subagent độc lập (agentcore nguyên thủy), không chiếm hạn mức 100_000 turn của Coordinator. Các subagent liên lạc qua artifact có cấu trúc trong Store, Coordinator chỉ truyền "mô tả nhiệm vụ" chứ không khuân nội dung.

`bootstrap.ModelSet` hỗ trợ mô hình cấp vai: coordinator/architect/writer/editor mỗi cái cấu hình độc lập + provider failover. Writer chạy Sonnet thay vì Opus có thể tiết kiệm một bậc độ lớn chi phí trên truyện dài 200 chương.

### 6.3 Ba mô thức cộng tác

Các subagent không liên lạc trực tiếp, mọi luồng thông tin chảy qua artifact có cấu trúc trong Store. Ba mô thức phủ toàn bộ luồng làm việc của hệ thống:

**Mô thức A · Bàn giao tuần tự (trục chính)**: Coordinator → Architect quy hoạch → Writer chương 1..N → Editor rà soát cuối cung → Writer viết lại. Mô thức thường gặp nhất, Coordinator tra trạng thái hiện tại qua `novel_context` để phán đoán bước tiếp theo gọi ai.

**Mô thức B · Phản hồi rà soát (vòng kín)**: Writer phát hiện lệch dàn ý trong draft → giá trị trả về của `commit_chapter` mang theo trường `writer_feedback` → Coordinator thấy phản hồi phán đoán có nên leo thang thành gọi architect để chỉnh dàn ý. Writer không gọi thẳng Architect, phản hồi gửi về Coordinator qua trường có cấu trúc.

**Mô thức C · Khai triển khung (quy hoạch cuốn chiếu)**: `commit_chapter` phát hiện cung tiếp theo vẫn là khung → trả về `arc_end_reached + next_skeleton_arc` → Flow Router phái chỉ thị → Coordinator gọi architect_long khai triển chương chi tiết của cung tiếp theo → Writer viết tiếp. Năng lực "quy hoạch cuốn chiếu" của truyện dài chính là vòng kín này hiện thực hóa.

### 6.4 Ràng buộc bằng mã cho luồng subagent (không dựa nạng prompt)

> Luồng writer thời kỳ đầu dựa vào ràng buộc "tiến hành nghiêm theo trình tự sau" trong `writer.md`. LLM thường vi phạm — bỏ qua plan draft thẳng, sau commit nói thêm một đoạn ngốn token, viết chính văn chỉ vào chat mà không ghi đĩa. **Ràng buộc quy trình bằng prompt không ổn định**, mạnh yếu hoàn toàn phụ thuộc "độ nghe lời" của mô hình lúc đó, mô hình nâng cấp lại có thể khiến nó "sáng tạo không tuân thủ".

Bốn tầng ràng buộc bằng mã (đồng thời có hiệu lực):

| Tầng | Điểm đặt | Tác dụng |
|---|---|---|
| `StopAfterTools` / `StopAfterToolResult` | `agents/build.go` SubAgentConfig | Tool then chốt thành công thì end_turn thoát subagent run. Writer trúng `commit_chapter` là dừng (`StopAfterTools`); `save_arc_summary`/`save_volume_summary` của Editor, thu cung/quyển của Architect đi `StopAfterToolResult`. `save_review` của Editor không dừng cứng — nếu không sẽ vượt qua StopGuard chặt đứt run tóm tắt cung, phần thu giao cho `NewEditorStopGuard` |
| `CheckpointDeltaGuard` | `host/reminder/subagent_guards.go` | Lấy baseline checkpoint làm mốc, trước khi kết thúc lượt này phải thấy checkpoint mới của step tương ứng, nếu không thì từ chối `end_turn`; chặn liên tiếp 3 lần thì leo thang terminate (đỡ vòng lặp vô hạn của mô hình yếu) |
| `next_step` nội tuyến trong tool | trường giá trị trả về của từng tool | Mỗi sự kiện tự mang "gợi ý bước tiếp theo". Như `plan_chapter` trả về `next_step: "lập tức gọi draft_chapter..."`. LLM thấy sự kiện là biết bước tiếp theo, không cần quay lại system prompt tìm |
| Kiểm tra quyền sở hữu/tiền đề trong tool | `edit_chapter` `commit_chapter`… | Chặn vật lý ở tầng dữ liệu: `edit_chapter` từ chối sửa chương đã hoàn thành không có trong `PendingRewrites`; `commit_chapter` từ chối commit rỗng khi draft==bản cuối; `ConcurrencySafe=false` chặn tranh chấp đồng thời |

writer.md trong kiến trúc mới chỉ đảm nhiệm: hướng dẫn chất lượng viết, mô hình nhận thức chạy tiếp từ điểm dừng, đọc hiểu hợp đồng chương. **Không còn làm điều phối quy trình** — LLM nhảy bước thì prompt sẽ không cứu, mã sẽ cứu. architect / editor có cùng bốn tầng ràng buộc trong tool/Guard của mình.

> Về luật thép một: `next_step` là trần thuật sự kiện nội tuyến trong tool ("tôi vừa lưu plan"), không phải điều phối quy trình do Host tiêm vào liên cuộc gọi. Điều phối liên subagent ở tầng Coordinator vẫn nghiêm ngặt đi Flow Router → FollowUp.

### 6.5 Phụ thuộc agentcore

`../agentcore` là thư viện Agent dùng chung của riêng dự án này (liên kết qua go.work). Các nguyên thủy mà kiến trúc mới dùng đều đã tồn tại: `Prompt` / `Inject` / `FollowUp` / `Subscribe` / `WithMaxTurns` / `WithStopGuard` / `WithToolGate` / `SubAgentConfig` / `WithContextManager`.

**Ranh giới sửa đổi**:

- Được vào agentcore: chiến lược ContextManager mới, adapter provider mới, loại sự kiện mới, mô thức tiêm thông điệp dùng chung
- Không vào agentcore: mô hình nghiệp vụ như Progress/Checkpoint/Scope, tool nghiệp vụ như novel_context/commit_chapter, rule nghiệp vụ như phát hiện kết thúc cung/môn chốt rà soát

Tiêu chí phán đoán: giả định agentcore tương lai sẽ được coding agent / agent chăm sóc khách hàng đưa vào, năng lực mới thêm vẫn có ý nghĩa trong bối cảnh đó thì mới cho vào. **Cấm viết bản vá đỡ ở tầng ứng dụng** (proxy, wrapper, monkey patch) — thiếu năng lực thì vào thẳng agentcore mà sửa.

**Năng lực cố ý không dùng** (tránh dùng nhầm):

- `Agent.TaskRuntime() / Tasks() / StopTask()` — bộ quản lý tác vụ nền có sẵn của agentcore (fire-and-forget background subagent). Mọi cuộc gọi subagent của kiến trúc mới đều là tiền cảnh đồng bộ, **không dùng**
- `Agent.FollowUp(msg)` — **người dùng hợp lệ duy nhất là `flow.Dispatcher`**, dùng để hạ `[Host hạ chỉ thị]`. Các phương thức công khai khác của Host cấm gọi. Steer người dùng đi `Inject` (giữ năng lực ngắt tức thì), Resume đi `Prompt` khởi run mới
- `Agent.Steer(msg)` — giao diện steering cũ, kiến trúc mới nhất loạt dùng `Inject`
- `WithPermission*` — cơ chế phê duyệt quyền (người duyệt thao tác nguy hiểm), ứng dụng tiểu thuyết không có thao tác nguy hiểm, **không dùng**

**Policy hook đã bật**: `WithToolGate` — công dụng duy nhất là khi `phase=complete` thì chặn cứng việc phái `subagent` (`agents/build.go` `completePhaseGate`). Sau khi hoàn thành nếu người dùng yêu cầu viết tiếp/viết lại, Coordinator LLM vẫn có thể tự phái subagent, mà Writer viết chương vượt biên sẽ bị `commit_chapter` từ chối, `CheckpointDeltaGuard` lại không cho `end_turn` → vòng lặp vô hạn. Flow Router lúc complete trả về nil chỉ chặn được việc Host tự phái, không chặn được LLM chủ động phái, nên để Gate bổ một lớp phòng vệ trạng thái cuối tại yết hầu. Nó là cú đỡ quy trình mục đích hẹp, **không phải luồng phê duyệt kiểu `WithPermission*`**, đừng lẫn lộn hai cái.

---

## 7. Tầng Host

### 7.1 Cấu trúc

```go
type Host struct {
    cfg               bootstrap.Config
    bundle            assets.Bundle
    store             *store.Store
    models            *bootstrap.ModelSet
    coordinator       *agentcore.Agent
    coordinatorCtxMgr *corecontext.ContextEngine  // liên động context window khi đổi mô hình
    askUser           *tools.AskUserTool
    writerRestore     *ctxpack.WriterRestorePack

    observer     *observer
    router       *flow.Dispatcher  // đăng ký + Route + FollowUp
    routerDetach func()
    usage        *UsageTracker
    usageCancel  context.CancelFunc
    budget       *BudgetSentinel   // thành phần policy Host: thực thi tuyên bố ngân sách người dùng (tương đương Abort thay người dùng), đăng ký trước Dispatcher
    notifier     *notify.Notifier  // tầng quan sát: bản sao ngoài màn hình của ba loại cảnh báo run_end/repeat/budget, không bao giờ can dự luồng điều khiển

    events, streamCh, done chan ...

    mu        sync.Mutex
    lifecycle lifecycle  // idle / running / paused / completed
    closeOnce sync.Once
}
```

### 7.2 API công khai

**Vòng đời** (lối vào Run của Coordinator): `Start` / `StartPrepared` / `Resume` / `Continue` / `Steer` / `Abort` / `Close`

**Kênh quan sát**: `Events` / `Stream` / `Done` (làm sạch luồng đi qua sentinel trong streamCh)

**Tổng hợp UI**: `Snapshot()` — TUI kéo một lần mọi dữ liệu cần hiển thị

**Cấu hình/mở rộng**: quản lý mô hình (`SwitchModel`), nhập tiểu thuyết ngoài bằng phương pháp suy ngược (`ImportFrom`), đối thoại đồng sáng tác (`CoCreateStream`), phát lại sự kiện (`ReplayQueue`), hồ sơ mô phỏng (`Simulate`/`ImportSimulationProfile`), xuất (`Export`)

Không có các phương thức điều phối nghiệp vụ như `decideNext` `retryActiveTask`. Flow Router là tổ hợp mỏng của hàm thuần + FollowUp, không giữ trạng thái ngầm kiểu "tác vụ đang thử lại".

### 7.3 Hình thái `waitDone`

```go
func (h *Host) waitDone() {
    h.coordinator.WaitForIdle()
    h.observer.finalize()

    if Phase == Complete { lifecycle=completed; phát sự kiện "Sáng tác hoàn thành" }
    else if running        { lifecycle=idle;     phát sự kiện "Coordinator dừng (đã hoàn thành N chương)" }

    select { case h.done <- struct{}{}: default: }
}
```

Ba việc: chờ idle → chuyển lifecycle → phát sự kiện trạng thái cuối + gửi tín hiệu done. **Cấm `Inject` / `FollowUp` / `Prompt` xuất hiện trong thân hàm**. Sau khi LLM chạy xong một lần Run thì cả Host vào trạng thái cuối.

Muốn động lại chỉ có hai cách: người dùng chủ động `Continue`/`Start`, hoặc khởi động lại tiến trình đi `Resume`.

> Bài học lịch sử: từng thêm bản vá `idleResumeCount` tự khởi động lại Run vào hàm này. Trong lần thực tế duy nhất kích hoạt ở cú chạy dài mimo, nó 100% không cứu được, ngược lại che mất nguyên nhân thật ở tầng agentcore "thông điệp thinking-only stop vào lịch sử". **"Khởi động lại phòng vệ" ở tầng Host vĩnh viễn là sửa sai chỗ**. Chi tiết xem `feedback_no_host_resilience.md` và §10 mục 5.

---

## 8. Khởi động và khôi phục

### 8.1 Tạo mới

```
User: "nhu cầu một câu"
  → Host.Start
    → store.Progress.Init / store.Checkpoints.Reset
    → coordinator.Prompt(userPrompt) + flow.Dispatcher.Enable + Dispatch
    → Coordinator long loop: quy hoạch → viết 1..N → rà soát → done
```

### 8.2 Khôi phục (khởi động lại sau khi sập)

```
Tiến trình khởi động
  → đọc Progress + Checkpoint gần nhất + PendingCommit + PendingSteer
  → buildResumePrompt → thông báo ngắn (không phải chỉ thị cấp step)
  → coordinator.Prompt(resumePrompt) + Dispatcher.Enable + Dispatch
  → Coordinator tiếp tục theo chỉ thị Host
```

Resume dùng `Prompt` khởi Run mới (đặt lại bộ đếm turn, context sạch), không phải `FollowUp`. Bước tiếp theo cụ thể do Flow Router phái sinh từ tầng sự kiện sau TurnEnd đầu tiên.

### 8.3 Người dùng can thiệp

| Lối vào | Tiền tố | Ngữ nghĩa | Hiện thực |
|---|---|---|---|
| `Steer(text)` | `[Người dùng can thiệp]` | Sửa/truy vấn, cần Coordinator phán định | Đang chạy đi `Inject`; dừng máy ghi PendingSteer vào `meta/run.json` |
| `Continue(text)` | `[Người dùng can thiệp]` | Viết tiếp, đánh thức sau khi dừng máy | Đang chạy đi `FollowUp`; dừng máy đi `Inject` tự khôi phục run |

Hai lối vào thống nhất qua helper `interventionMsg` thêm tiền tố `[Người dùng can thiệp]` — đó là điểm neo phân loại can thiệp trong `coordinator.md`; trước đây Continue gửi văn bản trần sẽ vượt qua phân loại, bị phái nhầm cho writer sửa chương đã viết (đã sửa).

Ngữ nghĩa `Inject`: đang chạy thì chen hàng vào hàng đợi run hiện tại; rảnh thì tự khôi phục run rồi tiêm; tạm dừng thì xếp hàng chờ khôi phục.

**Tầng bền của can thiệp dài hạn**: trong phân loại can thiệp, "yêu cầu dài hạn chỉ ảnh hưởng việc viết về sau" (loại phong cách/khuynh hướng) do Coordinator gọi `save_directive` ghi đĩa vào `meta/user_directives.json` (tối đa 20 mục, add khử trùng / remove theo số thứ tự), `novel_context` tiêm vào `working_memory.user_directives` — mọi subagent mỗi chương tự động thấy, xuyên qua nén, xuyên qua khởi động lại đều có hiệu lực, không phụ thuộc trí nhớ hội thoại của Coordinator và việc chuyển phái. Ba loại can thiệp còn lại vốn đã rơi vào store (độ dài→compass/outline, thiết lập→foundation, sửa chương cũ→PendingRewrites). Đi qua phong bì chứ không qua system prompt: bảo vệ cache tiền tố system xuyên chương của writer.

Mỗi chỉ thị khi ghi đĩa đính kèm **ảnh chụp tiến độ lúc hạ lệnh** (at_chapter / at_total_chapters): chỉ thị có hiệu lực từ at_chapter trở về sau (editor không truy ngược chương cũ); lỡ khi chỉ thị dạng tương đối ("tăng 10 chương") bị lưu nhầm thành yêu cầu dài hạn, bên đọc có thể dựa vào ảnh chụp để phán định đã thỏa mãn thay vì thực thi lặp. Đường chính của chỉ thị dạng hành động vẫn là dịch-lúc-ghi của route tương ứng (architect/editor → trạng thái tuyệt đối của outline/compass/PendingRewrites), ảnh chụp là bảo hiểm khi phân loại nhầm.

---

## 9. Cấu trúc thư mục

```
internal/
  domain/         dữ liệu thuần: Phase / FlowState / Progress / Checkpoint / Scope / Story / Plan /
                  Review / StateChange / quy tắc chuyển Phase-Flow
  store/          bền hóa hệ thống tệp (tmp+rename + bộ ba): progress / checkpoints / outline /
                  drafts / summaries / characters / world / signals / run_meta / runtime / session
  tools/          11 tool Agent, loại ghi toàn bộ bộ ba nguyên tử + idempotent theo digest + ConcurrencySafe=false
                  + premise_structure (save_foundation dùng nội bộ) + ask_user
  agents/         build.go lắp ráp Coordinator + ba subagent; ctxpack/ chiến lược nén context của Writer
  host/           host.go + resume.go + observer.go + events.go + usage.go + usage_replay.go
                  + stream_extract.go + cocreate.go
    flow/         router.go (hàm thuần 11 nhánh) + state.go + dispatcher.go + router_test.go
    reminder/     stop_guard.go (Coordinator) + subagent_guards.go (CheckpointDeltaGuard ×3)
    imp/          nhập tiểu thuyết ngoài bằng suy ngược: split → foundation → phân tích từng chương
    exp/          xuất chương đã hoàn thành: gộp chương → TXT / EPUB 3, hậu tố đường dẫn dẫn dắt; thuần chỉ-đọc, không phụ thuộc LLM
  entry/          tui (Bubble Tea) / headless / startup
  bootstrap/      config + ModelSet + provider failover + trình hướng dẫn setup
  models/         registry mô hình công cộng như OpenRouter + làm mới giá (cache đĩa 24h)
  errs/           phân tầng lỗi
  diag/           module chẩn đoán chỉ-đọc đăng ký luồng sự kiện host
  utils/          di sản kiến trúc cũ (ít tool phân tích, mã mới không nên phụ thuộc)

assets/
  prompts/        coordinator (~55 dòng) / architect-short|long / writer / editor / import-* / simulation-*
  references/     kỹ thuật viết + mẫu thể loại + quy hoạch truyện dài v.v.
  styles/         mặc định/kỳ ảo/ngôn tình/trinh thám

../agentcore     framework Agent dùng chung (thư mục anh em go.work, được thêm năng lực chung, không thêm nghiệp vụ)
../litellm       gateway LLM
```

### 9.1 Cột mốc tiến hóa

| Thời gian | Tái cấu trúc | Hiệu quả ròng |
|---|---|---|
| 2026-04-10 | `internal/orchestrator/` (6342 dòng) → `host/` + `agents/` | Lõi runtime -74% |
| 2026-04-20 | Hybrid Coordinator: tạo mới `host/flow/`, `reminder/` giảm gầy, `coordinator.md` 88 dòng → 45 dòng | Tỉ lệ lỗi định tuyến tiệm cận 0 |
| 2026-05-02 | agentcore `WithMaxToolErrors(0)` + `isReasoningOnlyStopAssistant`; `StreamIdleTimeout=5min`; xóa bản vá chạy tiếp `idleResumeCount` | mimo / streaming suy nghĩ chậm chạy thông |
| 2026-06-05 | Vòng kín quy hoạch cuốn chiếu (`expand_arc`/`append_volume`) + `/import` suy ngược phân tầng viết tiếp + can thiệp độ dài người dùng | Lần đầu chạy thông 200+ chương |

Thực đo: hy3-preview free 12 chương / 73 phút, mimo-v2.5-pro 10 chương / 84 nghìn chữ (trung bình 8400 chữ/chương), đều chạy xong một lần; truyện dài gpt-5.4 "Phàm Cốt" 235 chương / 1,27 triệu chữ / trung bình 5407 chữ/chương, vòng kín quy hoạch cuốn chiếu chạy thông.

---

## 10. Những điều dứt khoát không làm

Vi phạm tức là kiến trúc đã lệch hướng.

1. **Không đưa vào khái niệm Task / Job / WorkItem**. "Tác vụ hiện tại" mà UI hiển thị là phép chiếu luồng sự kiện, không phải sự kiện.
2. **Không đưa vào Dispatcher / Scheduler / Ready Evaluator**. Quyền quyết định ở Coordinator LLM và tầng tool.
3. **Không làm cơ chế "chạy tiếp khi rảnh" kiểu `idle_dispatch`**. Coordinator Run kết thúc = Host phát done.
4. **Không vượt qua Coordinator ở Host để gọi thẳng SubAgent**. Flow Router qua `coordinator.FollowUp` để Coordinator sinh tool_call. Resume dùng `Prompt` khởi Run mới.
5. **Không thêm bản vá tự chạy tiếp ở Host cho việc LLM dừng máy bất thường**. Run kết thúc = Host vào trạng thái cuối. `idleResumeCount` từng có đã bị xóa (chi tiết xem §7.3, `feedback_no_host_resilience.md`).
6. **Không suy ra hoàn thành tác vụ dựa trên "tool exec end"**. Bằng chứng hoàn thành duy nhất là checkpoint được ghi.
7. **Không làm mô hình bốn tầng kiểu WorkflowInstance / TaskInstance / Command + Apply**. Tầng sự kiện chỉ có ba loại Progress + Checkpoint + Artifact.
8. **Không hỗ trợ task song song**. Một Coordinator Run hoạt động duy nhất, một cuốn sách tuần tự đẩy tới. Nhiều tiểu thuyết xin dùng đa tiến trình.
9. **Không gọi LLM ở tầng tool** (trừ chính tool Agent). Thuần IO + kiểm tra + idempotent.
10. **Không để UI đọc thẳng Store**. Chỉ được đăng ký sự kiện hoặc đọc `Snapshot()` của Host.
11. **Không dùng tệp tín hiệu làm IPC**. Host đọc thẳng Progress + Checkpoint + dàn ý phân tầng, `flow.Route` phái sinh chỉ thị từ sự kiện là định tuyến chuyên ngành dọc hợp lý.
12. **Không viết máy trạng thái Flow ở phía Host**. Nhãn Flow chỉ do tool cập nhật, Router chỉ đọc không ghi.
13. **Không viết hardcode đỡ cho "LLM ảo giác"**. Tối ưu prompt, cải thiện cấu trúc giá trị trả về của tool, để `novel_context` trình bày sự kiện rõ hơn — chứ không phải Host ép đổi quy trình.
14. **Không để diag / tầng quan sát can dự luồng điều khiển**. Chẩn đoán chỉ đọc, chỉ sinh Finding và xuất khử nhạy cảm; tự sửa / chạy tiếp / đổi quy trình nhất loạt không làm (xem §2.3 kỷ luật người quan sát).
15. **Ngân sách và cảnh báo không vào tầng Route/tool, cảnh báo không vào luồng điều khiển**. `BudgetSentinel` là thành phần policy Host (thực thi Abort người dùng đã ký trước, không đánh giá hành vi mô hình); `notify` là thuần quan sát (không thử lại, không đổi phái, không dừng máy). `flow.Route` giữ là hàm thuần, vô cảm với cả hai.

---

## 11. Chiến lược kiểm chứng

### 11.1 Kịch bản ổn định

- **A Chạy dài**: 80~200 chương chạy xong một lần, Phase=complete. Cho phép provider failover, tools thử lại transient; cấm Host chạy tiếp hay Coordinator Run nhiều lần.
- **B Khôi phục khi sập**: sau draft chương N / trước commit thì kill tiến trình → Resume → tiếp tục từ consistency_check, không viết lại draft đã ghi đĩa. `checkpoints.jsonl` không có step trùng.
- **C provider rung lắc**: mô phỏng 503 gián đoạn → litellm failover; vòng lặp chính LLM vô cảm.
- **D Người dùng can thiệp**: Steer lúc runtime → Coordinator xử lý ở turn tiếp; Steer sau khi dừng máy → prompt Resume lần sau bao gồm.

### 11.2 Tính tuân thủ (có thể viết thành linter / test)

- `internal/host/` không cho phép `import "internal/scheduler"` và các gói điều phối tương tự
- Số lượng API vòng đời của `host.go` ổn định; phương thức công khai mới thêm chỉ được là loại "lối vào mở rộng" (đồng sáng tác/nhập/quản lý mô hình)
- Trong thân hàm `waitDone` không cho phép `coordinator.Inject` / `FollowUp` / `Prompt`
- Mã liên quan `recovery` chỉ được xuất hiện trong `host/resume.go`
- `flow.Route` bắt buộc là hàm thuần: cấm đọc Store / mọi IO

### 11.3 Lặp chất lượng

Sửa `writer.md` lập tức tạo thay đổi phong cách; chiều rà soát editor mới thêm tương thích ngược (save_review nhận JSON có cấu trúc). Thêm một md tài liệu tham khảo mới cần đấu nối ba chỗ (trường `tools.References` + `loadReferences` trong `assets/load.go` + tiêm `writerReferences`/`architectReferences` trong `novel_context.go`), chứ không phải bỏ vào thư mục là tự nạp — `References` là ánh xạ trường tường minh, tiện cắt tỉa theo vai/chương.

**Thống kê phong cách cấp toàn sách (`internal/stylestat`)**: cửa sổ rà soát trong cung tự nhiên mù với loại cố hóa cấp toàn sách như "tic câu vài chục lần/chương, hình thái cuối chương đồng cấu, đọc lại nguyên văn xuyên chương" — nhìn từng chương thì mỗi chỗ đều bình thường. Đường dẫn chương của `novel_context` chạy thống kê tất định trên toàn bộ chương đã hoàn thành (loại mô thức câu/cụm ngắn tần cao cận cửa sổ/câu lặp xuyên chương/hình thái cuối chương/dùng lẫn định dạng tiêu đề), tiêm vào `episodic_memory.style_stats`: editor phán định theo con số ở chiều aesthetic, writer dựa đó tự tránh. **Thống kê thuộc về mã, phán định thuộc về LLM** — ngưỡng không hardcode trong mã, con số có thành bệnh hay không do mô hình phán theo thể loại. Sánh ngang với nó là giới hạn đáy sản phẩm `rules.Lint` (markdown sót lại/đoạn không phải tiếng Trung) luôn được thực thi ở commit_chapter, chỉ trả về sự kiện.

---

## 12. Tổng kết

> **Để LLM viết xong cả một cuốn tiểu thuyết trong một lần Run, Host chỉ làm khởi động / khôi phục / định tuyến / quan sát, bản ghi sự kiện do tool ghi đĩa nguyên tử, quyền quyết định để dành tối đa cho mô hình.**

Không có workflow engine, không có task queue, không có dispatcher, không có scheduler. Cái có chỉ là:

- Một Coordinator 100_000 turn
- Ba subagent chức năng (context và mô hình độc lập)
- 11 tool nguyên tử
- Một tệp checkpoint jsonl
- Vỏ Host ~860 dòng
- Hàm thuần Flow Router ~150 dòng (11 nhánh + unit test)

Mỗi dòng mã nghiệp vụ Host đều là vụ cược đối ứng với việc nâng cấp mô hình. **Host nhỏ nhất, Prompt béo nhất (tầng chất lượng), tool mạnh nhất** khiến kiến trúc tự động tốt lên mỗi năm — Coordinator quyết chính xác hơn, Writer viết hay hơn, Editor rà soát chuẩn hơn, Architect quy hoạch tinh hơn, tất cả là lợi tức vô cảm với kiến trúc khi đổi mô hình.

Ngược lại nếu hardcode trong Host các rule kiểu "lần review trước nói viết lại chương 3, 5" hay "liên tiếp 3 lần không tiến triển thì dừng máy", mô hình nâng cấp sẽ khiến nó thành **lợi tức âm**: phán đoán đáng lẽ LLM làm trở nên dư thừa, logic bảo vệ thành báo động giả. **Tệ nhất là không ai dám xóa — xóa tức là "tin mô hình", gánh nặng tâm lý còn khó dọn hơn cả mã**. Loại mã này để lại càng nhiều, chi phí tái cấu trúc tương lai càng cao.

**Khả năng mở rộng đến từ điểm mở rộng đúng**: đổi phong cách → đổi prompt; chiều rà soát mới → đổi prompt; thể loại mới → thêm tài liệu tham khảo; loại subagent mới → thêm một dòng SubAgentConfig; nhiều tiểu thuyết song song → đa tiến trình.

Kỷ luật duy nhất: **khi có người muốn "làm Host thông minh hơn chút", trước hết hãy hỏi "tại sao không làm LLM thông minh hơn chút"**. Câu hỏi này mà không trả lời được lý do "Host buộc phải", thì đừng thêm mã vào Host.
