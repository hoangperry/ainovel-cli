> [English](refactor-flow-driven.en.md)

# Đề xuất tái cấu trúc: Hybrid Coordinator — Host định tuyến × LLM phân xử

> Trạng thái: **Đã chấp nhận và triển khai** (2026-04-20)
> Thời điểm khảo sát: 2026-04-20
> Tài liệu hiện hành tương ứng: `docs/architecture.md` §2 / §3 / §7 / §8 / §13 đã được cập nhật đồng bộ
>
> **Đây là bản thảo thứ hai.** Các vấn đề của bản thảo đầu (phương án quyết liệt: xóa hoàn toàn Coordinator) được nêu chi tiết ở Phụ lục A; giữ lại phần đó để tránh đi lại đường vòng.
>
> Kết quả triển khai:
> - Tạo mới `internal/host/flow/` (router.go / state.go / dispatcher.go / router_test.go, 15 unit test theo nhánh đều pass)
> - `internal/host/reminder/` xóa `flow.go` / `queue_guard.go` / `book_complete.go`; giữ StopGuard và Guard của subagent
> - `assets/prompts/coordinator.md` nén từ 88 dòng xuống ~45 dòng (thu hẹp trách nhiệm còn: thực thi chỉ thị Host + phân xử + chọn lựa lúc khởi động)
> - `internal/host/resume.go` đơn giản hóa mạnh, chỉ tạo label và prompt ngắn; bước kế tiếp cụ thể do Router phát đi sau lần TurnEnd đầu tiên
> - `internal/store/` thêm các phương thức hỗ trợ `HasArcReview` / `HasArcSummary` / `HasVolumeSummary` / `CheckConsistency`
> - sửa luôn bug agent state bị kẹt ở `working` trong `observer.go`

---

## 1. Bối cảnh

### 1.1 Định vị dự án

```
agentcore       — framework agent tổng quát
litellm         — LLM gateway tổng quát
ainovel-cli     — agent dọc cho sáng tác tiểu thuyết (dự án này)
```

Không gian quyết định của agent dọc là **khép kín**: lưu đồ cố định, số nhánh hữu hạn, dựa trên sự kiện thực tế (fact-driven). Triết lý thiết kế của agent tổng quát ("đặt cược vào năng lực mô hình") đem áp vào kịch bản dọc thì có phần thuần khiết thái quá.

### 1.2 Mục tiêu người dùng (theo ưu tiên)

1. **Tính ổn định** — viết liên tục không ngừng, không bị gián đoạn vì định tuyến sai
2. **Hưởng lợi từ nâng cấp LLM** — kiến trúc không đối kháng năng lực mô hình
3. **Tận dụng tối đa năng lực multi-agent** — phân công chức năng rõ ràng

Đề xuất này tạo ra một **cải tiến Pareto** trên cả ba (không hy sinh bất kỳ mục tiêu nào để đổi lấy mục tiêu khác).

---

## 2. Khảo sát hiện trạng

### 2.1 Phân loại các điểm quyết định của Coordinator

Trích từng điểm quyết định trong `coordinator.md`:

| # | Điểm quyết định | Bản chất | Tần suất |
|---|---|---|---|
| 1 | Chọn architect_long / short lúc khởi động | Phân xử (hiểu ngữ nghĩa) | 1 lần / cuốn |
| 2 | Mở rộng đầu vào (tự bổ sung khi <20 ký tự) | Phân xử (mang tính sáng tác) | 0-1 lần / cuốn |
| 3 | Vòng lặp bổ sung kế hoạch | Định tuyến (fact-driven) | 1-3 lần |
| 4 | Bước kế tiếp sau mỗi commit chương | **Định tuyến** | **1-2 lần / chương** |
| 5 | Thực thi theo bước phần rà soát cuối cung truyện | Định tuyến | 3-5 lần / cung |
| 6 | Rẽ nhánh theo verdict rà soát | Định tuyến (đã code hóa, xem §2.3) | 1 lần / cung |
| 7 | Xử lý can thiệp người dùng | Phân xử (bắt buộc LLM) | bất kỳ |
| 8 | Phát lại khi subagent báo lỗi | Định tuyến | thi thoảng |
| 9 | Xuất tổng kết khi hoàn thành cả cuốn | Định tuyến | 1 lần |

**Kết luận**: trong 9 điểm quyết định, 6 điểm là định tuyến thuần (tra bảng), 3 điểm thực sự cần LLM phân xử. **Định tuyến xảy ra với tần suất cao hơn phân xử rất nhiều** (1-2 lần/chương so với vài lần/cuốn).

### 2.2 Kênh Reminder vốn đã là bán thành phẩm của việc code hóa quy trình

Các generator dưới `internal/host/reminder/` mỗi lượt sinh ra **chỉ thị cụ thể tới từng hành động** dựa trên sự kiện thực tế:

- `flow.go` → `"flow hiện tại=writing, next_chapter=37. Hãy gọi trực tiếp subagent(writer, \"viết chương 37\")..."`
- `queue_guard.go` → `"flow hiện tại=rewriting, hàng đợi chờ xử lý: [3,5]. Hãy gọi writer ngay để viết lại từng chương..."`
- `book_complete.go` → `"cả cuốn đã xong. Hãy xuất tổng kết toàn cuốn..."`

