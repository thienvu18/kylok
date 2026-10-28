# Kylok — Tích hợp quản lý thiết bị Android vào sản phẩm của bạn

[![Trạng thái: đang thử nghiệm](https://img.shields.io/badge/status-experimental-orange)](ROADMAP.md)
[![Giấy phép: LGPL-3.0-only](https://img.shields.io/badge/license-LGPL--3.0--only-blue)](LICENSE)

[English](README.md) · [Tiếng Việt](README.vi.md)

Kylok giúp bạn đưa quản lý thiết bị Android vào sản phẩm hiện có mà không phải tự xây lại từ đầu các luồng đăng ký, phân phối ứng dụng và áp dụng chính sách. Bộ SDK gồm **SDK máy chủ Go** cùng **thư viện Android Device Policy Controller (DPC) viết bằng Kotlin**, vận hành trên hạ tầng do bạn làm chủ. Đội ngũ của bạn vẫn quyết định cách phân quyền, vòng đời máy chủ, định danh ứng dụng và khóa ký.

> [!IMPORTANT]
> Kylok đang được phát triển tích cực. Gói Go và Android có trên [Go module proxy](https://proxy.golang.org/github.com/thienvu18/kylok/go) và [Maven Central](https://central.sonatype.com/artifact/io.github.thienvu18.kylok/kylok-dpc). Khi build từ mã nguồn, dùng hai SDK cùng revision. Các điều kiện kiểm định stable vẫn còn mở; xem [trạng thái hiện tại](website/vi/compatibility.md).

## Bạn có thể xây dựng gì với Kylok?

- Đăng ký thiết bị Android bằng QR riêng, chỉ dùng một lần; hỗ trợ địa chỉ IP hoặc tên miền, qua HTTP hoặc HTTPS.
- Quản lý định danh thiết bị với token gắn theo phạm vi, cơ chế khôi phục thông tin xác thực đã mã hóa và khả năng thu hồi.
- Gán APK đã ký và theo dõi phiên bản cần có, danh sách ứng dụng trên thiết bị, tiến độ cài đặt cùng kết quả.
- Tiếp tục tải APK có xác thực sau khi gián đoạn, kiểm tra digest/khóa ký và khôi phục phiên cài đặt.
- Áp dụng các chính sách cơ bản cho ứng dụng bắt buộc, Android Lock Task, quyền runtime và một số giới hạn; theo dõi kết quả thực tế của từng quy tắc.
- Tích hợp SDK vào ứng dụng Go và Android của bạn. Có sẵn lưu trữ SQLite và hệ thống tệp; ứng dụng chủ toàn quyền quản lý provider tùy chỉnh.
- Bắt đầu từ bảng điều khiển web và ứng dụng Android mẫu để thử luồng đăng ký, ứng dụng, chính sách và trạng thái thiết bị.

Kylok là bộ SDK để tích hợp, không phải dịch vụ quản lý thiết bị lưu trữ sẵn trên đám mây: ứng dụng của bạn quyết định cách phân quyền, vận hành máy chủ, đặt application ID và ký ứng dụng. Các ứng dụng mẫu minh họa một cách kết nối những thành phần này. Mã nguồn nhắm Android API 33–37; compile/target SDK là 37. Xem [bằng chứng kiểm thử](website/vi/compatibility.md) để biết những gì đã được xác nhận.

## Đã kiểm tra trên thiết bị Android thật

Mã nguồn hiện tại đã được kiểm tra trên POCO Pad X1 chạy Android 15/API 35 và HyperOS: đăng ký QR khi có Internet, cài/cập nhật APK đã ký, cấu hình ứng dụng được quản lý và khôi phục sau khi gỡ chính sách, đồng bộ nền, khởi động lại máy chủ, mất mạng LAN và khởi động lại thiết bị. Máy chủ cũng từ chối yêu cầu trùng có nội dung khác và yêu cầu hạ phiên bản ứng dụng. Đã kiểm tra thêm chính sách camera, ảnh chụp màn hình và tài khoản. Bằng chứng áp dụng cho đúng thiết bị, firmware và build trong báo cáo. Android vẫn cho gỡ ứng dụng dù máy chủ báo chính sách chặn đã áp dụng, nên chức năng này chưa được xác nhận trên firmware đó. Xem [báo cáo thiết bị](docs/validation/2026-10-27-poco-pad-x1.md) và [trạng thái hiện tại](docs/compatibility.md).

## Cài đặt SDK

```sh
go get github.com/thienvu18/kylok/go@v0.1.0-dev.40316176224
```

```kotlin
repositories { mavenCentral() }
dependencies { implementation("io.github.thienvu18.kylok:kylok-dpc:0.1.0-dev.40316176224") }
```

Không cần tài khoản registry để tải các gói công khai. Xem [hướng dẫn tích hợp](website/vi/install.md) để chuẩn bị APK, HTTP/2 và vòng đời ứng dụng. Mã nguồn phát triển hiện đã có [PostgreSQL/luồng thay đổi](docs/host-integration.md) và [cấu hình Android/ứng dụng được quản lý](docs/managed-configuration.md); cần build từ mã nguồn cho đến khi có bản phát hành gói tiếp theo.

## Trải nghiệm ứng dụng mẫu

Sau khi cài [bộ công cụ](website/vi/development.md), khởi động máy chủ mẫu và ứng dụng Android:

```sh
./scripts/prepare-local-server.sh 192.168.101.5 http
./scripts/run-local-server.sh 192.168.101.5 http
```

Để thử trong LAN tin cậy, máy chủ mẫu cung cấp API và APK qua HTTP mà không cần tên miền hay chứng chỉ. Lưu ý HTTP không mã hóa dữ liệu truyền đi. Mở bảng điều khiển mẫu tại `http://127.0.0.1:8081`, rồi nhập token riêng theo đường dẫn trong log khởi động. Đây là ứng dụng tham khảo, không phải dịch vụ Kylok được lưu trữ sẵn. Xem [hướng dẫn ứng dụng mẫu](website/vi/examples.md) để đăng ký và quản lý thiết bị; bắt đầu từ [hướng dẫn tích hợp SDK](website/vi/install.md) nếu muốn nhúng Kylok vào sản phẩm.

Tài liệu [tiếng Anh](website/en/index.md) và [tiếng Việt](website/vi/index.md) hướng dẫn thiết lập, phân phối ứng dụng và quản lý chính sách. Tham khảo [API](website/vi/api.md) và [bằng chứng tương thích](website/vi/compatibility.md) khi lên kế hoạch tích hợp.

## Người đóng góp

[![Người đóng góp](https://contrib.rocks/image?repo=thienvu18/kylok)](https://github.com/thienvu18/kylok/graphs/contributors)

[Tích hợp SDK](docs/sdk.md) · [Kiến trúc](docs/architecture.md) · [Giao thức](docs/enrollment-protocol.md) · [Lộ trình](ROADMAP.md) · [Đóng góp](CONTRIBUTING.md) · [Quy tắc ứng xử](CODE_OF_CONDUCT.md) · [Bảo mật](SECURITY.md)

[Kiểm tra trên thiết bị thật](docs/provisioning.md) · [Tương thích](docs/compatibility.md) · [Chính sách](docs/policies.md)

Đóng góp gốc của dự án dùng giấy phép **LGPL-3.0-only**; xem [NOTICE](NOTICE), [LICENSE](LICENSE) và [COPYING](COPYING).

## Vận hành fleet trong mã nguồn hiện tại

SDK cung cấp nhóm thiết bị theo scope, gán APK hàng loạt có checkpoint, lịch triển khai tuyệt đối và hủy tác vụ. Lệnh reboot/khóa bảo mật dùng checkpoint trước hiệu ứng và kết quả `UNCERTAIN` khi chưa thể xác nhận; phần này có kiểm tra tự động và cần kiểm định thiết bị thật. Xem [hợp đồng fleet](docs/fleet-operations.md). Quản lý Wi-Fi/CA riêng dùng secret reference theo scope, xác minh đường khôi phục và journal khi xoay/gỡ cấu hình; xem [quản lý mạng](docs/managed-network.md). Phần mạng có kiểm tra phần mềm và cần kiểm định thiết bị thật.
