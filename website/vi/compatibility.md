# Tương thích và bằng chứng

Kylok nhắm Android API 33–37, với min SDK 33 và compile/target 37. Đây là dải API dùng để build, không có nghĩa mọi thiết bị hay firmware trong dải này đều đã được chứng nhận. Hãy dùng cặp gói Go và Android cùng một phiên bản; [GitHub Releases](https://github.com/thienvu18/kylok/releases) liệt kê các bản hiện có.

[Kiểm tra tích hợp SDK](https://github.com/thienvu18/kylok/actions/runs/40282303400) đã thành công ngày 2026-10-26 trên mã nguồn `4e0730f`. Kết quả xác nhận hai SDK hoạt động cùng nhau trong môi trường tự động; đây không phải chứng nhận cho thiết bị Android thật.

| Bằng chứng | Chứng minh điều gì |
| --- | --- |
| Hành vi máy chủ Go | Kiểm tra tự động các luồng đăng ký, phân tách dữ liệu thiết bị, đồng bộ, gán ứng dụng và chính sách. |
| Giao tiếp Go và Android | Đăng ký có xác thực, tải APK và gửi báo cáo qua HTTP/HTTPS; tác động cài đặt và chính sách trên Android được mô phỏng. |
| Tích hợp gói Android | AAR build được và tích hợp vào ứng dụng Android độc lập, bao gồm cả bản build minify. |
| Tích hợp qua gói SDK | Ứng dụng Go và Android độc lập dùng các gói SDK mà không chép mã nguồn SDK vào project. |
| POCO Pad X1, Android 15 / API 35, HyperOS OS2.0.205.0.VOZMIXM | Một APK phát triển đã thiết lập bằng QR và đăng ký LAN qua HTTP có xác thực, **khi Internet vẫn hoạt động**. |

## Những nội dung còn cần kiểm tra trên thiết bị

- Đăng ký thiết bị đã đặt lại khi chặn Internet suốt quá trình thiết lập và tải APK, nhưng vẫn truy cập được máy chủ qua LAN.
- Cài/cập nhật ứng dụng trên thiết bị thật, gồm cả trường hợp cài thất bại, không tương thích hoặc cần người dùng xác nhận.
- Phục hồi định danh và công việc còn dang dở sau khi mất mạng, dừng ứng dụng, khởi động lại máy chủ hoặc khởi động lại thiết bị.
- Cập nhật DPC mà vẫn giữ được Device Owner, định danh thiết bị và kết nối.
- Áp dụng và gỡ chính sách kiosk, ứng dụng, quyền và giới hạn trên firmware mục tiêu.

Nếu sản phẩm của bạn phụ thuộc vào một trong các luồng này, hãy kiểm tra trên đúng thiết bị, firmware và cấu hình mạng dự kiến sử dụng. Bảo mật QR đăng ký, token, thông tin xác thực và khóa riêng như các bí mật.