**Kiến trúc hiện tại tồn tại double dispatch**:
```
Tầng quy tắc: coordinator.md định nghĩa "nếu A thì B"
  ↓
Tầng Reminder: mỗi lượt cụ thể hóa quy tắc từ sự kiện → sinh ra "bây giờ hãy làm B"
  ↓
Tầng LLM: đọc reminder sinh tool_call (về cơ bản là nhắc lại reminder)
  ↓
SubAgent thực thi
```

**LLM thực ra chỉ đang "thực thi" chỉ thị mà Reminder đưa cho nó**. Mắt xích trung gian này vừa tốn token, vừa đưa thêm tính bất định (LLM có thể không tuân thủ hoàn toàn reminder, ví dụ lỗi định tuyến mid đã quan sát được).

### 2.3 Tầng tool đã gánh phần lớn phán đoán

- `save_review.evaluateScorecardGate()`: gate theo phiếu chấm điểm, tự nâng accept lên polish/rewrite
- Kiểm tra `save_review.ContractStatus`: contract=missed tự nâng lên rewrite
- `commit_chapter.CheckArcBoundary()`: tính tức thì `arc_end / needs_expansion / needs_new_volume`
- `commit_chapter.applyCompletion()`: phán định tức thì `book_complete`
- `CommitResult` trả về 17 trường sự kiện

**Kết luận**: tầng tool đã code hóa phần lớn "phán đoán"; quyết định mà Coordinator đưa ra từ các sự kiện này về cơ bản chỉ là if-else.

### 2.4 Chi phí thực tế của hiện trạng

Số lượt (turn) LLM của Coordinator mỗi chương:
- **1-2 turn / chương** (đọc system prompt ~3000 token + reminder ~200 token + lịch sử + CommitResult ~500 token → sinh tool_call ~50 token)
- Tiểu thuyết dài 200 chương: khoảng **200-400 turn** gọi LLM Coordinator
- Trong đó **~90% là định tuyến thuần** (LLM nhắc lại reminder), **~10% là phân xử**

**Mỗi chương tiêu ~3500-7000 token cho quyết định của Coordinator, 95% là dư thừa** (Reminder đã tính ra đáp án).

---

## 3. Phương án thiết kế: Hybrid Coordinator

### 3.1 Ý tưởng cốt lõi

**Chuyển quyết định quy trình từ LLM sang Host, nhưng giữ Coordinator làm node phân xử và kênh thực thi chỉ thị**.

```
┌──────────────────────────────────────────────────────────┐
│                   Entry (TUI / headless)                   │
└────────────────────────────────┬─────────────────────────┘
                                 │ Start / Resume / Steer
┌────────────────────────────────▼─────────────────────────┐
│                            Host                            │
│                                                             │
│   ┌──────────────────────────────────────────────────┐     │
│   │  Flow Router (lõi mới)                            │     │
│   │  ───────────                                      │     │
│   │  Đăng ký sự kiện Coordinator: kích hoạt khi tool  │     │
│   │  subagent trả về                                  │     │
│   │  Hàm thuần: route(Progress, Checkpoint,           │     │
│   │      Boundary) → NextInstruction                  │     │
│   │  Có chỉ thị → coordinator.FollowUp(chỉ thị)       │     │
│   │  Không có chỉ thị (kịch bản phân xử) → không can  │     │
│   │      thiệp, để LLM tự chủ                         │     │
│   └──────────────────────────────────────────────────┘     │
│                                                             │
│   Giữ: API vòng đời / Observer / Usage Tracker             │
│   Giữ: resume.go (đơn giản hóa, lõi logic không đổi)        │
└────────────────────────────────┬─────────────────────────┘
                                 │
┌────────────────────────────────▼─────────────────────────┐
│                    Coordinator Agent (LLM)                  │
│                                                             │
│   Trách nhiệm thu hẹp còn hai loại:                         │
│   1. Nhận chỉ thị Host FollowUp → sinh tool_call tương ứng  │
│   2. Tự chủ phân xử khi Steer của người dùng tới            │
│      (đánh giá truy vấn/chỉnh sửa)                          │
│                                                             │
│   coordinator.md: 88 dòng → ~25 dòng                        │
│   MaxTurns: giữ 1000 (đáp ứng user steer + thực thi chỉ      │
│      thị Host)                                              │
└────────────────────────────────┬─────────────────────────┘
                                 │
                                 ▼
         ┌──────────────────────┼───────────────────────┐
         ▼                      ▼                       ▼
    ┌────────┐             ┌────────┐             ┌────────┐
    │Architect│             │ Writer │             │ Editor │
    └────────┘             └────────┘             └────────┘
```

### 3.2 Phân chia lại trách nhiệm

| Tầng | Làm gì | Không làm gì |
|---|---|---|
| **Host / Flow Router** | đọc sự kiện → định tuyến hàm thuần → chỉ thị FollowUp | tự gọi SubAgent (vẫn qua Coordinator) |
| **Coordinator** | thực thi chỉ thị Host + phân xử can thiệp người dùng + chọn architect lúc khởi động | tự quyết "làm gì tiếp theo" |
| **SubAgent (A/W/E)** | công việc chuyên môn của mình | không đổi |
| **Tầng tool** | lưu chốt nguyên tử + trả sự kiện | không đổi |

