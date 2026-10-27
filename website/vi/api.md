# Tham chiếu API

Tra cứu API để biết dịch vụ Go và ứng dụng Android của bạn có thể gọi những thao tác nào:

- [SDK máy chủ Go](../api/go.html)
- [SDK Android và trạng thái thiết bị](../api/android/index.html)

Bắt đầu từ [hướng dẫn tích hợp](install.html) nếu bạn đang đưa Kylok vào sản phẩm: trang đó chỉ cách khai báo SDK, cấu hình ứng dụng chủ và chạy thử một luồng hoàn chỉnh.

## Quản lý thiết bị từ dịch vụ Go

| Mục tiêu | API |
| --- | --- |
| Khởi động và dừng dịch vụ | `New`, `Close`, `Handler`, `BootstrapHandler`, `Version` |
| Đăng ký thiết bị | `CreateEnrollment`, `QR`, `QRWithOrigins` |
| Xem hoặc thu hồi thiết bị | `ListDevices`, `RevokeDevice` |
| Phân phối và cập nhật ứng dụng | `RegisterApplication`, `ListApplications`, `AssignApplication`, `ListOperations`, `RemoveApplication` |
| Cấu hình và xem chính sách | `SetPolicy`, `GetPolicy` |
| Dùng bộ lưu trữ hoặc sự kiện riêng | `ports.StateStore`, `ArtifactStore`, `Options.Events`, `Clock` |

## Xem trạng thái thiết bị trong ứng dụng Android

| Mục tiêu | API |
| --- | --- |
| Lên lịch đồng bộ | `Kylok.retry(context)` |
| Đọc trạng thái thiết bị mới nhất | `Kylok.snapshot(context) { AgentStatus -> … }` |
| Nhận cập nhật trạng thái | `Kylok.observe(StatusListener { … })`; đóng đăng ký khi ứng dụng kết thúc |
| Mở xác nhận cài đặt của Android | Gọi `Kylok.confirmPendingInstall(context)` sau thao tác của người dùng |
| Hiển thị thông tin chẩn đoán | `Kylok.status`, `sdkVersion`, `protocolVersion` |

`AgentStatus` cho biết ứng dụng đang yêu cầu phiên bản nào và thiết bị đã cài phiên bản nào, kết quả từng quy tắc chính sách cùng tiến độ cài đặt còn dang dở. Trạng thái này không chứa token đăng ký, thông tin xác thực hay khóa riêng.

Mã nguồn hiện tại bổ sung API đọc/liên kết theo phạm vi, PostgreSQL, luồng thay đổi bền vững và `HostOptions`/cấu hình ứng dụng theo kiểu. Xem [tích hợp host](https://github.com/thienvu18/kylok/blob/main/docs/host-integration.md) và [cấu hình Android](https://github.com/thienvu18/kylok/blob/main/docs/managed-configuration.md); tham chiếu sinh tự động phản ánh mã nguồn hiện tại, mới hơn cặp SDK đầu tiên.
