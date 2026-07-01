# ainovel-cli

> [English](README.en.md)

Engine sáng tác tiểu thuyết dài bằng AI hoàn toàn tự động. Coordinator chỉ trong một lần Prompt sẽ điều phối ba subagent Architect / Writer / Editor để hoàn thành cả cuốn sách, còn Host chỉ lo khởi động, khôi phục và quan sát. Từ một câu yêu cầu đến một cuốn tiểu thuyết hoàn chỉnh, toàn bộ quá trình không cần can thiệp thủ công.

<p align="center">
  <img src="scripts/sample.gif" alt="ainovel-cli demo" width="800">
  <img src="scripts/novel.png" alt="ainovel-cli bg" width="800">
</p>

## Đặc điểm

- **Cộng tác đa tác tử** — Coordinator điều phối ba subagent Architect / Writer / Editor trong một vòng lặp dài duy nhất, tự chủ quyết định quy trình sáng tác
- **Vòng lặp dài do LLM dẫn dắt** — Một lần Prompt viết xong cả cuốn sách, Host không xen vào điều phối. Càng đơn giản càng ổn định, từ chối orchestration phức tạp
- **Khôi phục checkpoint cấp Step** — Sau khi mỗi tool thực thi thành công sẽ ghi checkpoint, sau khi sập có thể khôi phục chính xác đến cấp bước plan/draft/check/commit
- **Quy hoạch cuốn lăn hai tầng** — Truyện dài không còn quy hoạch toàn bộ chương một lần. Ban đầu chỉ quy hoạch khung 2 cung truyện đầu + chương chi tiết của cung 1, các cung/quyển về sau sẽ do Architect mở rộng khi viết tới, mỗi lần mở rộng đều tham chiếu tóm tắt phần trước và trạng thái nhân vật, để quy hoạch xa không bị rỗng
- **Gợi ý thông minh chương liên quan** — Khi viết mỗi chương, hệ thống tự động gợi ý các chương lịch sử liên quan theo bốn chiều: phục bút (cài cắm), nhân vật xuất hiện, biến đổi trạng thái và quan hệ; kết hợp với phần giới thiệu chương kế tiếp để đảm bảo tính liên tục cho truyện dài 500+ chương
- **Chiến lược context thích ứng** — Tự động chuyển đổi giữa toàn lượng / cửa sổ trượt / tóm tắt phân tầng dựa theo tổng số chương, hỗ trợ truyện dài 500+ chương
- **Rà soát chất lượng bảy chiều** — Editor rà soát theo bảy chiều: nhất quán thiết lập, hành vi nhân vật, nhịp điệu, mạch tự sự liền lạc, phục bút, hook và chất lượng thẩm mỹ; trong đó chiều thẩm mỹ chia nhỏ thành năm hạng mục (chất cảm miêu tả / thủ pháp tự sự / độ phân biệt đối thoại / chất lượng dùng từ / sức lay động cảm xúc), mỗi hạng mục bắt buộc phải dẫn nguyên văn làm chứng
- **Can thiệp thời gian thực của người dùng** — Trong quá trình viết có thể tiêm ý kiến chỉnh sửa vào ô nhập bất cứ lúc nào (không cần tạm dừng), hệ thống tự động đánh giá phạm vi ảnh hưởng và viết lại các chương bị tác động
- **Cổng vào TUI hợp nhất** — Giao diện tương tác quan sát tiến độ theo thời gian thực, cũng hỗ trợ mang theo một câu yêu cầu để khởi động trực tiếp
- **Hỗ trợ đa LLM** — OpenRouter / Anthropic / Gemini / OpenAI v.v. chuyển đổi tùy ý

## Kiến trúc

Thiết kế cốt lõi: **LLM dẫn dắt, Host phục vụ**. Coordinator tự chủ quyết định quy trình sáng tác cả cuốn sách trong một lần Run, Host chỉ lo khởi động, khôi phục và quan sát sự kiện.

```
┌─────────────────────────────────────────────────┐
│                Host (vỏ mỏng)                    │
│      Khởi động / Khôi phục / Quan sát / Tiêm can thiệp │
└──────────────────────┬──────────────────────────┘
                       │ một lần Prompt
┌──────────────────────▼──────────────────────────┐
│           Coordinator (vòng lặp dài LLM)         │
│  đọc novel_context → gọi subagent → đọc kết quả → tiếp │
└────┬──────────┬──────────┬──────────────────────┘
     │          │          │
 ┌───▼────┐ ┌───▼───┐ ┌────▼────┐
 │Architect│ │Writer │ │ Editor  │
 └───┬────┘ └───┬───┘ └────┬────┘
     └──────────┼──────────┘
                │ gọi tool (IO + checkpoint)
┌───────────────▼─────────────────────────────────┐
│                   Store                         │
│  Progress / Checkpoint / Outline / Drafts / ... │
└─────────────────────────────────────────────────┘
```

- **Host** — Khởi động Coordinator, khôi phục sau sự cố, chiếu sự kiện cho TUI. Không đưa ra bất kỳ quyết định điều phối nào
- **Coordinator** — Người ra quyết định duy nhất, dẫn dắt toàn bộ quy trình quy hoạch → viết → rà soát → tổng kết trong một lần Run
- **SubAgents** — Architect / Writer / Editor mỗi cái có context độc lập, cộng tác qua các artifact trong Store
- **Tools** — IO nguyên tử + ghi checkpoint, chỉ trả về JSON sự thật, không kèm chỉ thị

### Trách nhiệm các tác tử