**Bất biến then chốt**:
- ✅ Coordinator vẫn là một agent run liên tục, giữ "cảm nhận liên tục" toàn cuốn
- ✅ Steer của người dùng vẫn qua `coordinator.Inject()`, giữ khả năng ngắt tức thì
- ✅ SubAgentTool vẫn do LLM gọi (đi đường native của agentcore), luồng sự kiện / ContextManager / chuyển mô hình đều không đổi
- ✅ agentcore không phải sửa gì

### 3.3 Logic cụ thể của Flow Router

```go
// internal/host/flow/router.go

type NextInstruction struct {
    Agent  string   // architect_long / architect_short / writer / editor
    Task   string   // mô tả nhiệm vụ cho subagent
    Reason string   // lý do cho Coordinator xem (tùy chọn, tiện debug)
}

type RouterState struct {
    Progress        *domain.Progress
    LatestCheckpoint *domain.Checkpoint
    // biên cung truyện ở chế độ phân tầng (tính khi chương trước đã xong)
    LastCompleted   int
    ArcBoundary     *store.ArcBoundary
    HasArcReview    bool
    HasArcSummary   bool
    // các hạng mục thiếu của thiết lập nền
    FoundationMissing []string
}

// Route trả về chỉ thị bước kế tiếp. Trả nil nghĩa là để Coordinator tự phân xử (kịch bản phân xử).
func Route(s RouterState) *NextInstruction {
    p := s.Progress

    // 0. Trạng thái cuối: để LLM xuất tổng kết, không định tuyến
    if p.Phase == domain.PhaseComplete {
        return nil
    }

    // 1. Giai đoạn lập kế hoạch: phân xử (chọn architect) do LLM làm, không định tuyến
    if p.Phase != domain.PhaseWriting {
        return nil
    }

    // 2. Giai đoạn viết
    // 2a. Ưu tiên hàng đợi viết lại/đánh bóng
    if len(p.PendingRewrites) > 0 {
        ch := p.PendingRewrites[0]
        verb := "viết lại"
        if p.Flow == domain.FlowPolishing {
            verb = "đánh bóng"
        }
        return &NextInstruction{
            Agent:  "writer",
            Task:   fmt.Sprintf("%s chương %d", verb, ch),
            Reason: fmt.Sprintf("Hàng đợi PendingRewrites còn %d chương", len(p.PendingRewrites)),
        }
    }

    // 2b. Đang rà soát: không định tuyến, để Coordinator rẽ nhánh verdict theo kết quả save_review
    if p.Flow == domain.FlowReviewing {
        return nil
    }

    // 2c. Hậu xử lý cuối cung truyện ở chế độ phân tầng
    if p.Layered && s.ArcBoundary != nil && s.ArcBoundary.IsArcEnd {
        b := s.ArcBoundary
        if !s.HasArcReview {
            return &NextInstruction{
                Agent:  "editor",
                Task:   fmt.Sprintf("rà soát cấp cung truyện cho cung %d quyển %d", b.Arc, b.Volume),
                Reason: "rà soát cuối cung truyện chưa xong",
            }
        }
        if !s.HasArcSummary {
            return &NextInstruction{
                Agent:  "editor",
                Task:   fmt.Sprintf("tạo bản tóm tắt cung %d quyển %d", b.Arc, b.Volume),
                Reason: "tóm tắt cung truyện chưa xong",
            }
        }
        if b.NeedsExpansion {
            return &NextInstruction{
                Agent:  "architect_long",
                Task:   fmt.Sprintf("mở rộng cung %d quyển %d (save_foundation type=expand_arc)", b.NextArc, b.NextVolume),
                Reason: "khung xương cung truyện kế tiếp chờ mở rộng",
            }
        }
        if b.NeedsNewVolume {
            return &NextInstruction{
                Agent:  "architect_long",
                Task:   "đánh giá và thực thi save_foundation(type=append_volume) hoặc mark_final",
                Reason: "quyển kết thúc, cần quyết định thêm quyển mới",
            }
        }
    }

    // 2d. Viết tiếp bình thường
    next := p.NextChapter()
    return &NextInstruction{
        Agent:  "writer",
        Task:   fmt.Sprintf("viết chương %d", next),
        Reason: "viết tiếp",
    }
}
```

**Đặc tính của hàm**:
- Hàm thuần (đầu vào RouterState, đầu ra NextInstruction)
- Có thể unit test (cho một trạng thái, khẳng định kết quả định tuyến)
- **Trả nil là hợp lệ** — nghĩa là "đây là kịch bản phân xử, hãy để LLM tự chủ"

### 3.4 Thời điểm kích hoạt

Host đăng ký sự kiện `agentcore.EventToolExecEnd`:

```go
coordinator.Subscribe(func(ev agentcore.Event) {
    if ev.Type == agentcore.EventToolExecEnd && ev.Tool == "subagent" && !ev.IsError {
        // SubAgent vừa trả về → đọc trạng thái mới nhất → định tuyến
        h.flowRouter.Dispatch()
    }
})
```

