# Sổ tay quan sát (observability)

> [English](observability.en.md)

Khi chạy tiểu thuyết dài, làm sao biết các cơ chế có thực sự hoạt động không?

Tài liệu này không phải là chép lại nguyên các quy tắc diag, mà hướng tới **vận hành thực tế**: bạn đã chạy tới chương thứ N, thì nên mở file nào, xem trường nào, để phán đoán là khỏe mạnh hay bất thường.

---

## 1. Quy trình rà soát chung

```
1. /diag                       # tự động chẩn đoán, xem khu vực Findings
2. cd output/{novel}/meta/     # cat trực tiếp các artifact then chốt
3. cat meta/sessions/coordinator.jsonl | tail  # xem vài lượt LLM gần nhất
```

Những sự thật mà `/diag` không phủ tới (bao gồm các mục "chẩn đoán còn thiếu" liệt kê trong tài liệu này) cần tra tay ở bước 2-3.

### Báo issue: xuất chẩn đoán đã ẩn danh

Mỗi lần `/diag` đều ghi thêm `output/{novel}/meta/diag-export.md` — một bản chẩn đoán **đã ẩn danh** (đã gỡ bỏ phần chính văn tiểu thuyết / prompt / suy nghĩ, chỉ giữ bộ khung hành vi: tên tool, chuỗi lỗi, số lần lặp, phase/flow, step bị kẹt, phân loại lỗi trong log). Gặp các vấn đề kiểu vòng lặp chết / gián đoạn, chỉ cần dán file này vào GitHub issue là người bảo trì có thể định vị, không cần dữ liệu `output/` của bạn.

---

## 2. Bảng tra nhanh các artifact then chốt

Sắp xếp theo "đường rà soát thường gặp nhất khi có sự cố":

| Artifact | Đường dẫn | Xem gì | Khỏe mạnh | Không khỏe |
|---|---|---|---|---|
| Tiến độ | `meta/progress.json` | `phase` / `flow` / `completed_chapters` | phase tiến đơn điệu, flow nằm trong tập hợp hợp lệ | phase lùi lại / flow kẹt ở một trạng thái |
| La bàn | `meta/compass.json` | khoảng cách giữa `last_updated` và chương mới nhất | gap < 15 chương | gap > 15 chương (trúng CompassDrift) |
| Danh bạ nhân vật phụ | `meta/cast_ledger.json` | số mục / tỷ lệ điền brief_role / nhất quán tên | xem §4 | xem §4 |
| Sổ phục bút | `meta/foreshadow.json` | số chương đình trệ lâu nhất của mục `status="planted"` | < số chương/3 | > số chương/3 (trúng StaleForeshadow) |
| Dàn ý | `meta/layered_outline.json` | số chương chưa viết còn lại của quyển hiện tại | đã triển khai trước 1-2 chương | đã viết tới chương hiện tại nhưng chương sau không có outline (OutlineExhausted) |
| Hồ sơ nhân vật | `meta/characters.json` | có tìm thấy nhân vật core/important trong tóm tắt N chương gần nhất không | đều tìm thấy | vắng mặt (trúng GhostCharacter) |
| Checkpoint | `meta/checkpoints.jsonl` | `step` của dòng gần nhất có khớp progress không | nhất quán | không nhất quán (khôi phục sau sự cố chưa tự lành) |
| Phiên Coordinator | `meta/sessions/coordinator.jsonl` | mẫu hình tool_call của 5-10 lượt gần nhất | mỗi lượt đẩy tiến nhanh | cùng một tool gọi rỗng nhiều lần (kẹt vòng lặp chết) |

---

## 3. Quan sát la bàn (compass)

**Thời điểm sửa**: 2026-05-08 (commit `fix: update_compass 工具自动填 last_updated`)

### Xem gì

```bash
cat output/{novel}/meta/compass.json
```

Ngữ nghĩa các trường:
- `ending_direction`: hướng kết cục (nên khớp với đoạn "终局方向" trong `premise.md`)
- `open_threads`: tuyến dài đang hoạt động (Architect thêm/bớt theo ranh giới mỗi quyển)
- `estimated_scale`: quy mô ước tính (ví dụ "4-6 quyển", cập nhật theo ranh giới mỗi quyển)
- `last_updated`: **tool tự động điền** thành số chương đã hoàn thành lớn nhất tại thời điểm cập nhật (không còn phụ thuộc LLM tự điền)

### Phán đoán mức độ khỏe mạnh