| Tác tử | Trách nhiệm | Tool |
|--------|------|------|
| **Coordinator** | Điều phối toàn cục, xử lý phán quyết rà soát và can thiệp người dùng | `subagent` `novel_context` |
| **Architect** | Sinh tiền đề, dàn ý, hồ sơ nhân vật, quy tắc thế giới | `novel_context` `save_foundation` |
| **Writer** | Tự chủ hoàn thành việc lên ý tưởng, viết, tự rà và commit một chương | `novel_context` `read_chapter` `plan_chapter` `draft_chapter` `check_consistency` `commit_chapter` |
| **Editor** | Đọc nguyên văn, rà soát ở hai tầng cấu trúc và thẩm mỹ | `novel_context` `read_chapter` `save_review` `save_arc_summary` `save_volume_summary` |

### Quy trình viết

```
Yêu cầu người dùng → Architect quy hoạch khung + chương cung đầu → Writer viết từng chương → Editor rà soát cấp cung
                                                  ↑                   │
                                                  ├── viết lại/đánh bóng ◄──────┘
                                                  │
                                       Architect mở rộng cung/quyển kế tiếp
                                      (tham chiếu tóm tắt phần trước + ảnh chụp nhân vật)
```

Writer hoàn thành mỗi chương theo trình tự cố định (nội dung viết hoàn toàn tự chủ, trình tự gọi tool nghiêm ngặt):

1. `novel_context` — Nạp context (tóm tắt tình tiết trước, phục bút, trạng thái nhân vật, quy tắc phong cách, gợi ý chương liên quan)
2. `read_chapter` — Đọc lại phần trước để tìm lại giọng điệu và nhịp điệu
3. `plan_chapter` — Lên ý tưởng mục tiêu, xung đột, cung cảm xúc của chương này
4. `draft_chapter` — Viết toàn bộ phần chính của chương
5. `check_consistency` — Đối chiếu dữ liệu trạng thái để kiểm tra nhất quán (bắt buộc sau draft)
6. `commit_chapter` — Commit bản chốt, trả về các trường sự thật (`arc_end_reached` / `next_chapter` v.v.), bước tiếp theo do Reminder dẫn dắt

### Quy tắc chuyển đổi trạng thái

Hệ thống chia trạng thái chạy nội bộ thành hai tầng:

- **Phase** — Giai đoạn lớn, biểu thị tác phẩm hiện đang ở kỳ thiết lập, kỳ viết hay đã hoàn thành
- **Flow** — Quy trình đang hoạt động hiện tại, biểu thị hệ thống lúc này đang viết bình thường, rà soát, viết lại, đánh bóng hay xử lý can thiệp người dùng

#### Phase

`Phase` áp dụng quy tắc "chỉ tiến không lùi":

```text
init -> premise -> outline -> writing -> complete
  \-------> outline ------^
  \--------------> writing
```

Ý nghĩa:

- `init` — Tác vụ đã được tạo, chưa hình thành thiết lập ổn định
- `premise` — Đã lưu tiền đề câu chuyện
- `outline` — Đã lưu dàn ý, có thể bước vào viết chính thức
- `writing` — Đã bước vào kỳ sáng tác chương
- `complete` — Toàn bộ quy trình của cuốn sách kết thúc

Giải thích quy tắc:

- Cho phép cập nhật cùng trạng thái, ví dụ `writing -> writing`
- Cho phép tiến tới, ví dụ `outline -> writing`
- Không cho phép lùi lại, ví dụ `writing -> premise`, `complete -> writing`

#### Flow

`Flow` chỉ mô tả quy trình đang hoạt động trong kỳ viết, cho phép chuyển đổi giữa vài luồng công việc:

```text
writing   -> reviewing / rewriting / polishing / steering / writing
reviewing -> writing / rewriting / polishing / steering / reviewing
rewriting -> writing / steering / rewriting
polishing -> writing / steering / polishing
steering  -> writing / reviewing / rewriting / polishing / steering
```

Ý nghĩa:

- `writing` — Tiến tới chương kế tiếp bình thường
- `reviewing` — Editor đang rà soát
- `rewriting` — Xử lý các chương bắt buộc phải viết lại
- `polishing` — Xử lý các chương chỉ cần đánh bóng
- `steering` — Đang đánh giá và xử lý can thiệp người dùng

Giải thích quy tắc:

- Cho phép `writing -> reviewing`, ví dụ chương sau khi commit kích hoạt rà soát
- Cho phép `reviewing -> rewriting/polishing/writing`, do kết quả rà soát quyết định
- Cho phép `steering -> writing/reviewing/rewriting/polishing`, do phạm vi ảnh hưởng của can thiệp quyết định
- Không cho phép các bước nhảy bất thường rõ rệt, ví dụ `rewriting -> reviewing`

Các quy tắc này hiện được ràng buộc thống nhất bởi một bộ kiểm tra nhẹ trong code, tránh trạng thái lùi lại hoặc nhảy sang nhánh quy trình bất hợp lý.

### Quy hoạch lăn cho truyện dài

Phương án truyền thống quy hoạch tất cả chương một lần, đến 300+ chương thì dàn ý rỗng tuếch, nhịp điệu như chạy đua tiến độ. Hệ thống này áp dụng **quy hoạch lăn theo la bàn + tầm nhìn**, mô phỏng quy trình sáng tác thực của tác giả truyện mạng:

```
Quy hoạch ban đầu             Khi cung kết thúc              Khi quyển kết thúc
┌────────────────────┐    ┌─────────────────────┐    ┌─────────────────────┐
│ Hướng kết cục (la bàn)│    │ Editor rà soát cấp cung │    │ Editor rà soát cấp quyển │
│ Khởi động 2 quyển, sau│    │ Tóm tắt cung + ảnh chụp NV│    │ Tóm tắt quyển           │
│ theo nhu cầu          │    │                       │    │                         │
│ Chương chi tiết cung 1 │ →  │ Architect mở rộng cung sau│ →  │ Architect tự chủ tạo    │
│ Nhân vật + thế giới quan│   │ Writer viết tiếp        │    │ quyển sau + cập nhật la bàn│
└────────────────────┘    └─────────────────────┘    └─────────────────────┘
```