```go
func (r *FlowRouter) Dispatch() {
    state := r.loadState()
    instruction := Route(state)
    if instruction == nil {
        return // kịch bản phân xử, để LLM tự chủ
    }
    msg := formatInstruction(instruction)
    _ = r.coordinator.FollowUp(agentcore.UserMsg(msg))
}

func formatInstruction(i *NextInstruction) string {
    return fmt.Sprintf(
        "[Host ra chỉ thị] Bước kế tiếp: gọi subagent(%s, %q)\n"+
        "Lý do: %s\n"+
        "Đây là chỉ thị rõ ràng của tầng quy trình, hãy thực thi ngay, đừng gọi novel_context trước, đừng xuất suy luận trước.",
        i.Agent, i.Task, i.Reason,
    )
}
```

### 3.5 Tính đáp ứng và đồng thời

**Đường đi của user Steer** (không đổi):
```
Steer → coordinator.Inject(UserMsg("[can thiệp người dùng] xxx"))
```

- Đang chạy: chèn message vào hàng đợi của run hiện tại
- Idle: resume run
- Paused: xếp hàng

**Đồng thời giữa chỉ thị định tuyến + Steer**:
- Cả hai vào hàng đợi message của Coordinator, xử lý theo thứ tự native của agentcore
- Nếu Host vừa gửi `FollowUp("[Host chỉ thị] viết chương 37")`, ngay sau đó người dùng Steer `"khoan đã, chỉnh lại văn phong"`
  - Coordinator xử lý chỉ thị Host trước? Hay xử lý Steer trước?
  - **Ngữ nghĩa của `Inject` là chèn lên đầu hàng đợi hiện tại**, nên Steer được xử lý trước
  - Đây là hành vi mong muốn: can thiệp người dùng có ưu tiên cao hơn lịch điều phối thường lệ của Host

**Tránh xung đột giữa chỉ thị Host và Steer**:
- Sau khi nhận tín hiệu "Steer đã được inject", Flow Router **tạm dừng ngắn** vài turn (để Coordinator xử lý xong Steer rồi mới định tuyến)
- Cảm nhận kết quả xử lý Steer qua việc đăng ký `agentcore.EventMessageEnd` + kiểm tra thay đổi trạng thái Progress

### 3.6 Ví dụ đơn giản hóa coordinator.md

Cắt từ 88 dòng xuống khoảng 25:

```markdown
Bạn là tổng điều phối viên sáng tác tiểu thuyết.

## Chế độ làm việc của bạn

**Trục chính**: Sau mỗi lần subagent trả về, Host sẽ ra một message `[Host ra chỉ thị]` cho bạn biết gọi subagent nào tiếp theo và làm gì. Nhận chỉ thị thì sinh ngay tool_call tương ứng, đừng gọi novel_context để suy luận trước, đừng nhắc lại.

**Phân xử**: Gặp các tình huống sau bạn cần tự phán đoán (Host sẽ không ra chỉ thị, bạn phải chủ động hành động):

### Lúc khởi động: chọn architect

- Mặc định → `architect_long`
- Chỉ khi người dùng yêu cầu rõ truyện ngắn/đơn quyển/tiểu phẩm và độ dài giới hạn trong 25 chương → `architect_short`

Nếu đầu vào người dùng < 20 ký tự, trước hết bổ sung vào phần mô tả task: hướng đi khác biệt, độc giả mục tiêu, và ít nhất một hook (điểm câu kéo) phi quy ước, rồi mới phát đi.

### User Steer

Định dạng: `[can thiệp người dùng] xxx`

- **Loại truy vấn** (hỏi trạng thái/thiết lập): xuất thẳng câu trả lời bằng văn bản, không cần gọi tool nữa; Host sẽ tiếp tục phát đi.
- **Loại chỉnh sửa** (yêu cầu sửa thiết lập/viết lại/chỉnh văn phong): đánh giá phạm vi ảnh hưởng:
  - Liên quan thay đổi thiết lập → gọi architect_* để làm `save_foundation(type=...)`
  - Liên quan các chương đã viết → để tool tự ghi các chương mục tiêu vào `PendingRewrites` (có thể nêu rõ ý định viết lại khi gọi writer lần sau)
  - Chỉ ảnh hưởng văn phong về sau → mô tả ngắn yêu cầu rồi đính vào phần mô tả task của writer ở lần nhận chỉ thị Host kế tiếp

## Tool

- `subagent(agent, task)`: gọi subagent
- `novel_context`: chỉ dùng khi truy vấn của người dùng cần, đừng gọi trước khi chỉ thị Host tới

## Subagent

- `architect_long` / `architect_short` / `writer` / `editor`

## Cấm

- Khi chỉ thị Host tới mà gọi novel_context trước rồi mới hành động
- Tự quyết bước kế tiếp khi không có user Steer và cũng không có chỉ thị Host
```

### 3.7 Kênh Reminder giảm tải mạnh

**Xóa**:
- `flow.go` (Host FollowUp đã ra chỉ thị cụ thể, nhắc nhở định tuyến của Reminder mất giá trị)
- `queue_guard.go` (hàng đợi do Host Router bảo đảm)
- `book_complete.go` (Host FollowUp chỉ thị xuất tổng kết khi Phase=Complete)

