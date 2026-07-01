# Mẫu lập kế hoạch dàn ý

Tác dụng của mẫu này không phải ép mọi tác phẩm vào một độ dài cố định, mà giúp trước tiên phán đoán cấp độ tác phẩm, rồi chọn độ chi tiết của dàn ý.

## Bước một: Trước tiên phán đoán cấp độ độ dài tác phẩm

### Truyện ngắn / truyện một quyển

- Phù hợp: một xung đột, một mục tiêu, ít nhân vật, kết cục tập trung
- Độ dài tham khảo: 8-25 chương
- Định dạng đề xuất: `outline` phẳng

### Truyện vừa / truyện nhiều giai đoạn

- Phù hợp: có leo thang theo giai đoạn, vài tuyến phụ, quan hệ nhân vật sẽ biến đổi
- Độ dài tham khảo: 25-60 chương
- Định dạng đề xuất: `outline` phẳng hoặc phân tầng nhẹ

### Truyện dài kỳ / truyện kiểu web

- Phù hợp: đề tài tự nhiên có không gian leo thang liên tục, sức căng quan hệ dài hạn, nhiều mục tiêu theo giai đoạn, thế giới mở rộng được, bí ẩn dài hạn hoặc tuyến trưởng thành dài hạn
- Độ dài tham khảo: 80-200+ chương
- Định dạng đề xuất: `layered_outline` phân tầng

## Bước hai: Phán đoán có bắt buộc dùng dàn ý phân tầng không

Chỉ cần thỏa bất kỳ 2 điều dưới đây, ưu tiên dùng `layered_outline`:

- Thế giới quan cần triển khai dần, không phải kể hết một lần
- Nhân vật chính trưởng thành không phải nhảy vọt một lần, mà leo thang nhiều giai đoạn
- Quan hệ nhân vật sẽ biến đổi liên tục qua nhiều giai đoạn
- Trung kỳ và hậu kỳ tồn tại các loại mâu thuẫn chính khác nhau
- Cần nhiều lần chuyển bản đồ/thế lực/thân phận/mục tiêu
- Đề tài rõ ràng giống tiểu thuyết thương mại dài kỳ hơn là truyện một quyển

## Bước ba: Khi viết truyện dài đừng làm thẳng "liệt kê chương cả truyện"

Thứ tự lập kế hoạch truyện dài đề xuất là:

1. Điểm bán và sự khác biệt của tác phẩm
2. Động cơ câu chuyện dài hạn
3. Chủ đề cấp quyển và leo thang
4. Mục tiêu cấp cung và bước ngoặt giai đoạn
5. Sự kiện cấp chương và hook

Cách làm sai:

- Trước viết tóm tắt 20 chương, rồi cố kéo dài
- Mỗi quyển đều lặp lại "gặp địch - mạnh lên - đổi bản đồ"
- Chỉ có tuyến chính leo thang, không có quan hệ leo thang
- Đầu truyện xài hết mọi bí mật lớn, trung hậu kỳ chỉ còn lặp lại công thức

## Mẫu dàn ý phẳng (truyện ngắn/vừa)

```json
[
  {
    "chapter": 1,
    "title": "Tiêu đề chương",
    "core_event": "Sự kiện cốt lõi của chương",
    "hook": "Hook cuối chương",
    "scenes": ["Cảnh 1", "Cảnh 2", "Cảnh 3"]
  }
]
```

## Mẫu dàn ý phân tầng (truyện dài - quyển-cung hai tầng triển khai cuộn)

Lập kế hoạch ban đầu dùng cuộn hai tầng: 2 quyển đầu có khung cung, các quyển còn lại là quyển khung; cung đầu tiên có chương chi tiết.

```json
[
  {
    "index": 1,
    "title": "Tiêu đề quyển một",
    "theme": "Mâu thuẫn/chủ đề cốt lõi mới của quyển này",
    "arcs": [
      {
        "index": 1,
        "title": "Cung một (đã triển khai)",
        "goal": "Mục tiêu cục bộ, lực cản và bước ngoặt",
        "chapters": [
          {"chapter": 1, "title": "Tiêu đề chương", "core_event": "Sự kiện cốt lõi", "hook": "Hook cuối chương", "scenes": ["Cảnh 1", "Cảnh 2"]}
        ]
      },
      {
        "index": 2,
        "title": "Cung hai (cung khung)",
        "goal": "Tóm tắt mục tiêu của cung này",
        "estimated_chapters": 12,
        "chapters": []
      }
    ]
  },
  {
    "index": 2,
    "title": "Tiêu đề quyển hai",
    "theme": "Chủ đề quyển hai",
    "arcs": [
      {"index": 1, "title": "Tiêu đề cung", "goal": "Mục tiêu cung", "estimated_chapters": 15, "chapters": []},
      {"index": 2, "title": "Tiêu đề cung", "goal": "Mục tiêu cung", "estimated_chapters": 10, "chapters": []}
    ]
  },
  {
    "index": 3,
    "title": "Tiêu đề quyển ba (quyển khung)",
    "theme": "Hướng chủ đề quyển ba",
    "estimated_chapters": 60,
    "arcs": []
  }
]
```

- Triển khai cấp cung: khi viết tiến tới cung khung, Architect triển khai chương chi tiết của cung đó
- Triển khai cấp quyển: khi viết tiến tới quyển khung, Architect triển khai cấu trúc cung của quyển đó + chương của cung đầu

## Danh sách kiểm tra cấp quyển cho truyện dài

Mỗi quyển đều phải trả lời:

- Quyển này thêm thông tin thế giới gì mới?
- Quyển này leo thang mâu thuẫn cốt lõi nào?
- Quyển này cho nhân vật chính được gì, và mất gì?
- Quyển này thay đổi quan hệ nhân vật chính thế nào?
- Sau khi quyển này kết thúc, vì sao câu chuyện buộc phải bước vào quyển sau?

## Danh sách kiểm tra cấp cung cho truyện dài

Mỗi cung đều phải trả lời:

- Mục tiêu rõ ràng của cung này là gì?
- Lực cản đến từ ai, quy tắc gì, cái giá gì?
- Bước ngoặt là gì?
- Sau khi cung này kết thúc, trạng thái nào biến đổi không thể đảo ngược?

## Danh sách kiểm tra cấp chương

- Mỗi chương phải phục vụ mục tiêu của cung mà nó thuộc về
- Mỗi chương phải gồm một sự kiện đẩy tới không thể xóa
- Hook phải đa dạng, đừng dựa hết vào một mẫu "phát hiện bí mật"
- Chương đầu truyện không thể chỉ "giới thiệu thế giới", phải đồng thời đẩy nhân vật và xung đột
