# Bản đồ nội dung assets

> [English](README.en.md)

Trước khi thêm "một đoạn văn / một tài liệu / một quy tắc" vào hệ thống, hãy tra bảng dưới để xác định nơi thuộc về, rồi xem cách đấu nối (wiring).

| Thư mục | Chứa gì | Ai tiêu thụ | Cách đấu nối |
|---|---|---|---|
| `prompts/` | system prompt của các vai trò thường trú (coordinator / writer / editor / architect×2) và prompt nhiệm vụ dùng một lần (import×2 / simulation×2) | `agents/build.go` lắp ráp; imp / sim runner | Trường Prompts trong `load.go`. Lưu ý: simulation_guidance được `load.go` tiêm vào lúc tải, không thấy trong file md |
| `references/` | Tài liệu kiến thức viết không phụ thuộc thể loại. Không vào system prompt, được novel_context cắt gọt theo vai trò / chương rồi tiêm vào `reference_pack` | writer / editor / architect | **Đấu nối ở ba chỗ**: thêm trường vào `tools.References` + `load.go` loadReferences đọc + `novel_context.go` writerReferences / architectReferences tiêm. Bỏ vào thư mục sẽ không tự động tải |
| `references/genres/<style>/` | Kiến thức chuyên biệt theo thể loại (style-references / arc-templates) | Như trên, tải khi `style != default` | `load.go` loadReferences |
| `rules/` | Giá trị mặc định của quy tắc cơ học (số chữ / từ cấm / từ gây mỏi), được code kiểm tra cưỡng chế khi commit | rules loader hợp nhất ba lớp: tích hợp sẵn → `~/.ainovel/rules/` → dự án `./.ainovel/rules/` | `rules/default.md`; định dạng lớp người dùng xem `rules.md.example` ở thư mục gốc. Chỉ đặt chuỗi cố định độ dài cố định, các mẫu có biến giao cho editor phán đoán ngữ nghĩa |
| `styles/<style>.md` | Chỉ thị phong cách viết theo thể loại | Ghép vào system prompt của **writer** (`agents/build.go`) | Tên file chính là giá trị `config.style`. Cùng với `references/genres/<style>/` là hai vật mang của cùng một khái niệm thể loại: cái trước là chỉ thị phong cách, cái sau là tài liệu kiến thức |

## Phán đoán nơi thuộc về của nội dung mới (năm câu hỏi)

1. Quy trình này bắt buộc phải được **bảo đảm**? → Không viết prompt, viết ràng buộc bằng code (StopAfterTools / tool guard / Flow Router)
2. Đây là tiêu chí phân xử (khi nào phái ai)? → `prompts/coordinator.md`
3. Đây là tiêu chuẩn thẩm mỹ / thực thi của một vai trò nào đó? → `prompts/<role>.md`
4. Đây là quy tắc có thể liệt kê cơ học (từ cấm / số chữ / ngưỡng)? → `rules/` (code cưỡng chế, chi phí LLM bằng không)
5. Đây là tài liệu kiến thức viết? → `references/` (nhớ đấu nối ba chỗ)

## Bảo đảm tính nhất quán

Đường dẫn envelope mà prompt tham chiếu (`working_memory.*` v.v.) và tài liệu tham số commit_chapter của writer.md
được `prompts_consistency_test.go` kiểm tra tự động — hai loại trôi dạt này không báo lỗi, chỉ khiến mô hình âm thầm kém đi, phải dựa vào đèn đỏ của test để phơi bày.
Đoạn quy trình trong prompt là "sổ tay người dùng", chân lý quy trình nằm ở tầng code; khi hai bên lệch nhau thì lấy code làm chuẩn rồi quay lại sửa prompt.