**Giữ**:
- `subagent_guards.go` (StopGuard của Writer/Architect/Editor, đảm bảo subagent không kết thúc tay trắng)
- Thêm một `foundation_reminder.go` nhẹ: báo cho Coordinator các hạng mục thiếu ở giai đoạn lập kế hoạch (đây là **thông tin mà phân xử cần** chứ không phải chỉ thị định tuyến)

**StopGuard được giữ**:
- StopGuard của Coordinator được giữ (chặn end_turn làm phương án dự phòng khi `Phase != Complete`)
- Thêm nhắc nhở khi "đã nhận chỉ thị Host nhưng lượt này chưa gọi subagent tương ứng"

### 3.8 resume.go đơn giản hóa nhẹ

`buildResumePrompt` hiện tại sinh chỉ thị ngôn ngữ tự nhiên chính xác tới từng step theo checkpoint (121 dòng).

Kiến trúc mới:
- Khi Resume, đọc Progress trước, Flow Router tính ra `NextInstruction`
- Coordinator nhận một resume prompt **cực ngắn**, rồi chờ chỉ thị FollowUp của Host

```
[Khôi phục] Cuốn "xxx" đã hoàn thành N chương, vào giai đoạn XX.
Hãy chờ chỉ thị kế tiếp của Host, hoặc xử lý can thiệp người dùng có thể còn lại trong lúc dừng máy.
```

Gần như toàn bộ logic phân nhánh hạ xuống Flow Router (Router vốn đã phải định tuyến theo trạng thái, Resume không cần đường đi đặc biệt).

---

## 4. Đánh giá mức độ đạt mục tiêu

### 4.1 Tính ổn định

| Rủi ro | Hiện tại | Kiến trúc mới |
|---|---|---|
| Coordinator chọn nhầm architect | từng xảy ra (lỗi định tuyến mid) | lúc khởi động vẫn là phân xử, nhưng prompt từ ba bậc còn nhị phân (đã làm), bề mặt lỗi thu hẹp đáng kể |
| Coordinator không tuân "chỉ nói viết chương N" | từng xảy ra | Host ra chỉ thị định dạng cố định, không cần LLM sinh mô tả task nữa |
| Coordinator bỏ sót kiểm tra queue_drained | từng xảy ra | Host Router ép đi theo đúng thứ tự |
| Cuối cung truyện, sau commit Coordinator quên gọi editor | có thể | Host Router phát hiện IsArcEnd && !HasArcReview thì phát đi trực tiếp |
| Bỏ sót nhánh khôi phục sau sự cố | lỗ hổng đã biết | máy trạng thái của Flow Router phủ tự nhiên mọi nhánh |
| StopGuard chặn liên tiếp 5 lần thì nâng lên fatal | tồn tại | sau khi chỉ thị Host rõ ràng, LLM khó chặn liên tiếp (trừ khi prompt hỏng nặng) |

### 4.2 Lợi tức nâng cấp LLM

| Chiều | Mức giữ lại |
|---|---|
| Nâng cấp mô hình Writer → chất lượng viết | 100% |
| Nâng cấp mô hình Editor → rà soát chính xác | 100% |
| Nâng cấp mô hình Architect → lập kế hoạch tinh tế | 100% |
| **Nâng cấp mô hình Coordinator → phân xử chuẩn hơn** | **100%** (giữ kịch bản phân xử) |
| ~~Nâng cấp mô hình Coordinator → định tuyến chuẩn hơn~~ | bỏ (tỉ lệ lỗi định tuyến vốn nên là 0, không cần LLM thông minh hơn) |

**Điểm giữ lại quan trọng**: các kịch bản phân xử như đánh giá can thiệp người dùng, chọn architect, phán định biên verdict vẫn do LLM xử lý, hưởng lợi trực tiếp từ nâng cấp mô hình.

### 4.3 Năng lực multi-agent

- Số lượng, chức năng, cách lắp ráp của SubAgent **hoàn toàn không đổi**
- Mô hình dị thể (coordinator/architect/writer/editor cấu hình độc lập) **hoàn toàn không đổi**
- Coordinator vẫn là run liên tục, giữ "góc nhìn toàn cuốn"
- Phương tiện cộng tác (các sản phẩm trong Store) không đổi

### 4.4 Tính đáp ứng

- Khả năng ngắt qua `coordinator.Inject` cho user Steer **được giữ hoàn toàn**
- Host Router phát chỉ thị khi SubAgent trả về, đi cùng một kênh message với user Steer
- Inject có ưu tiên cao hơn FollowUp (ngữ nghĩa `Inject` là chèn hàng), Steer không bị chỉ thị Host chen mất

### 4.5 Chi phí token

Hiện tại mỗi chương: Coordinator ~3500-7000 token × 1-2 turn = 3500-14000 token

Kiến trúc mới mỗi chương:
- prompt Coordinator nén từ ~3000 token xuống ~800 token
- vẫn cần 1 turn / chương (Coordinator đọc chỉ thị FollowUp + sinh tool_call)
- tổng ~1000-1500 token

