# Lộ trình

Mã nguồn hiện tại gồm N1–N4: tích hợp host, PostgreSQL, thay đổi bền vững, cấu hình host Android và cấu hình ứng dụng có kiểu. Luồng app mẫu có bằng chứng trên một POCO Pad X1; kiểm tra vật lý `HostOptions` vẫn còn mở. N5–N6 có triển khai phần mềm và kiểm tra tự động; kiểm định thiết bị thật vẫn mở. N7 là phần việc tiếp theo. Xem [kiểm định thiết bị hiện tại](compatibility.html).

| Cột mốc | Kết quả dự kiến |
| --- | --- |
| N1 | API đọc theo phạm vi, liên kết đăng ký và hợp đồng tích hợp/vòng đời ứng dụng chủ. |
| N2 | PostgreSQL tùy chọn, kiểm tra hợp đồng adapter và khôi phục khi nhiều instance cùng chạy. |
| N3 | Luồng thay đổi lưu cùng transaction, có thể đọc lại và đồng bộ dữ liệu ứng dụng chủ tin cậy. |
| N4 | Cấu hình Android sớm và cấu hình ứng dụng được quản lý với kiểu dữ liệu rõ ràng; app mẫu đã nhận, áp dụng và khôi phục cấu hình trên một bản phát triển chạy trên POCO. Kiểm tra phần cứng riêng cho `HostOptions` vẫn còn mở. |
| N5 | Phần mềm có nhóm theo scope, đích cố định, gán hàng loạt có checkpoint, lịch/hủy triển khai và lệnh reboot/khóa bảo mật; cần kiểm định thiết bị thật. |
| N6 | Phần mềm có Wi-Fi/CA riêng, secret reference theo scope, xác minh đường khôi phục và xoay/gỡ cấu hình theo journal; cần kiểm định thiết bị thật. |
| N7 | Danh sách thiết bị/firmware hỗ trợ, phạm vi tương thích, bằng chứng quy mô và kiểm định stable. |

Ứng dụng chủ quản lý người dùng, tài sản, phân quyền, workflow và giao diện. Kylok cung cấp ID phạm vi/thiết bị trung lập cùng ngữ nghĩa quản lý Android. PostgreSQL và luồng thay đổi tin cậy đã có trong mã nguồn hiện tại; callback vẫn là best-effort.

Lần kiểm tra thiết bị ngày 2026-10-27 cũng phát hiện lỗi chặn gỡ ứng dụng. Cần khắc phục và kiểm tra lại trước khi đánh giá stable; đăng ký khi chặn WAN, kiosk và quyền runtime vẫn còn mở. [Lộ trình repository](https://github.com/thienvu18/kylok/blob/main/ROADMAP.md) nêu điều kiện hoàn tất; [kế hoạch triển khai](https://github.com/thienvu18/kylok/blob/main/docs/implementation-plan.md) nêu interface, thứ tự và kiểm tra chấp nhận. [Checklist thiết bị](https://github.com/thienvu18/kylok/blob/main/docs/provisioning.md) ghi các bước còn lại.

N5 có triển khai phần mềm: nhóm theo scope, danh sách đích cố định, fan-out có checkpoint, lịch/hủy triển khai và lệnh reboot/khóa bảo mật có kết quả chưa xác định. Các luồng có kiểm tra tự động; kiểm định thiết bị thật vẫn mở. N6 có quản lý Wi-Fi/CA riêng, đường khôi phục độc lập và kiểm tra kết nối xác thực; các hiệu ứng hệ điều hành cần kiểm định thiết bị thật.
