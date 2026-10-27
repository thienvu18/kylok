# Cài đặt và tích hợp

Kylok gồm hai thư viện phối hợp với nhau: SDK Go chạy trên máy chủ và thư viện Android Device Policy Controller (DPC) chạy trong ứng dụng trên thiết bị. Dùng phiên bản Go và Android tương ứng. Ứng dụng của bạn tự quản lý phân quyền nghiệp vụ, vòng đời máy chủ, application ID và khóa ký.

## Lấy gói SDK

Chọn phiên bản hiện có tại [GitHub Releases](https://github.com/thienvu18/kylok/releases). Go module được công khai; thư viện Android có trên Maven Central. Cả hai đều không cần thông tin đăng nhập để tải. Hãy dùng Go và Android SDK cùng phiên bản/revision.

## Tích hợp vào máy chủ Go

Thêm SDK Go vào module của máy chủ:

```sh
go get github.com/thienvu18/kylok/go@v0.1.0-dev.40316176224
```

Kiểm tra APK host đã ký, khởi tạo SDK rồi gắn handler vào máy chủ:

```go
apk, err := provisioning.Inspect(ctx, signedHostAPK, provisioning.Tools{})
// Xử lý err trước khi khởi tạo SDK.
sdk, err := kylok.New(ctx, kylok.Options{
    PublicURL: "https://mdm.example.com",
    Bootstrap: apk,
    DataDir: "/var/lib/kylok",
})
// Xử lý err. Gắn sdk.Handler() vào HTTP server của bạn.
```

Máy chủ của bạn quyết định cách phân quyền, địa chỉ lắng nghe và thời điểm dừng. SDK Android dùng gRPC qua HTTP/2: hãy bật HTTP/2 trên listener; nếu dùng HTTP, cần bật thêm h2c. Listener chỉ hỗ trợ HTTP/1 sẽ không phục vụ được ứng dụng Android.

Dùng API Go để tạo QR đăng ký, xem hoặc thu hồi thiết bị, đăng ký và gán APK, theo dõi tiến độ cài đặt, đặt hoặc kiểm tra chính sách. SDK có sẵn lưu trữ bằng SQLite và hệ thống tệp. Nếu sản phẩm cần lưu ở nơi khác, bạn có thể cung cấp bộ lưu trữ riêng qua các interface trong `ports`.

## Tích hợp vào ứng dụng Android

Thêm Maven Central và dependency Kylok vào ứng dụng Android:

```kotlin
repositories {
    google()
    mavenCentral()
}
dependencies {
    implementation("io.github.thienvu18.kylok:kylok-dpc:0.1.0-dev.40316176224")
}
```

Dùng JDK 17, min SDK 33, compile/target SDK 37 và trình biên dịch Kotlin 2.2 tương thích. Ứng dụng của bạn sở hữu application ID, khóa ký, cấu hình tin cậy mạng và chính sách sao lưu. Giữ lại các thành phần provisioning và quy tắc loại trừ dữ liệu sao lưu do thư viện cung cấp. Chỉ quy trình provisioning của Android mới cấp được quyền Device Owner.

Đọc trạng thái thiết bị hoặc lên lịch đồng bộ từ ứng dụng:

```kotlin
val registration = Kylok.observe(StatusListener { status -> render(status) })
Kylok.snapshot(context) { status -> render(status) }
// Đóng registration theo vòng đời ứng dụng.
Kylok.retry(context) // lên lịch đồng bộ; đọc kết quả riêng sau đó
```

Chỉ gọi `Kylok.confirmPendingInstall` sau thao tác của người dùng khi Android yêu cầu xác nhận cài đặt. Callback trạng thái chỉ để đọc; chúng không phê duyệt chính sách hay bỏ qua xác thực. Xem [tham chiếu API](api.html) và [luồng ứng dụng mẫu](examples.html).

Mã nguồn hiện tại có API [tích hợp host](https://github.com/thienvu18/kylok/blob/main/docs/host-integration.md) và [cấu hình Android](https://github.com/thienvu18/kylok/blob/main/docs/managed-configuration.md).

## Quyền và secret cho quản lý mạng

Quản lý mạng là tùy chọn. Host cấp quyền vị trí chính xác và thiết bị Wi-Fi ở gần, giữ một SSID khôi phục độc lập và cung cấp secret reference theo scope có giá trị bất biến qua `Options.NetworkSecrets`. Dashboard Android mẫu chỉ hiện lời nhắc quyền khi có network policy. CLI cục bộ có `set-network`/`network`, thao tác fleet và lệnh một lần. Xem [vòng đời mạng](https://github.com/thienvu18/kylok/blob/main/docs/managed-network.md) và [lệnh CLI](https://github.com/thienvu18/kylok/blob/main/docs/local-server.md). Các luồng có kiểm tra phần mềm và cần kiểm định thiết bị thật.
