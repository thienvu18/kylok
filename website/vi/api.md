# Tham chiếu API

Tài liệu API được tự sinh từ chữ ký hiện tại cùng chú thích mục đích/cách dùng trong nguồn:

- [Go facade, hợp đồng lưu trữ, adapter và provisioning](../api/go.html)
- [API host Android và thành phần nền tảng, sinh bằng Dokka](../api/android/index.html)

Chữ ký và chú thích nguồn là tham chiếu kỹ thuật dùng chung. Bắt đầu bằng [hướng dẫn tích hợp](install.html) để hiểu vòng đời host và cách dùng có thể chạy. Lớp triển khai/giao thức tự sinh không phải hợp đồng plugin tổng quát.

## Thao tác Go thường dùng

| Mục tiêu | API |
| --- | --- |
| Khởi tạo/đóng | `New`, `Close`, `Handler`, `BootstrapHandler`, `Version` |
| Provisioning | `CreateEnrollment`, `QR`, `QRWithOrigins` |
| Danh bạ | `ListDevices`, `RevokeDevice` |
| Ứng dụng/cập nhật | `RegisterApplication`, `ListApplications`, `AssignApplication`, `ListOperations`, `RemoveApplication` |
| Chính sách | `SetPolicy`, `GetPolicy` |
| Mở rộng | `ports.StateStore`, `ArtifactStore`, `portstest`, `Options.Events`, `Clock` |

## Thao tác Android thường dùng

| Mục tiêu | API |
| --- | --- |
| Yêu cầu đối chiếu/đồng bộ | `Kylok.retry(context)` |
| Đọc trạng thái cục bộ hiện tại | `Kylok.snapshot(context) { AgentStatus -> … }` |
| Theo dõi thay đổi | `Kylok.observe(StatusListener { … })`; đóng registration theo vòng đời |
| Xác nhận cài đặt OS | `Kylok.confirmPendingInstall(context)` từ thao tác người dùng |
| Chẩn đoán | `Kylok.status`, `sdkVersion`, `protocolVersion` |

`AgentStatus` tách yêu cầu/quan sát ứng dụng, kết quả chính sách và tiến độ cài đặt cục bộ đã lưu. Không trả về credential, nội dung QR hoặc khóa riêng.
