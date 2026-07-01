---
# Quy tắc mặc định tích hợp sẵn của dự án (Phase 1 - bản an toàn)
#
# Ở đây chỉ đặt các ràng buộc mặc định "kiểm tra cơ học được + ít gây tranh cãi".
# Các ưu tiên thẩm mỹ không cơ học hóa được (như khuynh hướng văn phong) hiện vẫn
# do writer.md / editor.md đảm nhiệm, chờ Phase 1.5 (sau khi F1 kiểm thử thủ công
# xác nhận sức ràng buộc của working_memory) mới quyết định có chuyển vào file này không.
#
# Người dùng có thể ghi đè các trường thông thường trong thư mục ./.ainovel/rules/ hoặc
# ~/.ainovel/rules/ (bất kỳ file .md nào bên dưới);
# fatigue_words hợp nhất theo từng từ, cùng một từ sẽ lấy ngưỡng từ nguồn gần hơn.
# Ngữ nghĩa chi tiết của các trường xem ở rules.md.example tại gốc dự án.

# Khoảng số chữ mỗi chương: lệch <20% cảnh báo; ≥20% lỗi.
# (Lưu ý: trường này bị tắt ở chế độ vi nên giữ nguyên dải số mang tính tham khảo.)
chapter_words: 3000-6000

# Danh sách đen cụm từ: xuất hiện ≥1 lần là error. Checker đối khớp chuỗi con theo
# mặt chữ, không có ký tự đại diện, nên chỉ đặt các "câu khuôn AI cố định độ dài"
# (ít gây tranh cãi); các mẫu có biến (như "không phải X mà là Y") đối khớp mặt chữ
# bắt không được, giao cho lớp ngữ nghĩa anti-ai-tone.md.
forbidden_phrases:
  - "ở một mức độ nào đó"
  - "đáng chú ý là"
  - "không hiểu vì sao"
  - "trăm mối cảm xúc đan xen"

# Ký tự cấm: tiếng Việt không có lớp ký tự cần chặn theo kiểu đặc thù chữ Hán,
# nên để trống; người dùng có thể tự cấu hình trong ./.ainovel/rules/ nếu cần.
forbidden_chars: []

# Giới hạn mềm cho từ lặp: commit_chapter sẽ đếm số lần xuất hiện mỗi chương,
# vượt ngưỡng báo warning. Đây là những từ thường bị lạm dụng trong truyện mạng/tiểu
# thuyết; anti-ai-tone.md có gợi ý ngữ nghĩa cùng hướng — tín hiệu hai nguồn nhất quán.
# Ngưỡng đặt rộng để dung thứ cho cách dùng bình thường.
fatigue_words:
  bỗng: 2
  khẽ: 2
  dường như: 2
  một cách: 3
  chợt: 2
  thoáng: 2
  phảng phất: 1
  bất giác: 1
  như thể: 2
  im lặng: 2
  không nói gì: 2
  một tia: 2
  một thoáng: 2
  hồi lâu: 2
---
