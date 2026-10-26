# Gói phát hành

Kylok gồm SDK máy chủ Go và thư viện Android. Hãy chọn hai phiên bản tương ứng tại [GitHub Releases](https://github.com/thienvu18/kylok/releases). Go module tải từ kho Go công khai; gói Android có trên Maven Central. API của các bản phát triển có thể thay đổi mà không bảo đảm tương thích ngược.

## Cài đặt phiên bản đã phát hành

Dùng đúng phiên bản được ghi trong GitHub Releases cho cả hai SDK. Thêm Go module vào máy chủ bằng `go get github.com/thienvu18/kylok/go@vVERSION`. Trong ứng dụng Android, thêm `mavenCentral()` vào danh sách repository rồi khai báo dependency `io.github.thienvu18.kylok:kylok-dpc:VERSION`. Bạn không cần thông tin đăng nhập để tải các gói này.

## Tự build gói từ mã nguồn

Nếu muốn chỉnh sửa Kylok hoặc tạo gói để tích hợp cục bộ, chạy:

```sh
make distribution
make test-consumers
```

Các gói dùng tại máy được tạo trong `out/release`; lệnh này không công khai gói. Xem [hướng dẫn phát triển](development.html) để biết yêu cầu bộ công cụ và các bước kiểm tra.