**Tiết kiệm 60-80%**. Tiểu thuyết dài 200 chương tiết kiệm khoảng 400k-1M token (không bằng 100% của phương án quyết liệt, nhưng không hy sinh tính đáp ứng và góc nhìn toàn cuốn).

---

## 5. Ảnh hưởng tới docs/architecture.md

### 5.1 Điều chỉnh nguyên tắc cốt lõi §2

**Nguyên tắc một** (vòng lặp chính do LLM dẫn dắt) → điều chỉnh thành:
```
LLM dẫn dắt sáng tác và phân xử, Host dẫn dắt định tuyến quy trình.

- Sáng tác và phân xử (các quyết định cần hiểu ngữ nghĩa, phán đoán chất lượng, nhận diện
  ý định) vẫn để cho LLM
- Định tuyến quy trình (đọc sự kiện→tra bảng→phát chỉ thị) do code Host gánh
- Host không vòng qua Coordinator để gọi SubAgent trực tiếp, mà ra chỉ thị rõ ràng qua
  FollowUp, giữ Coordinator làm kênh thực thi chỉ thị và node phân xử
```

**Nguyên tắc hai** (đặt cược vào năng lực mô hình, không đặt cược vào hardcode) → điều chỉnh thành:
```
Ở chiều sáng tác và phân xử thì đặt cược vào mô hình (năng lực phân xử của Writer/Editor/
Architect/Coordinator), ở chiều định tuyến quy trình thì diễn đạt bằng code (không gian quyết
định của agent dọc là khép kín, tác vụ tra bảng không cho LLM lợi tức nào).
```

### 5.2 Điều chỉnh danh sách cấm §13

- §13.13 "không làm control plane mang tính tất định kiểu Host đọc file tín hiệu → inject chỉ thị bước kế tiếp" →
  **sửa câu chữ**: "không dùng file tín hiệu làm IPC (đọc trực tiếp Progress + Checkpoint là đủ); Host đọc sự kiện rồi ra chỉ thị gọi subagent rõ ràng qua `coordinator.FollowUp` là định tuyến dọc hợp lý"
- §13.14 "không hardcode máy trạng thái di chuyển Flow" →
  **sửa câu chữ**: "nhãn Flow vẫn chỉ do tool cập nhật (không viết máy trạng thái kiểu 'nếu A thì SetFlow(B)' bên trong Host), nhưng Flow Router có thể dựa trên Flow và các sự kiện khác để quyết gọi ai tiếp theo"

### 5.3 Điều chỉnh lắp ráp agent §7

- Giữ phần lắp ráp Coordinator
- `coordinator.md` cắt từ 88 dòng xuống ~25
- Kênh Reminder thu nhỏ (xóa flow/queue_guard/book_complete, giữ foundation/subagent_guards)
- Thêm package `internal/host/flow/`

---

## 6. Điểm yếu đã biết (liệt kê trung thực)

### 6.1 Tiến hóa dài hạn của Flow Router

- Khi thêm kịch bản mới (trạng thái flow mới, hậu xử lý cuối cung truyện mới), switch-case của Router sẽ dài ra
- Cần ràng buộc chặt: **chỉ xử lý định tuyến, không xử lý logic nghiệp vụ**; viết quy tắc quyết định thành unit test
- Lời cảnh báo từ `handleSubAgentDone` của v0.0.1 luôn còn giá trị; nhưng phương án này tránh trượt sang đối tượng thần thánh nhờ "hàm thuần + unit test + chỉ tiêu thụ sự kiện thực tế thuần"

### 6.2 Độ phức tạp của can thiệp người dùng

- Thiết kế hiện tại giao trọn Steer cho LLM của Coordinator phân xử
- Nhưng một số Steer trải dài nhiều loại (ví dụ "viết rõ nhân vật A ở vài chương đầu + về sau thêm tuyến phụ cho anh ta")
- Cần dựa vào năng lực LLM để bóc tách, prompt phải đưa hướng dẫn rõ ràng
- **Phần này hưởng lợi trực tiếp từ nâng cấp mô hình** (so với phân loại enum cứng của InterventionAgent, LLM phân xử linh hoạt khớp với kịch bản thực hơn)

### 6.3 Phụ thuộc tiền đề vào nhất quán của tầng sự kiện

- Router quyết định dựa trên Progress + Checkpoint, tầng sự kiện phải đáng tin
- `withWriteLock` hiện tại đóng gói tốt, bộ ba của commit_chapter hoàn tất nguyên tử
- Nhưng nếu tầng sự kiện xuất hiện bất nhất (ví dụ Progress nói chương 3 đã xong nhưng dưới chapters/ không có), Router sẽ ra quyết định sai
- Đề xuất: thêm một lần **kiểm tra nhất quán tầng sự kiện** lúc khởi động (nếu phát hiện Progress.CompletedChapters không khớp thư mục chapters/ thì báo warning)

### 6.4 Coordinator vẫn giữ khả năng định tuyến bằng LLM