- **La bàn (Compass)** — Hướng kết cục + tuyến dài đang hoạt động + ước lượng quy mô, mỗi ranh giới quyển do Architect cập nhật, hướng câu chuyện có thể tiến hóa theo quá trình sáng tác
- **Sinh theo nhu cầu** — Sau khi viết xong quyển hiện tại, Architect tự chủ tạo quyển kế tiếp dựa trên nội dung đã viết. Quy hoạch ban đầu sinh 2 quyển để khởi động, các quyển về sau sinh theo nhu cầu
- **Cung khung** — Chỉ có goal + ước lượng số chương, đến nơi mới mở rộng chương chi tiết
- **Tinh chỉnh dần** — Mỗi lần mở rộng đều tham chiếu tóm tắt phần trước, ảnh chụp nhân vật, quy tắc phong cách, càng viết về sau càng chính xác
- **Mẫu nhịp điệu thông dụng** — Cung trưởng thành đột phá / cung tranh đấu thi đấu / cung khám phá phát hiện / cung ân oán xung đột / cung quá độ thường nhật, mỗi loại cung có mật độ tham chiếu và ánh xạ thể loại phù hợp

### Quản lý context cho truyện dài

Tiểu thuyết 500+ chương áp dụng ba tầng tóm tắt + đường ống nén bốn cấp + gợi ý thông minh:

```
Quyển (Volume) → tóm tắt quyển
└── Cung (Arc) → tóm tắt cung + ảnh chụp nhân vật + quy tắc phong cách
    └── Chương (Chapter) → tóm tắt chương (cửa sổ trượt 3 chương gần nhất)
```

- **Tóm tắt phân tầng** — Gần dùng tóm tắt chương, trung bình dùng tóm tắt cung, xa dùng tóm tắt quyển, nén tầng tầng không mất thông tin
- **Gợi ý chương liên quan** — Khi viết mỗi chương, truy ngược các chương lịch sử theo bốn chiều phục bút, nhân vật xuất hiện, biến đổi trạng thái, quan hệ; gợi ý Writer đọc lại theo nhu cầu
- **Giới thiệu chương kế tiếp** — Nạp dàn ý chương kế tiếp, giúp Writer thiết kế hook cuối chương và nối tiếp phục bút
- **Phát hiện ranh giới cung** — Tự động nhận biết cung/quyển kết thúc, kích hoạt rà soát, sinh tóm tắt và mở rộng cung/quyển kế tiếp

#### Đường ống nén context

Khi hội thoại vượt cửa sổ context của model, nén lần lượt từ cấp tốn ít chi phí đến tốn nhiều:

```
ToolResultMicrocompact → LightTrim → StoreSummaryCompact → FullSummary
   dọn kết quả tool cũ    cắt văn bản dài   store nén không LLM    tóm tắt LLM dự phòng
```

- **StoreSummaryCompact** — Chuyên cho Writer, dùng tóm tắt chương, ảnh chụp nhân vật, sổ phục bút sẵn có trong store để thay thế trực tiếp tin nhắn cũ, không tốn chi phí LLM
- **FullSummary tùy biến cho tiểu thuyết** — Writer dùng prompt tóm tắt hướng tới mạch tự sự liền lạc, yêu cầu rõ ràng phải giữ trạng thái nhân vật, manh mối phục bút, các mục cần sửa của bản rà soát, neo phong cách
- **Gói khôi phục sau nén** — Sau FullSummary tự động tiêm kế hoạch chương hiện tại, dàn ý và ảnh chụp nhân vật, tránh Writer "mất trí nhớ" sau khi nén
- **Cầu chì (circuit breaker)** — Khi nén thất bại liên tiếp sẽ tự động bỏ qua và cảnh báo tường minh, dùng chế độ bán mở, vòng sau tự động thử lại
- **Ước lượng CJK Token** — Tiếng Trung dùng `runes × 1.5`, không bị đánh giá thấp do `bytes/4` mà khiến việc nén bị trễ
- **Sức khỏe TUI chuyển sắc** — Mức chiếm dụng context xanh (<70%) → vàng (70-85%) → đỏ (>85%) hiển thị theo thời gian thực

## Bắt đầu nhanh

```bash
# Cài đặt một lệnh (macOS / Linux, không cần Go)
curl -fsSL https://raw.githubusercontent.com/voocel/ainovel-cli/main/scripts/install.sh | sh

# Cài đặt phiên bản chỉ định
curl -fsSL https://raw.githubusercontent.com/voocel/ainovel-cli/main/scripts/install.sh | sh -s -- v1.2.3

# Hoặc cài qua Go
go install github.com/voocel/ainovel-cli/cmd/ainovel-cli@latest

# Xem phiên bản / cập nhật lên phiên bản mới nhất
ainovel-cli --version
ainovel-cli update

# Chạy lần đầu, tự động vào luồng hướng dẫn (chọn Provider → nhập API Key → Base URL → tên model)
ainovel-cli
```

