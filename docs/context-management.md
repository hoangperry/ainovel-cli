# Thuyết minh quản lý context

> [English](context-management.en.md)

Tài liệu này mô tả hệ thống quản lý context hiện tại của `ainovel-cli`, bao gồm:

- Vì sao cần quản lý context
- Context đến từ đâu
- Lúc chạy thì nén, khôi phục, bàn giao như thế nào
- Giá trị, điều kiện kích hoạt và bối cảnh áp dụng của từng chiến lược
- Khi có vấn đề thì nên xem chỗ nào trước

Mục tiêu không phải giới thiệu khái niệm trừu tượng, mà để người bảo trì về sau chỉ cần mở tài liệu này ra là nhanh chóng hiểu được phần triển khai hiện tại và lối vào để gỡ lỗi.

## 1. Mục tiêu thiết kế

Quản lý context của dự án này không phải bối cảnh chat thông dụng, mà hướng tới bối cảnh sáng tác tiểu thuyết. Nó phải giải quyết đồng thời mấy loại vấn đề:

1. Hội thoại dài sẽ vượt quá context window của model.
2. Sáng tác tiểu thuyết cần giữ lại không phải "bản thân lịch sử chat", mà là ký ức tự sự có cấu trúc.
3. Sau khi nén, Writer không được đánh mất trạng thái nhân vật, phục bút (cài cắm), kế hoạch chương, ràng buộc văn phong, các mục cần sửa từ khâu rà soát.
4. Khi khôi phục việc viết, không được giả định model vẫn "nhớ những gì đã chat trước đó", mà phải ưu tiên dựa vào các artifact đã được lưu bền vững.

Vì vậy chúng tôi áp dụng một phương án "ký ức phân tầng":

- Ký ức ngắn hạn: phần đuôi các message vừa giữ lại gần nhất
- Ký ức trung hạn: `ContextSummary` sinh ra từ việc nén
- Ký ức dài hạn: các artifact có cấu trúc trong store của dự án
- Ký ức khôi phục: handoff / restore pack / novel_context

## 2. Kiến trúc tổng thể

### 2.1 Các tầng chính

Quản lý context hiện tại chia thành bốn tầng:

1. `agentcore/context`
   Phụ trách khung budget context thông dụng, pipeline chiến lược, khung nén/khôi phục.

2. `internal/tools/novel_context`
   Phụ trách lắp ráp dữ liệu có cấu trúc trong dự án tiểu thuyết thành context dùng được cho lượt hiện tại.

3. `internal/orchestrator/store_summary_*`
   Phụ trách nén nhanh dựa trên store, chuyên dụng cho Writer.

4. `internal/orchestrator/writer_restore.go`
   Phụ trách bổ sung một gói khôi phục sau khi nén nối tiếp sau `FullSummary`, đảm bảo Writer có thể viết tiếp.

### 2.2 Luồng dữ liệu

Lúc chạy chủ yếu có hai đường context:

1. Đường làm việc bình thường
   - Agent gọi `novel_context`
   - `novel_context` đọc từ store các dữ liệu như tóm tắt chương, kế hoạch, nhân vật, dòng thời gian
   - Những dữ liệu này đi vào prompt của lượt hiện tại

2. Đường context quá dài
   - `ContextManager` phát hiện áp lực token
   - Nén theo thứ tự chiến lược
   - Ưu tiên thử nén nhẹ và nén dựa trên store
   - Khi vẫn chưa đủ mới đi tới `FullSummary` của LLM
   - Sau `FullSummary` thì tiêm restore pack

## 3. Các file then chốt

### 3.1 Engine context thông dụng

- `../agentcore/context/strategy.go`
- `../agentcore/context/engine.go`
- `../agentcore/context/strategy_tool.go`
- `../agentcore/context/strategy_trim.go`
- `../agentcore/context/strategy_summary.go`
- `../agentcore/context/message.go`
- `../agentcore/context/summary_run.go`

Tác dụng:

- Định nghĩa `Strategy` / `ForceCompactionStrategy`
- Phụ trách thực thi chuỗi chiến lược dựa trên budget
- Phụ trách biểu diễn `ContextSummary` và chuyển đổi cho LLM
- Phụ trách nén tóm tắt LLM của `FullSummary`

### 3.2 Đấu nối phía dự án

- `internal/orchestrator/agents.go`

Tác dụng:

- Lắp ráp `ContextManager` cho Writer / Coordinator
- Tiêm thêm `StoreSummaryCompact` cho Writer
- Cấu hình prompt `FullSummary` tùy biến cho tiểu thuyết cho Writer
- Cấu hình `writerRestorePack` cho Writer

### 3.3 Nén và khôi phục phía dự án

- `internal/orchestrator/store_summary_strategy.go`
- `internal/orchestrator/store_summary_builder.go`
- `internal/orchestrator/writer_restore.go`

Tác dụng:

- Trước khi tóm tắt bằng LLM, ưu tiên dùng dữ liệu store để nén nhanh
- Thống nhất xây dựng context có cấu trúc cần thiết cho việc nén và khôi phục của Writer
- Sau `FullSummary` thì nối thêm một restore message thuần trong bộ nhớ

### 3.4 Lắp ráp context có cấu trúc

- `internal/tools/novel_context.go`
- `internal/tools/novel_context_builders.go`
- `internal/domain/runtime.go`

Tác dụng:

- Định nghĩa `ContextProfile` / `MemoryPolicy`
- Quyết định tải bao nhiêu tóm tắt chương, bao nhiêu dòng thời gian, có bật tóm tắt phân tầng hay không
- Lắp ráp ra từ store các thứ như chương, nhân vật, phục bút, dòng thời gian, kinh nghiệm rà soát

### 3.5 Bàn giao và khôi phục

- `internal/orchestrator/handoff_policy.go`
- `internal/orchestrator/recovery_engine.go`

Tác dụng:

- Trong các giai đoạn truyện dài / làm lại / rà soát thì ưu tiên dựa vào handoff
- Khi khôi phục thì ghép gói bàn giao có cấu trúc vào prompt

### 3.6 Khả năng quan sát (observability)

- `internal/orchestrator/run.go`
- `internal/orchestrator/runtime.go`
- `internal/entry/tui/panels.go`

Tác dụng:

- Ghi lại các sự kiện ghi đè context
- Xuất ra tên chiến lược, biến động token, lượng message được giữ lại
- Để TUI nhìn thấy được context hiện tại đang là `projected` hay `compacted`

## 4. ContextManager được lắp ráp như thế nào

Cả Writer lẫn Coordinator đều đi qua `newContextManager`, nhưng cấu hình khác nhau.

Các tham số then chốt của `contextManagerConfig` hiện tại:

- `ContextWindow`
  Tổng context window của model.

- `ReserveTokens`
  Token dành riêng cho phần xuất ra của model.

- `KeepRecentTokens`
  Budget cho phần đuôi message gần nhất cố gắng giữ lại khi nén.

- `ToolMicrocompact`
  Cấu hình vi nén (microcompact) cho kết quả tool.

- `ExtraStrategies`
  Các chiến lược nén bổ sung phía dự án. Hiện tại Writer dùng để gắn `StoreSummaryCompact`.

- `Summary`
  Cấu hình của `FullSummary`, bao gồm prompt tùy biến và post-summary hook.

Các giá trị cấu hình thực tế hiện tại:

| Tham số | Writer | Coordinator |
|------|--------|-------------|
| ReserveTokens | 16,384 | 32,000 |
| KeepRecentTokens | 20,000 | 30,000 |
| CommitOnProject | false | true |
| IdleThreshold | 5min | không có |
| ExtraStrategies | StoreSummaryCompact | không có |
| Prompt Summary tùy biến | bản tự sự tiểu thuyết | mặc định (bản trợ lý code) |

Ngưỡng kích hoạt nén = `ContextWindow - ReserveTokens`. Ví dụ khi window là 128K, Writer kích hoạt ở ~112K, Coordinator kích hoạt ở ~96K.

Thứ tự pipeline chiến lược của Writer hiện tại là:

1. `ToolResultMicrocompact`
2. `LightTrim`
3. `StoreSummaryCompact`
4. `FullSummary`

Thứ tự này có ý nghĩa rõ ràng:

- Trước tiên dùng cách rẻ nhất để dọn nhiễu từ tool
- Sau đó cắt bớt các khối văn bản quá dài
- Nếu dữ liệu store đủ thì làm thẳng nén có cấu trúc không cần LLM
- Cuối cùng mới lui về tóm tắt bằng LLM

## 5. Tác dụng của từng chiến lược

### 5.1 ToolResultMicrocompact

Vị trí triển khai:

- `../agentcore/context/strategy_tool.go`

Tác dụng:

- Dọn dẹp các `tool_result` lịch sử
- Thay kết quả tool cũ bằng văn bản giữ chỗ ngắn gọn

Giá trị:

- Nội dung tool trả về thường có dung lượng lớn, mật độ thông tin thấp
- Nhiều kết quả tool cũ chỉ là "nhiễu quá trình", không phải ký ức tiểu thuyết

Đặc điểm cấu hình của Writer hiện tại:

- Đã đặt `IdleThreshold = 5m`

Điều này nghĩa là:

- Nếu message assistant gần nhất đã nhàn rỗi vượt ngưỡng
- Sẽ giảm số lượng kết quả tool cũ giữ lại một cách quyết liệt hơn

Bối cảnh áp dụng:

- Nhiều lượt `novel_context`
- Sau nhiều lượt tool read / check / draft

### 5.2 LightTrim

Vị trí triển khai:

- `../agentcore/context/strategy_trim.go`

Tác dụng:

- Cắt bớt các khối văn bản rất dài
- Giữ lại phần đầu và phần đuôi, phần giữa thay bằng ký tự giữ chỗ

Giá trị:

- Giữ nguyên cấu trúc message
- Chi phí thấp
- Rất hợp để xử lý nguyên văn chương quá dài hoặc đoạn xuất ra lớn

Bối cảnh áp dụng:

- Một message quá dài, nhưng chưa cần làm summary cho cả đoạn lịch sử

### 5.3 StoreSummaryCompact

Vị trí triển khai:

- `internal/orchestrator/store_summary_strategy.go`
- `internal/orchestrator/store_summary_builder.go`

Tác dụng:

- Khi context của Writer quá dài
- Ưu tiên dùng ký ức có cấu trúc trong store đã lưu bền vững để thay thế message cũ
- Không gọi LLM

Nó không phải tóm tắt hội thoại, mà là "thay thế ký ức có cấu trúc".

Các dữ liệu cốt lõi hiện đang giữ lại bao gồm:

- Tiến độ hiện tại
- Tóm tắt chương gần nhất
- Kế hoạch chương hiện tại
- Dàn ý chương hiện tại
- Tóm tắt cung truyện hiện tại
- Tóm tắt quyển hiện tại
- Snapshot nhân vật
- Phục bút đang hoạt động
- Các vấn đề rà soát cần sửa
- Dòng thời gian gần nhất
- Quy tắc văn phong

Điều kiện kích hoạt:

- Chương hiện tại lớn hơn 1
- Trong store đã có đủ tóm tắt lịch sử
- Và chương hiện tại có ít nhất dữ liệu trạng thái làm việc
  - `chapter_plan` hoặc `current_outline`

Giá trị:

- Giảm số lần nén bằng LLM
- Tránh thông tin then chốt của tiểu thuyết bị trôi dạt khi tóm tắt
- Để ký ức dài hạn ưu tiên dựa vào sự thật đã lưu xuống ổ đĩa, chứ không phải lịch sử chat

Vì sao chỉ dùng cho Writer:

- Đây là chiến lược nghiệp vụ tiểu thuyết, không phải chiến lược khung thông dụng
- Mô hình context của Coordinator / Editor khác nhau
- Kiểm chứng trên Writer — nơi cần ký ức sáng tác liên tục nhất — trước là hợp lý nhất

### 5.4 FullSummary

Vị trí triển khai:

- `../agentcore/context/strategy_summary.go`
- `../agentcore/context/summary_run.go`

Tác dụng:

- Khi mấy tầng trên vẫn chưa đủ, dùng model sinh ra `ContextSummary`
- Giữ lại phần đuôi message gần nhất
- Biến context sớm hơn thành checkpoint có cấu trúc

Chỗ Writer khác với trợ lý code mặc định:

- Writer dùng summary prompt tùy biến
- Nội dung tóm tắt yêu cầu rõ ràng phải giữ lại:
  - Tiến độ hiện tại
  - Trạng thái tức thời của nhân vật
  - Phục bút và đầu mối đang hoạt động
  - Phản hồi rà soát và các vấn đề cần sửa
  - Văn phong và nhịp điệu
  - Quyết định then chốt
  - Bước tiếp theo
  - Context then chốt

Giá trị:

- Là chiến lược dự phòng cuối cùng
- Ngay cả khi dữ liệu store không đủ, vẫn có thể duy trì tính liên tục thông qua LLM

### 5.5 Bộ ngắt mạch (Circuit Breaker)