- Dù chỉ thị rõ ràng, LLM vẫn có thể "sáng tạo" không thực thi (ví dụ sinh một đoạn suy nghĩ rồi mới gọi tool)
- StopGuard dự phòng: nhận chỉ thị Host nhưng lượt này không gọi subagent thì inject nhắc nhở
- Đây là dự phòng, không phải lệnh cấm — đôi khi mô hình mạnh "nghĩ thêm một bước" cũng không xấu

### 6.5 Yêu cầu độ phủ test cao hơn

- Flow Router là hàm thuần, bắt buộc có unit test đầy đủ (phủ mọi tổ hợp Phase × Flow × Boundary)
- Test tích hợp: mô phỏng chuỗi đầy đủ "commit → router → FollowUp → coordinator phản hồi → subagent"
- Test khôi phục sự cố: kill tiến trình rồi resume, khẳng định Router suy ra đúng bước kế tiếp

---

## 7. Lộ trình triển khai

### Giai đoạn 1: Củng cố tầng sự kiện (~0.5 ngày)

- Hoàn thiện kiểm tra nhất quán ở §6.3: quét một lần lúc khởi động/Resume, sinh warning
- Đảm bảo các API `store.HasArcReview(vol, arc)` và `HasArcSummary(vol, arc)` khả dụng (chưa có thì thêm)

### Giai đoạn 2: Đưa vào khung Flow Router (~1 ngày)

- Tạo package `internal/host/flow/`:
  - `route.go` — hàm thuần `Route(state) → *NextInstruction`
  - `dispatcher.go` — đăng ký sự kiện + FollowUp phát chỉ thị
  - `route_test.go` — unit test phủ mọi nhánh
- Điều khiển kích hoạt bằng công tắc config `flow_driven: true/false`
- Mặc định tắt (false), chạy đối chứng trước

### Giai đoạn 3: Kích hoạt và kiểm chứng (~1 ngày)

- Bật `flow_driven: true`
- Chạy một tiểu thuyết 30-50 chương, đối chiếu chỉ số:
  - Số lần gọi LLM Coordinator
  - Số lỗi định tuyến (nên là 0)
  - Tính đáp ứng (ngắt bằng steer có bình thường không)
- Sửa bug, điều chỉnh quy tắc Router

### Giai đoạn 4: Đơn giản hóa coordinator.md + giảm tải Reminder (~0.5 ngày)

- Sửa coordinator.md theo §3.6
- Xóa `reminder/flow.go / queue_guard.go / book_complete.go`
- Giữ foundation reminder cần thiết
- Cập nhật StopGuard của subagent nếu cần (thường không)

### Giai đoạn 5: Đơn giản hóa resume.go (~0.5 ngày)

- Xóa phần lớn nhánh của `buildResumePrompt`
- Thay bằng message ngắn gọn chung "[Khôi phục] hãy chờ chỉ thị Host"
- Sau Resume, Router tự suy ra hành động tiếp tục

### Giai đoạn 6: Cập nhật tài liệu kiến trúc (~0.5 ngày)

- Sửa `docs/architecture.md` §2 / §13 / §7 theo §5
- Đổi trạng thái tài liệu đề xuất này thành "Đã chấp nhận", lưu trữ vào `docs/history/`

### Giai đoạn 7: Thời kỳ quan sát (2-4 tuần)

- Chạy liên tục 2-3 tiểu thuyết dài (mỗi cuốn 100+ chương)
- Ghi lại mọi lỗi định tuyến (nếu có), vấn đề đáp ứng, hành vi bất ngờ của Coordinator
- Tinh chỉnh quy tắc Router và coordinator.md dựa trên quan sát

**Tổng cộng khoảng 4 ngày triển khai + thời kỳ quan sát.**

---

## 8. Bảng so sánh

| Chiều | Kiến trúc hiện tại | Hybrid (phương án này) | Phương án quyết liệt (Phụ lục A) |
|---|---|---|---|
| Tính ổn định | trung (LLM thi thoảng định tuyến sai) | **cao** | cao |
| Tính đáp ứng | cao | **cao** | **thấp** (Host gọi SubAgent trực tiếp, không ngắt được) |
| Lợi tức LLM | 100% | **100%** | 85% (bỏ chiều định tuyến) |
| Tiết kiệm token | 0 | ~70% | ~95% |
| Góc nhìn toàn cuốn | có | **có** | không (mỗi SubAgent độc lập) |
| Chi phí triển khai | - | trung (~4 ngày) | cao (~1 tuần + sửa agentcore) |
| Cập nhật tài liệu | - | nhỏ (tinh chỉnh §2/§13) | lớn (viết lại nguyên tắc §2) |
| Cần sửa agentcore | - | không | có thể (gọi SubAgent trực tiếp) |
| Độ khó rollback | - | thấp (công tắc config) | cao |

---

## 9. Điểm quyết định

1. **Có chấp nhận đề xuất này (Hybrid Coordinator) không?** [ ] Chấp nhận · [ ] Chấp nhận sau khi sửa · [ ] Không chấp nhận
2. Có nên triển khai Giai đoạn 3 như một PR độc lập để kiểm chứng trước không? [ ]
3. Các điều chỉnh `docs/architecture.md` §2 / §13 có xử lý luôn lần này không? [ ]
4. Độ dài thời kỳ quan sát: [ ] 2 tuần · [ ] 4 tuần · [ ] dài hơn