> Windows hoặc cài thủ công: vào [Releases](https://github.com/voocel/ainovel-cli/releases/latest) tải gói tương ứng với nền tảng.

### Docker

Image Docker phù hợp chạy tác vụ dài headless trên server/NAS, cũng có thể dùng `-it` để vào TUI. Khuyến nghị mount thư mục config và thư mục tác phẩm ra máy chủ:

```bash
mkdir -p config workspace

# TUI
docker run --rm -it \
  -v "$PWD/config:/root/.ainovel" \
  -v "$PWD/workspace:/workspace" \
  ghcr.io/voocel/ainovel-cli:latest

# Headless
docker run --rm \
  -v "$PWD/config:/root/.ainovel" \
  -v "$PWD/workspace:/workspace" \
  ghcr.io/voocel/ainovel-cli:latest \
  --headless --prompt "Viết một bộ tiểu thuyết huyền huyễn phương Đông dài, nhân vật chính khởi đầu từ một tiểu thành biên thùy"
```

Cũng có thể dùng Compose:

```bash
docker compose run --rm ainovel
docker compose run --rm ainovel --headless --prompt "Viết một truyện ngắn trinh thám"
```

Sau khi vào TUI, ở giai đoạn khởi động hỗ trợ hai kiểu tương tác mở đầu:

- `Bắt đầu nhanh`: một câu trực tiếp vào sáng tác
- `Đồng sáng tác quy hoạch`: đối thoại nhiều vòng với AI để làm rõ yêu cầu, **bên phải đồng bộ thời gian thực bản nháp chỉ thị sáng tác đang được sắp xếp**; mỗi vòng AI chủ động đưa ra 1-3 gợi ý dẫn dắt, nhấn phím số là điền ngay vào ô nhập, nhấn `Ctrl+S` để vào sáng tác chính thức

Cả hai chế độ cuối cùng đều hội tụ về cùng một bản chỉ thị sáng tác, rồi vào cùng một bộ engine sáng tác.

### Quản lý nhiều cuốn tiểu thuyết

Mỗi cuốn tiểu thuyết gắn với thư mục khởi động, sản phẩm đặt tại `{cwd}/output/novel/`. Đổi thư mục khởi động = đổi một cuốn, `cd` về lại rồi khởi động = tự động khôi phục từ checkpoint gần nhất. Config `~/.ainovel/config.json` chia sẻ toàn cục, không cần sao chép.

### File cấu hình

Khi chạy lần đầu sẽ tự động hướng dẫn sinh file cấu hình `~/.ainovel/config.json`, sau đó có thể chỉnh sửa trực tiếp file này để điều chỉnh thiết lập. Xóa file cấu hình rồi chạy lại sẽ vào lại luồng hướng dẫn.

Cũng có thể tạo file cấu hình thủ công, tham khảo `~/.ainovel/config.example.jsonc` (tự động sinh khi hướng dẫn).

```jsonc
{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": {
    "openrouter": {
      "api_key": "sk-or-v1-xxx",
      "base_url": "https://openrouter.ai/api/v1",
      "models": ["google/gemini-2.5-flash", "google/gemini-2.5-pro"],
      "extra": {
        "user_agent": "my-client/1.0",
        "headers": { "X-Custom-Client": "my-client" }
      }
    }
  },
  "style": "default"
}
```

#### Thứ tự tìm file cấu hình (cái sau ghi đè cái trước)

1. `~/.ainovel/config.json` — Cấu hình toàn cục
2. `./.ainovel/config.json` — Ghi đè cấp dự án (tùy chọn)
3. `--config path/to/config.json` — Chỉ định qua dòng lệnh

> `.ainovel/` cấp dự án là bản sao gương của `~/.ainovel/` toàn cục: cùng cấu trúc, chỉ là thư mục gốc đổi từ thư mục home sang dự án hiện tại. Config đặt ở `./.ainovel/config.json`, quy tắc viết đặt ở `./.ainovel/rules/*.md` (xem mục "Khử mùi AI và quy tắc tùy chỉnh" bên dưới). Thư mục này chứa khóa bí mật, đã được thêm vào `.gitignore` mặc định.

Giải thích quy tắc ghi đè:

- Trường vô hướng theo cái sau ghi đè cái trước, ví dụ `provider`, `model`, `style`
- `providers` và `roles` hợp nhất theo key, mục cùng tên ghi đè nội bộ theo trường
- Trường không điền sẽ kế thừa cấu hình tầng trên, ví dụ cấu hình cấp dự án chỉ ghi `base_url` thì sẽ giữ `api_key` trong cấu hình toàn cục
- Hiện chưa hỗ trợ dùng chuỗi rỗng để xóa tường minh giá trị có sẵn của tầng trên; nếu cần xóa, hãy chỉnh trực tiếp file cấu hình ưu tiên cao hơn

> ⚠️ Giá trị của `provider` (và `roles.*.provider`) là **tên key** trong `providers` — một con trỏ, không phải tên giao thức. Nếu cấp dự án chuyển `provider` sang một tài khoản không tồn tại trong `providers` toàn cục, thì phải đồng thời bổ sung thông tin xác thực của tài khoản đó ở cấp dự án (`api_key` / `base_url`), nếu không khởi động sẽ báo "chưa cấu hình thông tin xác thực".

`providers.<name>.models` là trường tùy chọn, dùng để khai báo danh sách model được phép chuyển trong panel `/model` của TUI dưới provider đó; nếu chưa cấu hình, hệ thống sẽ lùi về các model của provider đó đã từng xuất hiện trong file cấu hình hiện tại.

`providers.<name>.extra` là cấu hình cấp provider, sẽ được truyền cho HTTP client bên dưới, phù hợp để cấu hình các trường nhận diện client như `user_agent`, `headers`, `anthropic_beta`; còn `providers.<name>.extra_body` mới là tham số mở rộng phần thân request, hai cái này không được dùng lẫn lộn.

## Báo cáo chẩn đoán

Trong TUI nhập `/diag` để chẩn đoán phân tích các sản phẩm output của tiểu thuyết hiện tại, đưa ra các phát hiện khả thi và đề xuất cải tiến.

Chẩn đoán bao phủ bốn chiều:

- **Quy trình** — Vòng lặp viết lại bị kẹt, chỉ thị steering chưa được tiêu thụ, trạng thái Phase/Flow bất thường, chương nhảy số
- **Chất lượng** — Chiều rà soát liên tục điểm thấp, tỷ lệ thực thi hợp đồng, tỷ lệ viết lại, số chữ chương bất thường
- **Quy hoạch** — Phục bút đình trệ, la bàn lỗi thời, dàn ý cạn kiệt, thiếu tóm tắt
- **Context** — Nhân vật biến mất, lỗ hổng dòng thời gian, dữ liệu quan hệ đình trệ

Mỗi phát hiện gồm: mô tả vấn đề, bằng chứng dữ liệu, đề xuất cải tiến (chỉ tới prompt/flow/config cụ thể).

`/diag` đồng thời cũng xuất ra một bản `meta/diag-export.md` **đã được khử nhạy** (loại bỏ phần chính tiểu thuyết, chỉ giữ khung hành vi như gọi tool, chuỗi lỗi, số lần lặp v.v.). Gặp vấn đề kiểu vòng lặp vô hạn / gián đoạn, dán nó vào GitHub issue là được, giúp người bảo trì định vị được vấn đề mà không cần dữ liệu cục bộ.

## Hồ sơ mô phỏng (mô phỏng viết)

Đặt bài tham khảo vào thư mục `simulate/` của thư mục khởi động hiện tại, rồi nhập `/simulate` trong TUI. Hệ thống sẽ đọc đệ quy các file `.txt`, `.md`, `.markdown`, dùng model architect để phân tích ngữ liệu và ghi vào:

```text
output/novel/meta/simulation_profile.json
```

Khi chạy `/simulate` lần nữa, sẽ bỏ qua các file không thay đổi theo `relative_path + sha256`; nếu không có nội dung mới thêm hoặc thay đổi, sẽ thông báo "hồ sơ đã là mới nhất" và sẽ không gọi LLM. Nếu đã có hồ sơ mà trong `simulate/` xuất hiện bài mới thêm hoặc đã sửa, hệ thống sẽ tiếp tục tổng hợp trên nền hồ sơ cũ.

Cũng có thể nhập hồ sơ đã sinh trước đó, tránh phân tích lặp cùng một lô bài:

```text
/simulate
/importsim ./profile.json
```

`/importsim` chỉ nhận JSON `simulation_profile.v1` do tính năng này sinh ra, và hợp nhất theo dấu vân tay ngữ liệu, nguồn trùng sẽ bị bỏ qua. Chỉ nhập file hồ sơ từ nguồn đáng tin cậy; nội dung nhập vào sẽ trở thành tham chiếu context cho các Agent về sau. Hồ sơ sẽ được tiêm vào `novel_context` ở dạng compact, Coordinator, Architect, Writer, Editor đều đọc được; mỗi Agent chỉ học hỏi cấu trúc, nhịp điệu, hook và thủ pháp thu hút độc giả, không sao chép cách diễn đạt nguyên văn hoặc thiết lập riêng có.

## Nhập (Import)

Trong TUI nhập `/import <đường dẫn file>` để suy ngược nhập một cuốn tiểu thuyết có sẵn: trước hết cắt theo chương, rồi dùng LLM suy ngược ra tiền đề / nhân vật / thế giới quan / dàn ý phân tầng / la bàn, ghi xuống đĩa từng chương. Nguyên văn được dựng thành quyển một có thể viết tiếp dạng đăng dài kỳ, sau khi nhập xong sẽ **tự động tiếp sức viết tiếp** — Coordinator rà soát/tóm tắt ở cuối quyển một, thêm quyển mới, viết tiếp từ chương kế tiếp.

```
/import ~/tieu-thuyet-cua-toi.txt              # Nhập từ đầu và suy ngược foundation
/import ~/tieu-thuyet-cua-toi.txt from=50      # Nhập tiếp từ chương 50 (bỏ qua suy ngược)
```

**Quy tắc cắt chương**: Tự động nhận biết các định dạng tiêu đề này (đầu dòng, có thể kèm tiền tố Markdown `#`/`##`, bọc bởi `【】`/`〖〗`, dấu cách full-width, tương thích encoding GBK/BOM):

- Đánh số tiếng Trung: `第一章` `第3回` `第十话` `第二卷` `第五节` `第二幕`, `卷一` độc lập, số hỗ trợ chữ hoa (`第壹章`), có thể kèm phụ đề (`第三章：决战`)
- Đơn vị đặc biệt tiếng Trung: `序章` `楔子` `引子` `前言` `尾声` `终章` `后记` `番外` `外传`
- Tiếng Anh: `Chapter 1` `Chapter II`, `Prologue` `Epilogue`, có thể kèm phụ đề (`Chapter 1: The Beginning`)

Nếu báo **"không nhận diện được chương nào"**, hãy xác nhận file đúng là văn bản tiểu thuyết chia chương (tiêu đề chương chiếm riêng một dòng, nằm ở đầu dòng).

> Import là phát lại xác định (deterministic replay), không qua Coordinator; nguyên văn được ghi xuống đĩa từng chữ thành các chương đã hoàn thành, do đó phù hợp để "viết tiếp cùng một cuốn sách". Nếu chỉ muốn học hỏi thiết lập để sáng tác hoàn toàn mới, hãy khởi tạo một cuốn sách mới theo cách thông thường và mô tả phong cách thiết lập mong muốn trong yêu cầu.

## Xuất (Export)

Trong TUI nhập `/export` để hợp nhất xuất các chương đã hoàn thành, mặc định TXT, ghi vào `{novelDir}/{NovelName}.txt`. Export là thao tác chỉ đọc, giữa chừng viết cũng có thể lấy "thành phẩm hiện giai đoạn" bất cứ lúc nào, không ảnh hưởng Coordinator đang chạy.

Định dạng do **hậu tố đường dẫn xuất** quyết định (`.txt` / `.epub`):

```text
/export                            # Mặc định TXT, {novelDir}/{NovelName}.txt
/export ~/quang-ban.txt            # Hậu tố .txt → TXT
/export ~/quang-ban.epub           # Hậu tố .epub → EPUB (Apple Books / WeChat Read / bộ chuyển Kindle đọc được)
/export from=10 to=30 --overwrite  # Khoảng chương + ghi đè
/export from=10 ~/x.epub --overwrite
```

- **TXT** — `《Tên sách》` → phân tách quyển → phần chính chương (chế độ phân tầng truyện dài tự động thêm phân tách quyển). Hai loại dữ liệu nội bộ **không vào bản xuất**: premise (bản thiết kế sáng tác, chứa thông tin hậu trường như độc giả mục tiêu / vùng cấm viết v.v., viết cho tác giả và engine xem), phân tách cung (dưới góc nhìn độc giả thì cung là cấu trúc nội bộ quá chi tiết). Bộ xuất thống nhất sinh "Chương N Tiêu đề", tiêu đề trùng lặp do writer tự kèm trong phần chính (`# 第N章…` hoặc `# tên chương`) sẽ bị bóc đi.
- **EPUB** — Container chuẩn EPUB 3, gồm trang bìa, mục lục, XHTML tách theo chương, định danh được phái sinh ổn định dựa trên nội dung (xuất lại cùng một cuốn thì trình đọc nhận là phiên bản cập nhật). Không kèm ảnh bìa.

Các chương chưa hoàn thành trong khoảng sẽ bị bỏ qua và hiển thị trong kết quả, không tính là lỗi.

#### Dùng model khác nhau theo vai trò

Qua trường `roles` để gán model khác nhau cho từng tác tử, vai trò chưa cấu hình thì dùng model mặc định:

```jsonc
{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": {
    "openrouter": { "api_key": "sk-or-v1-xxx", "base_url": "https://openrouter.ai/api/v1" },
    "anthropic": { "api_key": "sk-ant-xxx" }
  },
  "roles": {
    "writer": { "provider": "anthropic", "model": "claude-sonnet-4" },
    "architect": { "provider": "openrouter", "model": "google/gemini-2.5-pro" }
  }
}
```

Các vai trò có thể cấu hình: `coordinator` / `architect` / `writer` / `editor`

#### Proxy tùy chỉnh

Sau khi chọn bất kỳ Provider nào chỉ cần điền địa chỉ proxy, hoặc dùng Custom Proxy và chỉ định loại giao thức API. `api_key` của proxy tùy chỉnh là tùy chọn; nếu proxy của bạn không cần xác thực, có thể bỏ qua:

```jsonc
{
  "provider": "my-proxy",
  "model": "gpt-4o",
  "providers": {
    "my-proxy": {
      "type": "openai",
      "base_url": "https://proxy.example.com/v1",
      "extra": {
        "user_agent": "my-client/1.0",
        "headers": { "X-Custom-Client": "my-client" }
      }
    }
  }
}
```

Các Provider được hỗ trợ: `openrouter` / `anthropic` / `gemini` / `openai` / `deepseek` / `qwen` / `glm` / `grok` / `ollama` / `bedrock` và bất kỳ proxy tùy chỉnh nào.

Nếu proxy dùng giao thức Anthropic và yêu cầu trường nhận diện client, `type` nên đặt là `anthropic`, `anthropic_beta` đặt ở tầng đỉnh của `extra`, các HTTP header như Stainless đặt trong `extra.headers`:

```jsonc
{
  "provider": "claude-proxy",
  "model": "claude-sonnet-4-6",
  "providers": {
    "claude-proxy": {
      "type": "anthropic",
      "api_key": "sk-xxx",
      "base_url": "https://proxy.example.com",
      "extra": {
        "user_agent": "claude-code/2.1.183",
        "anthropic_beta": "claude-code-20250219",
        "headers": {
          "X-Stainless-Lang": "js",
          "X-Stainless-Package-Version": "0.94.0",
          "X-Stainless-Runtime": "node"
        }
      }
    }
  }
}
```

Về `api_key`:

- Các giao diện được lưu trữ như `openrouter` / `anthropic` / `gemini` / `openai` / `deepseek` / `qwen` / `glm` / `grok` thường cần điền `api_key`
- `ollama` và `bedrock` cho phép không điền `api_key`; Bedrock cần cấu hình `region`, `access_key_id`, `secret_access_key` trong `extra` (tùy chọn `session_token`)
- Proxy tùy chỉnh đã chỉ định tường minh `type` được phép không điền `api_key`

Ví dụ cấu hình `ollama` cục bộ:

```jsonc
{
  "provider": "ollama",
  "model": "qwen3:latest",
  "providers": {
    "ollama": {
      "base_url": "http://localhost:11434/v1"
    }
  }
}
```

### Phong cách viết

Chuyển đổi qua trường `style` của file cấu hình:

- `default` — Phong cách thông dụng
- `suspense` — Trinh thám hồi hộp
- `fantasy` — Kỳ ảo tiên hiệp
- `romance` — Ngôn tình

### Khử mùi AI và quy tắc tùy chỉnh

Tích hợp sẵn một baseline khử mùi AI (dưới `assets/`, mặc định xuất xưởng): danh sách đen máy móc `rules/default.md` (câu sáo / từ ngữ nhàm chán, kiểm tra xác định khi commit) + tiêu chí ngữ nghĩa `references/anti-ai-tone.md` (tiêm vào writer / editor để né tránh và dẫn chứng).

Muốn xếp chồng sở thích riêng của bạn **không cần sửa source code**: trong thư mục `~/.ainovel/rules/` (toàn cục, đặt `.md` tùy ý, hợp nhất theo thứ tự từ điển tên file) hoặc thư mục `./.ainovel/rules/` (theo cuốn sách, cũng đặt `.md` tùy ý, cùng hình thái với toàn cục), **chỉ cần viết sở thích bằng lời lẽ bình dân** (như "đừng viết nhân vật chính thành thánh mẫu", "dùng nhiều cảm nhận cơ thể"), editor sẽ rà soát theo ngữ nghĩa — không định dạng, không YAML. Muốn các kiểm tra xác định cứng như "số chữ / từ cấm" thì **tùy chọn** thêm một đoạn front matter ở đầu file. Ghi đè theo độ gần, xếp chồng có hiệu lực với baseline tích hợp; đầy đủ các trường xem [`rules.md.example`](rules.md.example).

## Cấu trúc output

Toàn bộ dữ liệu sáng tác (chương, dàn ý, nhân vật, tiến độ v.v.) được lưu trong thư mục output. Sau khi gián đoạn, chạy lại sẽ tự động viết tiếp từ tiến độ lần trước. Xóa thư mục output sẽ bắt đầu sáng tác lại từ đầu.

```
output/{novel_name}/
├── chapters/           # Bản chốt (Markdown)
│   ├── 01.md
│   └── ...
├── summaries/          # Tóm tắt chương (JSON)
├── drafts/             # Bản nháp chương
├── reviews/            # Báo cáo rà soát
├── meta/
│   ├── premise.md      # Tiền đề câu chuyện
│   ├── outline.json    # Dàn ý chương phẳng (chỉ gồm các chương đã mở rộng)
│   ├── layered_outline.json # Dàn ý phân tầng (quyển hiện tại + quyển xem trước, chế độ truyện dài)
│   ├── compass.json   # La bàn hướng kết cục (chế độ truyện dài)
│   ├── characters.json # Hồ sơ nhân vật
│   ├── world_rules.json# Quy tắc thế giới
│   ├── progress.json   # Trạng thái tiến độ
│   ├── timeline.json   # Dòng thời gian
│   ├── foreshadow.json # Sổ phục bút
│   ├── state_changes.json # Bản ghi biến đổi trạng thái nhân vật
│   ├── style_rules.json# Quy tắc phong cách viết (chắt lọc tại ranh giới cung)
│   ├── snapshots/      # Ảnh chụp trạng thái nhân vật (truyện dài)
│   ├── checkpoints.jsonl # Checkpoint cấp Step (thêm vào sau mỗi tool thành công)
│   ├── characters.md   # Hồ sơ nhân vật (bản dễ đọc)
│   └── world_rules.md  # Quy tắc thế giới (bản dễ đọc)
```

## Khôi phục checkpoint

Viết một bộ tiểu thuyết dài có thể mất nhiều giờ thậm chí nhiều ngày, giữa chừng sập, mất mạng, Ctrl+C đều là chuyện thường gặp. Hệ thống **tự động khôi phục khi chạy lại trong cùng thư mục**, không cần thao tác thủ công.

### Các tình huống khôi phục

| Thời điểm gián đoạn | Hành vi khôi phục |
|---|---|
| Giai đoạn quy hoạch (đang xây thế giới quan/dàn ý) | Kiểm tra thiết lập đã lưu, tự động bù mục thiếu |
| Một chương đang viết (có nháp chưa commit) | Viết tiếp từ chương đó, đọc nháp có sẵn để tiếp tục |
| Đang rà soát | Kích hoạt lại Editor rà soát |
| Hàng đợi viết lại/đánh bóng chưa được dọn sạch | Tiếp tục xử lý các chương chờ viết lại |
| Mở rộng cung/quyển bị gián đoạn (rà soát xong nhưng cung kế chưa mở rộng) | Tự động phát hiện cung/quyển khung, kích hoạt Architect mở rộng |
| Can thiệp người dùng chưa hoàn thành | Tiêm lại chỉ thị can thiệp lần trước |
| Gián đoạn khi viết bình thường | Viết tiếp từ chương kế tiếp |

### Nguyên lý hoạt động

Toàn bộ sản phẩm sáng tác được lưu bền vững trong thư mục `output/`. Sau khi mỗi tool thực thi thành công sẽ ghi checkpoint (`meta/checkpoints.jsonl`). Khi khởi động lại:

1. Đọc `progress.json` + checkpoint gần nhất + tín hiệu chờ xử lý
2. Sinh chỉ thị khôi phục chính xác đến cấp step (như "chương 7 draft đã ghi xuống đĩa, hãy tiếp tục check_consistency")
3. Một lần `Prompt` khởi động Coordinator, vào vòng lặp dài tiếp tục sáng tác

> Việc ghi file dùng thao tác nguyên tử temp + fsync + rename, ngay cả khi mất điện giữa lúc ghi cũng không làm hỏng dữ liệu có sẵn.

## Can thiệp thời gian thực (Steer)

Trong quá trình sáng tác có thể tiêm ý kiến chỉnh sửa qua ô nhập bất cứ lúc nào, **không cần tạm dừng hay khởi động lại**.

### Chế độ TUI

Sau khi sáng tác khởi động, ô nhập dưới cùng tự động chuyển sang chế độ can thiệp:

```
❯ Đưa tuyến tình cảm lên sớm tới chương 4, tăng các màn đối đầu của nam nữ chính
```

Sau khi nhập nhấn Enter, hệ thống tự động:
1. Ghi chỉ thị can thiệp vào `run.json` (dùng để khôi phục sau sự cố)
2. Tiêm vào Coordinator đang chạy
3. Coordinator đánh giá phạm vi ảnh hưởng, quyết định sửa thiết lập, viết lại chương đã có, hay điều chỉnh ở các chương về sau

### Ví dụ can thiệp

| Chỉ thị can thiệp | Phản hồi có thể của hệ thống |
|---|---|
| "Đổi nhân vật chính thành nữ" | Sửa thiết lập nhân vật, đánh giá các chương đã viết có cần viết lại không |
| "Đưa tuyến tình cảm lên sớm tới chương 4" | Điều chỉnh dàn ý, có thể viết lại chương 4 và về sau |
| "Thêm một nhân vật phản diện" | Cập nhật hồ sơ nhân vật và quy tắc thế giới, giới thiệu ở các chương về sau |
| "Nhịp quá chậm, đẩy nhanh tiến độ" | Điều chỉnh mật độ dàn ý các chương về sau |

## Triết lý thiết kế

> **Dời độ phức tạp từ code vào model.** Code càng ít, chỗ có thể hỏng càng ít. Trao quyền quyết định cho vai trò giỏi ra quyết định hơn.

### LLM dẫn dắt, càng đơn giản càng ổn định

- **Quyền quyết định thuộc về LLM** — Mọi quyết định quy trình đều do Coordinator tự chủ phán đoán, Host không xen vào. Khi tool thất bại sẽ trả về lỗi có cấu trúc, LLM tự quyết định thử lại hay điều chỉnh chiến lược
- **Tool chỉ trả sự thật** — IO nguyên tử + ghi checkpoint, giá trị trả về là các trường sự thật JSON (`final_verdict` / `pending_rewrites` / `arc_end_reached`), không kèm bất kỳ chuỗi chỉ thị nào
- **Reminder dẫn dắt mỗi vòng** — Trước mỗi lần gọi LLM, Host đọc tầng sự thật, chạy generator hàm thuần để sinh `<system-reminder>` tiêm vào, chỉ thị không vào lịch sử bền vững, mỗi vòng tính lại từ sự thật
- **StopGuard gác cổng vật lý** — Khi `Phase ≠ Complete`, Coordinator về mặt vật lý không thể `end_turn`, chỉ khi chặn liên tiếp vượt ngưỡng mới leo thang chấm dứt
- **Từ chối orchestration phức tạp** — Không có task queue, không có scheduler, không có policy engine. Một lần Run của Coordinator chính là luồng điều khiển duy nhất
- **Model càng mạnh lợi ích càng lớn** — Kiến trúc giữ quyền quyết định trong prompt và ngữ nghĩa tool, model nâng cấp xong là ăn lợi ích ngay, Host không phải sửa một dòng

### Vòng kín hoàn toàn tự động

Một câu nhập vào, tiểu thuyết hoàn chỉnh xuất ra:

```
"Viết một bộ tiểu thuyết trinh thám" → xây thế giới quan → thiết kế nhân vật → quy hoạch dàn ý
                → viết từng chương → rà soát chất lượng → tự động viết lại
                → tóm tắt cấp cung → ảnh chụp nhân vật → thành sách hoàn chỉnh
```

- **Coordinator tự chủ điều phối** — Trong một vòng lặp dài đọc tầng sự thật + Reminder để quyết định bước tiếp theo, không cần Host can thiệp
- **Writer tự chủ sáng tác** — Mỗi chương độc lập hoàn thành vòng kín plan → draft → check → commit
- **Editor tự chủ rà soát** — Phân tích vấn đề cấu trúc xuyên chương, xuất phán quyết và phạm vi ảnh hưởng
- **Architect tự chủ xây dựng** — Từ một câu yêu cầu suy ra thiết lập hoàn chỉnh, tự chủ mở rộng quy hoạch về sau tại ranh giới cung/quyển
- **Tự động quản lý phục bút** — Cài cắm, đẩy tiến, thu hồi đều do Agent tự theo dõi toàn trình
- **Tự động điều tiết nhịp điệu** — Theo dõi lịch sử tuyến tự sự và loại hook, tránh các chương liên tiếp giống nhau về cấu trúc

### Tách rời sự thật và chỉ thị

Tool chỉ trả sự thật, chỉ thị do Reminder tính lại từ tầng sự thật mỗi vòng:

- `commit_chapter` / `save_review` trả về sự thật có cấu trúc (`final_verdict` / `pending_rewrites` / `arc_end_reached` / `next_chapter`), không kèm bất kỳ chuỗi `[hệ thống]` nào
- Generator hàm thuần dưới `internal/host/reminder/` đọc `Progress` + `Outline`, mỗi vòng pre-turn sinh `<system-reminder>`: `flow` (hiện nên làm gì / phanh cuối cung) / `queue_guard` (hàng đợi chưa dọn thì cấm chương mới) / `book_complete` (cả sách hoàn thành mới cho qua). Lớp bảo hiểm vật lý do `StopGuard` đảm nhận khi `phase≠Complete` từ chối `end_turn`
- Reminder chỉ sống một vòng, không vào lịch sử, không tham gia nén; quy tắc có unit test, sự thoái hóa có thể bị regression bắt được

Như vậy chỉ thị sẽ không bị nuốt mất bởi gọi dây chuyền, cũng không trôi dạt trong sản phẩm của tool. Sửa bug chỉ cần thêm một generator + một test.

## Công nghệ sử dụng

- **Go 1.25** — Ngôn ngữ chính
- **[agentcore](https://github.com/voocel/agentcore)** — Kernel Agent cực gọn (tool-calling + streaming)
- **[litellm](https://github.com/voocel/litellm)** — Adapter giao diện LLM hợp nhất
- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** — Framework TUI terminal

## License

MIT

Dự án này tích cực tham gia và công nhận [cộng đồng linux.do](https://linux.do/).