| Tín hiệu | Phán đoán |
|---|---|
| `last_updated` nằm trong khoảng `[latest-15, latest]` | khỏe mạnh |
| `last_updated` trễ hơn latest quá 15 chương | Architect không cập nhật ở ranh giới cung truyện/quyển — kiểm tra prompt architect-long.md |
| `last_updated == 0` | **dữ liệu bẩn từ trước bản sửa này**, lần `update_compass` tới sẽ tự lành |
| `ending_direction` không khớp đoạn "终局方向" trong premise.md | Architect lén sửa ý định người dùng — ghi lại, quyết định có nên đóng băng trường này không (vấn đề thiết kế, xem todo.md) |

### Cách xác minh bản sửa có hiệu lực

So sánh trước/sau khi chạy truyện dài:
- **Trước khi sửa**: sau khi chạy 30+ chương thì `compass.last_updated` khả năng cao là `0` hoặc một số chương ở giai đoạn đầu
- **Sau khi sửa**: mỗi lần Architect gọi `update_compass`, `last_updated` đều bị tầng tool ghi đè thành latest hiện tại

---

## 4. Quan sát danh bạ nhân vật phụ (cast_ledger)

**Tính năng triển khai**: 2026-05-08 (commit `feat: 新增配角名册自动追踪次要角色`)

### Xem gì

```bash
cat output/{novel}/meta/cast_ledger.json | jq 'length'                     # tổng số mục
cat output/{novel}/meta/cast_ledger.json | jq '[.[] | select(.brief_role == "" or .brief_role == null)] | length'  # số mục thiếu brief_role
cat output/{novel}/meta/cast_ledger.json | jq '[.[] | select(.appearance_count >= 3)] | length'   # số mục xuất hiện thường xuyên (≥3 lần)
cat output/{novel}/meta/cast_ledger.json | jq 'sort_by(-.appearance_count) | .[:10]'  # 10 nhân vật xuất hiện nhiều nhất
```

### Phán đoán mức độ khỏe mạnh

| Chiều | Khỏe mạnh | Bất thường | Cách xử lý |
|---|---|---|---|
| **Số mục vs số chương đã hoàn thành** | số mục ledger ≈ số chương đã hoàn thành × 0.3-0.6 | > số chương × 0.8 (nhân vật thoáng qua bị đăng ký nhầm) | kiểm tra đoạn `cast_intros` trong writer.md đã đủ rõ chưa |
| **Tỷ lệ điền brief_role** | thiếu < 30% | thiếu > 50% | Writer bỏ sót nghiêm trọng — prompt dẫn dắt chưa đủ |
| **Độ tương đồng trùng tên** | không có nghi vấn một người nhiều tên | đồng thời xuất hiện "李X" / "老李" / "X掌柜" | LLM trôi dạt tên — thêm ràng buộc vào prompt "dùng tên nhất quán" hoặc thêm tool steer hợp nhất cho người dùng |
| **Nhân vật xuất hiện thường xuyên** | các mục có `appearance_count >= 5` thưa thớt | nhiều mục xuất hiện tần suất cao xuyên cung truyện | nên cân nhắc nâng cấp lên hồ sơ core (kênh nâng cấp ở giai đoạn 3) |
| **Recall có được tiêu thụ không** | khi Writer viết về nhân vật cũ, trường characters của commit_chapter chứa tên đã có trong ledger | Writer lặp lại phát minh cùng một tên (xuất hiện "老周A" và "老周B") | recent_cast recall chưa được tiêu thụ — kiểm tra đoạn "配角连续性" trong writer.md |

### Xác minh luồng dữ liệu (đầu-cuối)

Sau khi chạy 5 chương:
1. `cat meta/cast_ledger.json` không nên rỗng (trừ khi mỗi chương chỉ dùng nhân vật core)
2. Nếu Writer giới thiệu "老周" ở chương 1:
   - trong `cast_ledger` phải có mục `老周`, `appearance_count=1`
3. Nếu chương 5 lại viết về 老周:
   - `老周.appearance_count=2`, `last_seen_chapter=5`
4. Trong `meta/sessions/agents/writer-*.jsonl`, giá trị trả về của novel_context ở chương 5 phải thấy 老周 trong `episodic_memory.recent_cast`
5. Nếu bước trên thấy nhưng Writer không tiêu thụ (老周 viết ra không khớp với chương 1) — đây là vấn đề prompt

