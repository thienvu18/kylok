# Kylok — Tích hợp quản lý thiết bị Android vào sản phẩm của bạn

[![Trạng thái: đang thử nghiệm](https://img.shields.io/badge/status-experimental-orange)](ROADMAP.md)
[![Giấy phép: LGPL-3.0-only](https://img.shields.io/badge/license-LGPL--3.0--only-blue)](LICENSE)

[English](README.md) · [Tiếng Việt](README.vi.md)

Kylok giúp bạn đưa quản lý thiết bị Android vào sản phẩm hiện có mà không phải tự xây lại từ đầu các luồng đăng ký, phân phối ứng dụng và áp dụng chính sách. Bộ SDK gồm **SDK máy chủ Go** cùng **thư viện Android Device Policy Controller (DPC) viết bằng Kotlin**, vận hành trên hạ tầng do bạn làm chủ. Đội ngũ của bạn vẫn quyết định cách phân quyền, vòng đời máy chủ, định danh ứng dụng và khóa ký.

> [!IMPORTANT]
> Kylok đang được phát triển tích cực. Gói Go và Android có trên [Go module proxy](https://proxy.golang.org/github.com/thienvu18/kylok/go) và [Maven Central](https://central.sonatype.com/artifact/io.github.thienvu18.kylok/kylok-dpc). Khi build từ mã nguồn, dùng hai SDK cùng revision. Các điều kiện kiểm định stable vẫn còn mở; xem [trạng thái hiện tại](website/vi/compatibility.md).

## Năng lực hiện tại

- Đăng ký Android Device Owner bằng QR một lần, hỗ trợ địa chỉ IP hoặc tên miền qua HTTP/HTTPS.
- Phân phối APK đã ký với tải tiếp sau gián đoạn, kiểm tra tính toàn vẹn, theo dõi cài đặt bền vững và thực thi qua `PackageInstaller`.
- Thực thi chính sách trực tiếp ở tầng OS (`DevicePolicyManager.setUninstallBlocked`, `setCameraDisabled`, `setScreenCaptureDisabled`, `setMaximumTimeToLock`, `addUserRestriction`, `setPermissionGrantState`, và `setLockTaskPackages`).
- Cung cấp launcher HOME bền vững (`KylokHomeActivity`), phục hồi sau boot và khởi chạy lại ứng dụng khi bị crash (`LauncherRecovery`).
- Chạy vòng lặp đồng bộ định kỳ và tức thời qua WorkManager (`EnrollmentWorker`) cùng màn hình báo cáo tuân thủ trên thiết bị (`PolicyComplianceActivity`).
- Quản lý nhóm thiết bị theo scope, gán hàng loạt có thể tiếp tục, cửa sổ triển khai và lệnh reboot/khóa bảo mật.
- Tích hợp SQLite hoặc PostgreSQL, truy vấn theo scope và change feed vào host Go.
- Tùy chọn quản lý credential Wi-Fi/CA riêng bằng secret reference và xác minh đường khôi phục.

Kylok chạy trên hạ tầng do ứng dụng chủ quản lý. Host giữ application ID/khóa ký, phân quyền, quan hệ thiết bị/người dùng và giao diện quản trị. App nghiệp vụ tự quản lý màn hình, dữ liệu cục bộ và workflow. Dù các bộ kiểm thử tự động, conformance và interop đã xác minh hành vi phần mềm, việc kiểm định vật lý trên thiết bị và firmware OEM vẫn được theo dõi trong lộ trình và bằng chứng tương thích. Xem [lộ trình](ROADMAP.md) và [giới hạn tương thích](docs/compatibility.md).

## Cài đặt SDK

```sh
go get github.com/thienvu18/kylok/go@v0.1.0-dev.40449063797
```

```kotlin
repositories { mavenCentral() }
dependencies { implementation("io.github.thienvu18.kylok:kylok-dpc:0.1.0-dev.40449063797") }
```

Không cần tài khoản registry để tải các gói công khai. Xem [hướng dẫn tích hợp](website/vi/install.md) để chuẩn bị APK, HTTP/2 và vòng đời ứng dụng. Mã nguồn hiện có [PostgreSQL/change feed](docs/host-integration.md), [cấu hình host/app Android](docs/managed-configuration.md), [thao tác fleet](docs/fleet-operations.md) và [quản lý mạng](docs/managed-network.md).

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

[Thiết kế messaging cục bộ](docs/local-messaging.md) · [Kiểm tra trên thiết bị thật](docs/provisioning.md) · [Tương thích](docs/compatibility.md) · [Chính sách](docs/policies.md)

Đóng góp gốc của dự án dùng giấy phép **LGPL-3.0-only**; xem [NOTICE](NOTICE), [LICENSE](LICENSE) và [COPYING](COPYING).
