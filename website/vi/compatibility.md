# Kiểm định thiết bị hiện tại

Mã nguồn nhắm Android API 33–37, min SDK 33 và compile/target SDK 37. Đây là phạm vi build, không phải danh sách thiết bị được hỗ trợ. Khi build từ mã nguồn, dùng cùng revision cho SDK Go và Android.

| Thiết bị | Build và mạng | Kết quả quan sát |
| --- | --- | --- |
| POCO Pad X1, Xiaomi `25099RP08G`, Android 15 / API 35, HyperOS `OS2.0.205.0.VOZMIXM` | Mã nguồn `d9c375993eb0e4be78f1fa2868ca497ce345bb14`; LAN HTTP tại `192.168.101.5`; Internet vẫn hoạt động | Đăng ký online, cài/cập nhật app đã ký, cấu hình, đồng bộ nền, khôi phục sau khi khởi động lại máy chủ/mất LAN/khởi động lại thiết bị và một số chính sách đã vượt kiểm tra. Chặn gỡ app thất bại dù readback báo `APPLIED`. |

## Các kiểm tra còn mở

Không thể kiểm tra QR khi chặn WAN vì router không thể tách WAN nhưng vẫn giữ LAN. Kiosk, quyền runtime, thay thế DPC, khôi phục sau force-stop, timeout khóa an toàn, chặn thêm người dùng, origin khác, thu hồi và model/firmware khác vẫn còn mở hoặc chưa kiểm tra. `HostOptions` có kiểm tra tự động nhưng chưa có kết quả trên thiết bị với host consumer.

Kiểm tra Android tự động mô phỏng hiệu ứng hệ điều hành, không chứng minh chính sách được OEM thực thi. Xem [báo cáo thiết bị](https://github.com/thienvu18/kylok/blob/main/docs/validation/2026-10-27-poco-pad-x1.md), [checklist vật lý](https://github.com/thienvu18/kylok/blob/main/docs/provisioning.md) và [điều kiện stable](https://github.com/thienvu18/kylok/blob/main/docs/release-readiness.md). Không chia sẻ token, thông tin xác thực hoặc khóa riêng.
