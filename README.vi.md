# Kylok — Tích hợp quản lý thiết bị Android vào sản phẩm của bạn

[![Trạng thái: đang thử nghiệm](https://img.shields.io/badge/status-experimental-orange)](ROADMAP.md)
[![Giấy phép: LGPL-3.0-only](https://img.shields.io/badge/license-LGPL--3.0--only-blue)](LICENSE)

[English](README.md) · [Tiếng Việt](README.vi.md)

Kylok giúp bạn đưa quản lý thiết bị Android vào sản phẩm hiện có mà không phải tự xây lại từ đầu các luồng đăng ký, phân phối ứng dụng và áp dụng chính sách. Bộ SDK gồm **SDK máy chủ Go** cùng **thư viện Android Device Policy Controller (DPC) viết bằng Kotlin**, vận hành trên hạ tầng do bạn làm chủ. Đội ngũ của bạn vẫn quyết định cách phân quyền, vòng đời máy chủ, định danh ứng dụng và khóa ký.

> [!IMPORTANT]
> Kylok đang trong giai đoạn thử nghiệm. API, schema và cơ sở dữ liệu của các bản phát triển có thể thay đổi mà không bảo đảm tương thích ngược. Xem các gói phát triển hiện có tại [GitHub Releases](https://github.com/thienvu18/kylok/releases). Kiểm tra trên thiết bị thật đến nay xác nhận thiết lập bằng QR và đăng ký LAN có xác thực trên POCO Pad X1 chạy Android 15 / API 35, khi thiết bị vẫn có Internet. Đăng ký ngoại tuyến, cài đặt/khôi phục ứng dụng và áp dụng chính sách trên thiết bị thật vẫn cần được kiểm tra riêng.

## Bạn có thể xây dựng gì với Kylok?

- Đăng ký thiết bị Android bằng QR riêng, chỉ dùng một lần; hỗ trợ địa chỉ IP hoặc tên miền, qua HTTP hoặc HTTPS.
- Quản lý định danh thiết bị với token gắn theo phạm vi, cơ chế khôi phục thông tin xác thực đã mã hóa và khả năng thu hồi.
- Gán APK đã ký và theo dõi phiên bản cần có, danh sách ứng dụng trên thiết bị, tiến độ cài đặt cùng kết quả.
- Tiếp tục tải APK có xác thực sau khi gián đoạn, kiểm tra digest/khóa ký và khôi phục phiên cài đặt.
- Áp dụng các chính sách cơ bản cho ứng dụng bắt buộc, Android Lock Task, quyền runtime và một số giới hạn; theo dõi kết quả thực tế của từng quy tắc.
- Tích hợp SDK vào ứng dụng Go và Android của bạn. Có sẵn lưu trữ SQLite và hệ thống tệp; ứng dụng chủ toàn quyền quản lý provider tùy chỉnh.
- Bắt đầu từ bảng điều khiển web và ứng dụng Android mẫu để thử luồng đăng ký, ứng dụng, chính sách và trạng thái thiết bị.

Kylok là bộ SDK để tích hợp, không phải dịch vụ quản lý thiết bị lưu trữ sẵn trên đám mây: ứng dụng của bạn quyết định cách phân quyền, vận hành máy chủ, đặt application ID và ký ứng dụng. Các ứng dụng mẫu minh họa một cách kết nối những thành phần này. Mã nguồn nhắm Android API 33–37; compile/target SDK là 37. Xem [bằng chứng kiểm thử](website/vi/compatibility.md) để biết những gì đã được xác nhận.

## Trải nghiệm ứng dụng mẫu

Sau khi cài [bộ công cụ](website/vi/development.md), khởi động máy chủ mẫu và ứng dụng Android:

```sh
./scripts/prepare-local-server.sh 192.168.101.5 http
./scripts/run-local-server.sh 192.168.101.5 http
```

Để thử trong LAN tin cậy, máy chủ mẫu cung cấp API và APK qua HTTP mà không cần tên miền hay chứng chỉ. Lưu ý HTTP không mã hóa dữ liệu truyền đi. Mở bảng điều khiển mẫu tại `http://127.0.0.1:8081`, rồi nhập token riêng theo đường dẫn trong log khởi động. Đây là ứng dụng tham khảo, không phải dịch vụ Kylok được lưu trữ sẵn. Xem [hướng dẫn ứng dụng mẫu](website/vi/examples.md) để đăng ký và quản lý thiết bị; bắt đầu từ [hướng dẫn tích hợp SDK](website/vi/install.md) nếu muốn nhúng Kylok vào sản phẩm.

Tài liệu [tiếng Anh](website/en/index.md) và [tiếng Việt](website/vi/index.md) hướng dẫn thiết lập, phân phối ứng dụng và quản lý chính sách. Tham khảo [API](website/vi/api.md) và [bằng chứng tương thích](website/vi/compatibility.md) khi lên kế hoạch tích hợp.

[Tích hợp SDK](docs/sdk.md) · [Kiến trúc](docs/architecture.md) · [Giao thức](docs/enrollment-protocol.md) · [Lộ trình](ROADMAP.md) · [Đóng góp](CONTRIBUTING.md)

[Kiểm tra trên thiết bị thật](docs/provisioning.md) · [Tương thích](docs/compatibility.md) · [Chính sách](docs/policies.md)

Đóng góp gốc của dự án dùng giấy phép **LGPL-3.0-only**; xem [NOTICE](NOTICE), [LICENSE](LICENSE) và [COPYING](COPYING).