Vị trí triển khai:

- `../agentcore/context/engine.go`

Tác dụng:

- Khi nén thất bại liên tiếp đạt ngưỡng (mặc định 3 lần), bỏ qua việc nén ở lượt hiện tại
- Khi bỏ qua vẫn phát ra `RewriteEvent` (`Reason = "circuit_breaker"`)
- TUI sẽ hiển thị scope là "ngắt mạch bỏ qua"
- Áp dụng chế độ nửa mở (half-open): sau khi bỏ qua một lượt thì lần sau sẽ thử lại, thành công thì reset, lại thất bại thì lại bỏ qua

Vì sao cần:

- Tóm tắt LLM có thể thất bại liên tiếp vì lý do mạng, model từ chối, v.v.
- Nếu không có ngắt mạch, mỗi lượt Project đều thử và thất bại, lãng phí lệnh gọi API
- Trong phiên viết truyện dài, lãng phí này sẽ tích lũy

Gỡ lỗi:

- Nếu TUI liên tục hiển thị "ngắt mạch bỏ qua", nghĩa là đường tóm tắt LLM có vấn đề
- Kiểm tra các sự kiện ghi đè context có `reason=circuit_breaker` trong slog
- Ngắt mạch không ảnh hưởng `StoreSummaryCompact` (vì nó không gọi LLM)

### 5.6 Ước lượng token (nhận biết CJK)

Vị trí triển khai:

- `../agentcore/context/usage.go`

Tác dụng:

- Mọi việc kiểm soát budget, thời điểm kích hoạt nén đều dựa vào ước lượng token
- `estimateTextTokens` tự động phát hiện văn bản có chủ yếu là ký tự CJK hay không
- Văn bản chủ đạo CJK: `runes × 1.5`
- Văn bản chủ đạo ASCII: `bytes / 4`

Vì sao không dùng được `bytes/4` tiêu chuẩn:

- Một chữ Hán UTF-8 = 3 bytes
- `bytes/4` sẽ ước một chữ Hán thành 0.75 token, thực tế khoảng 1.5 token
- Ước thấp gấp 2 lần sẽ khiến việc kích hoạt nén bị trễ nghiêm trọng

Phạm vi ảnh hưởng:

- `EstimateTokens` (một message)
- `EstimateTotal` (danh sách message)
- `EstimateContextTokens` (ước lượng hỗn hợp: Usage do LLM báo lên + ước lượng message ở phần đuôi)
- Việc cắt budget trong `store_summary_builder.go`

Lưu ý: args của ToolCall là JSON (chủ đạo ASCII), vẫn dùng `bytes/4`, không chịu điều chỉnh CJK.

## 6. Vì sao Writer có hai bộ "ký ức sau khi nén"

Writer hiện tại có hai đường nhìn thì gần giống nhau, nhưng chức trách khác nhau:

### 6.1 StoreSummaryCompact

Chức trách:

- Thay thế trực tiếp message cũ trong quá trình nén

Đặc điểm:

- Xảy ra trước `FullSummary`
- Không LLM
- Dùng store thay thế lịch sử sớm hơn

### 6.2 writerRestorePack

Vị trí triển khai:

- `internal/orchestrator/writer_restore.go`

Chức trách:

- Sau `FullSummary` thì nối thêm một restore message

Đặc điểm:

- Xảy ra sau khi nén bằng LLM
- Được tiêm thông qua `PostSummaryHook`
- Dùng để bổ sung những thông tin có cấu trúc mà Writer bắt buộc phải thấy khi khôi phục để viết tiếp

Vì sao cần cả hai:

- `StoreSummaryCompact` không phải lúc nào cũng trúng
  - Ví dụ chương đầu hoặc khi dữ liệu store không đủ
- `FullSummary` dù làm tốt đến mấy cũng có thể bỏ sót thông tin chính xác trong store
- Nên restore pack đóng vai trò lớp bảo hiểm cuối cùng

Hiện tại hai thứ này đã dùng chung `store_summary_builder.go`, tránh trôi dạt tiêu chí.

## 7. Tác dụng của novel_context

Vị trí triển khai:

- `internal/tools/novel_context.go`
- `internal/tools/novel_context_builders.go`

`novel_context` không phải chiến lược nén, nó là "bộ lắp ráp context có cấu trúc" lúc chạy.

Nó chia dữ liệu trong store thành mấy loại:

- `working_memory`
  - Kế hoạch chương hiện tại
  - Dàn ý chương hiện tại
  - Tóm tắt chương gần nhất
  - Dòng thời gian
  - checkpoint
  - previous tail

- `episodic_memory`
  - Trạng thái nhân vật
  - Trạng thái quan hệ
  - Thay đổi trạng thái gần nhất
  - Phục bút

- `reference_pack`
  - Dữ liệu thiết lập và tham chiếu ổn định hơn

- `selected_memory`
  - Một ít ký ức quan trọng được chọn ra theo tác vụ hiện tại

Giá trị:

- Nó quyết định context tiểu thuyết có cấu trúc thực sự "đút cho model" ở mỗi lượt
- `StoreSummaryCompact` không phải gọi chính nó, nhưng dùng chung cùng loại nguồn dữ liệu và tư duy lắp ráp với nó

## 8. ContextProfile và MemoryPolicy

Vị trí triển khai:

- `internal/domain/runtime.go`

### 8.1 ContextProfile

Tác dụng:

- Quyết định kích thước cửa sổ tải theo tổng số chương

Quy tắc hiện tại:

- `<= 15` chương
  - `10` tóm tắt chương gần nhất
  - `10` dòng thời gian chương gần nhất

- `<= 50` chương
  - `5` tóm tắt chương gần nhất
  - `8` dòng thời gian chương gần nhất

- `> 50` chương
  - `3` tóm tắt chương gần nhất
  - `5` dòng thời gian chương gần nhất
  - Bật tóm tắt phân tầng

Giá trị:

- Kiểm soát quy mô context
- Tránh việc truyện dài nhét toàn bộ lịch sử vào prompt

### 8.2 MemoryPolicy

Tác dụng:

- Viết tường minh ra chiến lược sử dụng context hiện tại
- Cấp cho `novel_context` xuất ra
- Cấp cho logic handoff / reminder / chẩn đoán sử dụng

Các trường then chốt:

- `SummaryWindow`
- `TimelineWindow`
- `LayeredSummaries`
- `SummaryStrategy`
- `HandoffPreferred`
- `ReadOnlyThreshold`

Giá trị:

- Biến "hệ thống hiện tại nên dùng ký ức như thế nào" từ logic ngầm thành chiến lược tường minh lúc chạy

## 9. Tác dụng của handoff

Vị trí triển khai:

- `internal/orchestrator/handoff_policy.go`

Khi tác phẩm bước vào giai đoạn dài hơn, phức tạp hơn, phụ thuộc artifact có cấu trúc nhiều hơn, hệ thống sẽ thiên về handoff.

handoff pack sẽ ghi lại:

- Giai đoạn và flow hiện tại
- Vị trí chương tiếp theo
- Commit gần nhất
- Rà soát gần nhất
- Tóm tắt gần nhất
- Memory policy hiện tại
- Câu chỉ dẫn khôi phục

Giá trị:

- Khi khôi phục sau gián đoạn không phụ thuộc lịch sử chat
- Trong các bối cảnh làm lại, rà soát, truyện dài thì ưu tiên dựa vào artifact có cấu trúc

## 10. Khả năng quan sát và gỡ lỗi

### 10.1 Sự kiện ghi đè context

Vị trí triển khai:

- `internal/orchestrator/run.go`

Mỗi lần ghi đè context đều xuất ra thông qua `contextRewriteCallback`:

- `reason`
- `strategy`
- `committed`
- `tokens_before`
- `tokens_after`
- `messages_before`
- `messages_after`
- `compacted_count`
- `kept_count`
- `split_turn`
- `incremental`
- `summary_runes`
- `duration_ms`

Cái này sẽ đồng thời đi vào:

- `slog`
- hàng đợi runtime boundary
- sự kiện `COMPACT` của TUI

### 10.2 Trong TUI nhìn thấy được gì

TUI sẽ hiển thị:

- Token context hiện tại (kèm dải màu chuyển theo độ khỏe mạnh)
- context window
- scope context hiện tại (gồm cả "ngắt mạch bỏ qua")
- tên chiến lược lần cuối cùng hiện tại
- số lượng summary

Ý nghĩa màu sắc của phần trăm context (triển khai trong `internal/entry/tui/layout.go`):

| Màu | Điều kiện | Ý nghĩa |
|------|------|------|
| Xanh lá | < 70% | Dư dả, còn xa ngưỡng nén |
| Vàng | 70-85% | Gần ngưỡng nén |
| Đỏ | > 85% | Sắp hoặc đang nén |