### Hiện chưa có chẩn đoán tự động (nhưng snapshot đã được nạp)

`diag.Snapshot.CastLedger` đã được đọc trong `Load()`, có thể được các quy tắc tiêu thụ trực tiếp — nhưng hiện chưa viết quy tắc nào. Việc xác minh vẫn dựa vào các lệnh `jq` tra tay ở trên.

Nếu sau này muốn bổ sung quy tắc chẩn đoán (ứng viên):
- `CastBriefRoleMissing`: cảnh báo khi tỷ lệ thiếu > 50%
- `CastBloat`: cảnh báo khi số mục > số chương × 0.8
- `CastPromotionCandidate`: appearance_count ≥ 5 và xuyên cung truyện → đề xuất nâng cấp

Đừng chốt ngưỡng ngay bây giờ — chờ dữ liệu truyện dài có rồi, xem phân bố thực tế mới định. Bản thân mã quy tắc chỉ cần 30-50 dòng.

---

## 5. Writer có làm việc đúng kỳ vọng không

Khi chạy truyện dài, điều quan tâm nhất là **Writer có thực sự hành xử theo prompt không**. Cách quan sát trực tiếp nhất là session log:

```bash
ls output/{novel}/meta/sessions/agents/    # mỗi subagent một file jsonl
tail -50 output/{novel}/meta/sessions/agents/writer-*.jsonl
```

Xem vài hành vi cụ thể:

| Hành vi kỳ vọng | Thể hiện trong jsonl |
|---|---|
| Writer đã xem recent_cast | trường `episodic_memory.recent_cast` trong giá trị trả về của tool novel_context không rỗng |
| Writer đã điền cast_intros khi commit_chapter | tham số tool_call `cast_intros` là mảng không rỗng (chỉ ở chương giới thiệu nhân vật mới) |
| Writer đã dùng đề xuất các chương liên quan | số lần gọi `read_chapter` > 1 (mặc định 1 lần, vượt quá nghĩa là đã tra ngược lại) |
| Writer không vi phạm thứ tự tool | chuỗi tool_call tuân thủ nghiêm `novel_context → read_chapter → plan_chapter → draft_chapter → check_consistency → commit_chapter` |

Nếu trong jsonl thấy Writer gọi rỗng novel_context nhiều lần, hoặc sau commit_chapter còn gọi tool khác — là prompt chưa kìm được.

---

## 6. Lằn ranh đỏ trong kịch bản chạy dài

Khi chạy truyện dài 100+ chương, hễ trúng bất kỳ điều nào dưới đây thì nên dừng lại rà soát:

- [ ] CompassDrift trúng và kéo dài 2 cung truyện chưa gỡ được
- [ ] số mục cast_ledger > số chương đã hoàn thành × 0.8
- [ ] tỷ lệ điền brief_role trong cast_ledger < 30%
- [ ] cùng một nhân vật xuất hiện nghi vấn nhiều tên ("老李" / "李掌柜" cùng tồn tại)
- [ ] Writer viết chương mới mà không đọc nhân vật cũ đã có trong recent_cast (lặp lại phát minh)
- [ ] trong session Coordinator xuất hiện ≥ 5 lần liên tiếp gọi rỗng novel_context
- [ ] sau khi commit chương bất kỳ, `meta/checkpoints.jsonl` không có step `commit_chapter` tương ứng

4 điều đầu là mức độ khỏe mạnh của các cơ chế mới lần này; 3 điều sau là độ ổn định của các cơ chế đã có.

---

## 7. Quy phạm bảo trì tài liệu

**Khi thêm artifact tầng sự thật mới (tạo một `meta/*.json` / `meta/*.jsonl`), đồng bộ:**

1. Thêm một dòng tra nhanh vào §2 của tài liệu này
2. Nếu artifact cần quan sát chuyên biệt (không phải phán đoán đơn giản "có/không tồn tại"), thêm một đoạn chuyên đề §X
3. Nếu muốn chẩn đoán tự động, nạp trong `internal/diag/snapshot.go::Load` và thêm quy tắc trong `internal/diag/rules_*.go`

**Đừng:**
- Đừng chép tất cả các quy tắc trong `internal/diag/` vào tài liệu này (đó là tham chiếu quy tắc, không phải sổ tay quan sát)
- Đừng viết quy tắc chẩn đoán cho mọi cơ chế — ngưỡng chốt theo cảm tính sẽ sai, quan sát trước rồi bổ sung sau