---

## Phụ lục A: Phương án quyết liệt đã đánh giá (xóa hoàn toàn Coordinator)

> Phương án bản thảo đầu. Bị hạ xuống mức tham khảo do tính đáp ứng thụt lùi, tính khả thi kỹ thuật còn nghi vấn, mất góc nhìn toàn cuốn của Coordinator.

Cốt lõi của phương án quyết liệt: Host gọi trực tiếp `SubAgentTool.Execute`, không qua LLM của Coordinator.

**Các vấn đề đã nhận diện**:

1. **Tính đáp ứng thụt lùi**: `SubAgentTool.Execute` là gọi đồng bộ chặn (blocking); user Steer phải chờ SubAgent hiện tại trả về mới xử lý được. `Inject` của kiến trúc hiện tại có thể ngắt ngay.
2. **Tính khả thi kỹ thuật còn nghi vấn**:
   - Host gọi trực tiếp SubAgentTool vi phạm thông lệ sử dụng agentcore
   - Luồng sự kiện (Event của `Subscribe`) có thể không nổi bọt (bubble) đúng tới observer
   - Đường callback `ContextManagerFactory` / `OnMessage` của SubAgent chưa rõ
   - Cần sửa agentcore hoặc đại tu observer
3. **Mất góc nhìn toàn cuốn của Coordinator**: mỗi SubAgent là một run độc lập, không có "người canh gác LLM liên tục". Trong chặng dài, các vấn đề như trôi dạt văn phong, đứt gãy nhân vật mất đi một lớp bảo vệ vô hình.
4. **InterventionAgent đơn giản hóa quá mức**: phương án quyết liệt dùng enum (query/modify_setting/rewrite_chapters/adjust_style/noop) để phân loại ý định người dùng, Steer thực có thể trải nhiều loại, ép schema sẽ phân loại sai.
5. **Khối lượng viết lại tài liệu kiến trúc lớn**: nguyên tắc cốt lõi §2 bị lật, 30% luận điểm tài liệu bị ảnh hưởng.
6. **FlowDriver sẽ phình thành đối tượng thần thánh**: một vòng lặp nhồi toàn bộ logic định tuyến, thêm kịch bản nào cũng phải sửa, đồng dạng với `handleSubAgentDone` của v0.0.1.

Phương án Hybrid né được 4 vấn đề đầu, vấn đề thứ 5 hạ xuống mức tinh chỉnh, vấn đề thứ 6 được kiểm soát qua "hàm thuần + unit test".

---

## Phụ lục B: Chi tiết định vị các điểm quyết định

| Điểm quyết định | Vị trí hiện tại | Vị trí kiến trúc mới | Loại |
|---|---|---|---|
| Chọn architect | coordinator.md L26-29 | Coordinator LLM phân xử (lúc khởi động) | phân xử |
| Mở rộng đầu vào | coordinator.md L31 | Coordinator LLM phân xử (lúc khởi động) | phân xử |
| Vòng lặp bổ sung kế hoạch | coordinator.md L36-38 | Nhánh Host Router Phase=Premise/Outline (trả nil để LLM tự chủ hoặc FollowUp architect tường minh) | hỗn hợp |
| Bước kế tiếp mỗi chương | coordinator.md L46-51 + reminder/flow | **Nhánh 2d của Host Router** (FollowUp writer) | định tuyến |
| Rà soát cuối cung truyện | coordinator.md L78-82 | **Nhánh 2c của Host Router** (FollowUp editor/architect) | định tuyến |
| Rẽ nhánh verdict | coordinator.md L59-61 + tool save_review | tầng tool đã code hóa, Router chỉ đọc Flow | định tuyến (đã xong) |
| Can thiệp người dùng | coordinator.md L67-70 | Coordinator LLM phân xử (khi nhận message Inject) | phân xử |
| Phát lại khi architect báo lỗi | coordinator.md L40 | Host Router phát hiện FoundationMissing không đổi, đếm số lần thử lại | định tuyến |
| Tổng kết khi hoàn thành cả cuốn | coordinator.md L63-65 + reminder/book_complete | Host Router phát hiện Phase=Complete → FollowUp "xuất tổng kết" | định tuyến |

---

## Phụ lục C: Vị trí mã nguồn tham chiếu

- `assets/prompts/coordinator.md` — chờ đơn giản hóa
- `internal/host/reminder/flow.go` / `queue_guard.go` / `book_complete.go` — chờ xóa
- `internal/host/reminder/subagent_guards.go` — giữ
- `internal/host/reminder/stop_guard.go` — giữ + thêm kiểm tra "chỉ thị Host nhận được phải thực thi"
- `internal/host/resume.go` — đơn giản hóa mạnh
- `internal/host/observer.go` — đăng ký mới EventToolExecEnd để kích hoạt Router
- `internal/host/flow/` — package mới
- `internal/tools/commit_chapter.go` L220-280 — 17 trường CommitResult đã đầy đủ
- `internal/tools/save_review.go` L76-116 — nâng cấp verdict và di chuyển Flow đã code hóa
- `internal/store/outline.go` `CheckArcBoundary` — API sự kiện biên cung truyện
