# Build từ mã nguồn

Kylok đang trong giai đoạn thử nghiệm. API, schema và định dạng dữ liệu cục bộ có thể thay đổi mà không bảo đảm tương thích ngược. Nếu build từ mã nguồn, hãy dùng cùng một phiên bản nguồn cho SDK Go và Android.

## Build và kiểm tra

Dùng Go 1.27.1, JDK 17, Gradle 9.3.1, Android Gradle Plugin 9.1.1 và Android SDK/build-tools 37. Cấu hình `JAVA_HOME`, `ANDROID_HOME`, sau đó chạy:

```sh
make verify
python3 scripts/check-repository.py
```

Các bước này kiểm tra dịch vụ Go, thư viện Android, giao tiếp mạng và tích hợp qua các gói SDK. Tác động cài ứng dụng và áp dụng chính sách trên Android được mô phỏng; build thành công chưa xác nhận hành vi trên thiết bị thật. Xem [trang tương thích](compatibility.html) trước khi chọn thiết bị triển khai.

## Trước khi đưa vào sử dụng

Ứng dụng của bạn tự quản lý phân quyền nghiệp vụ, vòng đời máy chủ, application ID và khóa ký. Chỉ quy trình provisioning của Android mới cấp được quyền Device Owner. Hãy kiểm tra cấu hình tin cậy mạng và chính sách trên các thiết bị dự định hỗ trợ; chứng chỉ nhúng trong ứng dụng không thay đổi bước xác minh mà Android Setup Wizard thực hiện trước khi cài ứng dụng.

Xem [hướng dẫn đóng góp](https://github.com/thienvu18/kylok/blob/main/CONTRIBUTING.md) nếu bạn muốn tham gia phát triển dự án.
