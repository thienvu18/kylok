# Build từ mã nguồn

Kylok đang được phát triển. Khi build từ mã nguồn, hãy dùng cùng revision cho SDK Go và Android; tạo dữ liệu phát triển mới khi định dạng cục bộ thay đổi.

## Build và kiểm tra

Dùng Go 1.27.3 (module yêu cầu tối thiểu 1.27.1), JDK 17, Gradle 9.3.1, Android Gradle Plugin 9.1.1 và Android SDK/build-tools 37. Cấu hình `JAVA_HOME`, `ANDROID_HOME`, sau đó chạy:

```sh
make verify
```

Các bước này kiểm tra dịch vụ Go, thư viện Android, giao tiếp mạng và tích hợp qua các gói SDK. Tác động cài ứng dụng và áp dụng chính sách trên Android được mô phỏng; build thành công chưa xác nhận hành vi trên thiết bị thật. Xem [trang tương thích](compatibility.html) trước khi chọn thiết bị triển khai.

## Trước khi đưa vào sử dụng

Ứng dụng của bạn tự quản lý phân quyền nghiệp vụ, vòng đời máy chủ, application ID và khóa ký. Chỉ quy trình provisioning của Android mới cấp được quyền Device Owner. Hãy kiểm tra cấu hình tin cậy mạng và chính sách trên các thiết bị dự định hỗ trợ; chứng chỉ nhúng trong ứng dụng không thay đổi bước xác minh mà Android Setup Wizard thực hiện trước khi cài ứng dụng.

Xem [hướng dẫn đóng góp](https://github.com/thienvu18/kylok/blob/main/CONTRIBUTING.md) nếu bạn muốn tham gia phát triển dự án.

`make verify` đã bao gồm kiểm tra repository. Dùng `make check test-release-gates` cho thay đổi tài liệu/điều kiện phát hành và `make docs` để dựng lại website. Xem [chiến lược kiểm thử](https://github.com/thienvu18/kylok/blob/main/docs/testing.md) để biết phạm vi từng bộ kiểm tra.