Nhãn tiếng Việt của Scope:

| Scope | Hiển thị | Ý nghĩa |
|-------|------|------|
| baseline | đường nền | Trạng thái bình thường |
| projected | đầu chiếu | Xem trước nén tạm thời |
| compacted | đã commit | Nén đã có hiệu lực |
| recovered | khôi phục | Khôi phục sau khi tràn |
| skipped | ngắt mạch bỏ qua | Nén bị bộ ngắt mạch bỏ qua |

Giá trị:

- Có thể nhanh chóng đánh giá độ khỏe mạnh của context hiện tại
- Khi vàng/đỏ có thể dự đoán nén sắp xảy ra
- Thấy "ngắt mạch bỏ qua" nghĩa là đường tóm tắt LLM có vấn đề

### 10.3 Có vấn đề thì xem chỗ nào trước

#### Tình huống 1: Writer nén xong mất kế hoạch chương

Xem trước:

- `novel_context` có tiêm ổn định `chapter_plan` hay không
- `store_summary_builder.go` có lấy được `chapterPlan` hay không
- `writerRestorePack` có được làm mới hay không

File trọng tâm:

- `internal/tools/novel_context_builders.go`
- `internal/orchestrator/store_summary_builder.go`
- `internal/orchestrator/session.go`

#### Tình huống 2: Nén xong mất trạng thái nhân vật / phục bút

Xem trước:

- `LoadLatestSnapshots`
- `LoadActiveForeshadow`
- `store_summary_builder.go`
- Writer summary prompt có bị ghi đè hay không

#### Tình huống 3: Nén thường xuyên nhưng luôn không trúng store_summary

Xem trước:

- Chương hiện tại có phải `<= 1` hay không
- Đã có recent summaries / arc / volume summary hay chưa
- Có tồn tại `chapter_plan` hoặc `current_outline` hay không
- `writer.Context.Strategy` cuối cùng ghi lại có phải `full_summary` hay không

#### Tình huống 4: Sau khôi phục context không đủ

Xem trước:

- handoff có được sinh ra hay không
- restore pack có được làm mới hay không
- recovery prompt có tiêm handoff hay không

#### Tình huống 5: Kết quả tool quá nhiều khiến context phình to

Xem trước:

- `ToolResultMicrocompact` có trúng hay không
- `IdleThreshold` có hiệu lực hay không

## 11. Đánh đổi của phần triển khai hiện tại

### Các hướng đã kiên định rõ ràng

1. Không nhét logic nghiệp vụ tiểu thuyết vào `agentcore`
2. Ưu tiên dựa vào store có cấu trúc, chứ không phải lịch sử chat
3. Writer dùng prompt tóm tắt chuyên dụng cho tiểu thuyết
4. Nén và khôi phục cố gắng dùng chung builder, tránh trôi dạt tiêu chí

### Các giới hạn hiện vẫn cố ý giữ lại

1. `StoreSummaryCompact` chỉ dùng cho Writer
2. Chương đầu sẽ không trúng store-based compact
3. Khi dữ liệu store không đủ vẫn lui về `FullSummary`
4. `writerRestorePack` là bù đắp kiểu nối thêm, không thay thế `FullSummary`

Những giới hạn này không phải khiếm khuyết, mà là ranh giới đặt ra ở giai đoạn hiện tại để kiểm soát độ phức tạp.

## 12. Tóm tắt một câu

Quản lý context của dự án này không đơn giản là "nén hội thoại dài thành ngắn", mà là:

`Ưu tiên dùng ký ức tiểu thuyết có cấu trúc để duy trì tính liên tục, chỉ khi cần thiết mới để LLM đi tóm tắt hội thoại; và trong cả ba khâu nén, khôi phục, bàn giao đều cố gắng dựa vào cùng một bộ artifact lưu bền vững.`

Nếu về sau bạn muốn sửa hệ thống này, hãy ưu tiên giữ vững ba điều dưới đây:

1. Đừng để ký ức then chốt của Writer lại một lần nữa chỉ phụ thuộc lịch sử chat.
2. Đừng để `store_summary` và `writer_restore` phân nhánh tiêu chí.
3. Khi xuất hiện vấn đề về tính liên tục, hãy kiểm tra trước xem artifact có cấu trúc đã đi vào context hay chưa, rồi mới quyết định có sửa prompt hay không.
