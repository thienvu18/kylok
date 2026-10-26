# Cài đặt và tích hợp

Kylok gồm hai SDK có phiên bản độc lập. Bản phát triển hiện tại là Go `0.1.0-dev.40316176224` và Android `0.1.0-dev.40316176224`, dùng giao thức 1. Ghép hai SDK từ cùng một phiên bản mã nguồn; số giao thức giống nhau không bảo đảm tương thích giữa các bản phát triển.

## Lấy gói SDK

Chọn phiên bản tại [GitHub Releases](https://github.com/thienvu18/kylok/releases). Go module công khai và Maven Central không cần tài khoản đọc. Với `mavenCentral()`, khai báo `io.github.thienvu18.kylok:kylok-dpc:VERSION`; dùng `go get github.com/thienvu18/kylok/go@vVERSION` cho Go. Thay VERSION bằng phiên bản SDK đã phát hành. Để dùng gói cục bộ, chạy tại thư mục dự án với bộ công cụ đã cài:

```sh
make distribution
make test-consumers
```

`out/release` chứa Go module proxy, Maven repository, metadata đã kiểm tra, mã nguồn/giấy phép và checksum. `make test-release` tạo gói tải xuống đầy đủ tại `out/release-assets`. Không chỉ chép tệp AAR: metadata Maven cung cấp các dependency cần thiết.

## Ứng dụng chủ Go

Module: `github.com/thienvu18/kylok/go`. Để dùng gói cục bộ, thêm `go-proxy` đã tạo làm file proxy. Phiên bản Go công khai sẽ dùng tag `go/vX.Y.Z` trong repository.

Từ một module mới, thay đường dẫn tuyệt đối đến gói đã giải nén:

```sh
GOWORK=off GOPROXY=file:///absolute/path/to/release/go-proxy,https://proxy.golang.org \
  GONOSUMDB=github.com/thienvu18/kylok/go \
  go get github.com/thienvu18/kylok/go@v0.1.0-dev.40316176224
```

Ngoại lệ cơ sở dữ liệu checksum chỉ dùng cho bản cục bộ chưa phát hành. Bản công khai dùng xác minh module thông thường.

```go
apk, err := provisioning.Inspect(ctx, signedHostAPK, provisioning.Tools{})
// Xử lý err trước khi khởi tạo SDK.
sdk, err := kylok.New(ctx, kylok.Options{
    PublicURL: "http://192.168.101.5:8080",
    Bootstrap: apk,
    DataDir: "/var/lib/kylok",
})
// Xử lý err. Gắn sdk.Handler() vào listener do ứng dụng chủ quản lý.
```

Ứng dụng chủ chịu trách nhiệm phân quyền nghiệp vụ, listener và quá trình tắt. Phải bật HTTP/2: với HTTP, dùng `http.Protocols.SetUnencryptedHTTP2(true)` cùng HTTP/1; với HTTPS, dùng HTTP/2 và chứng chỉ được tin cậy. Listener chỉ hỗ trợ HTTP/1 không phục vụ được gRPC của Android.

`CreateEnrollment`/`QR` tạo đăng ký; `ListDevices`/`RevokeDevice` quản lý định danh; API ứng dụng đăng ký/gán/liệt kê/xóa metadata đã xác minh; API chính sách thay cấu hình yêu cầu và đọc mức tuân thủ. Mặc định dùng SQLite/hệ thống tệp bền vững. Provider tùy chỉnh của `ports` do ứng dụng chủ quản lý vòng đời và phải vượt bộ kiểm tra hợp đồng.

## Ứng dụng chủ Android

Cấu hình Maven repository trỏ đến repository cục bộ đã giải nén, rồi khai báo:

```kotlin
// Cấu hình repository dependency của ứng dụng chủ:
repositories {
    google()
    mavenCentral()
    maven { url = uri("/absolute/path/to/release/maven") }
}
// Module Android của ứng dụng chủ:
implementation("io.github.thienvu18.kylok:kylok-dpc:0.1.0-dev.40316176224")
```

Dùng JDK 17, min SDK 33, compile/target SDK 37 và trình biên dịch Kotlin 2.2 tương thích. Ứng dụng chủ sở hữu application ID, khóa ký, quyền HTTP/cấu hình tin cậy TLS và chính sách sao lưu. Giữ các thành phần provisioning và quy tắc loại trừ dữ liệu của AAR. Chỉ quy trình thiết lập Android có thể cấp Device Owner.

```kotlin
val registration = Kylok.observe(StatusListener { status -> render(status) })
Kylok.snapshot(context) { status -> render(status) }
// Đóng registration theo vòng đời ứng dụng chủ.
Kylok.retry(context) // lên lịch đồng bộ; kết quả được quan sát riêng
```

Chỉ gọi `Kylok.confirmPendingInstall` từ thao tác chủ động của người dùng khi Android yêu cầu xác nhận. Hook trạng thái/sự kiện chỉ đọc, phân phối best-effort; không thể phê duyệt chính sách hay bỏ qua xác thực. Xem [API](api.html) và [luồng ứng dụng mẫu](examples.html).
