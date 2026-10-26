# Kiến trúc và kết nối

Kylok bổ sung khả năng quản lý thiết bị vào những ứng dụng bạn đang có. SDK Go chạy trong máy chủ của bạn; thư viện Android chạy trong ứng dụng trên thiết bị. Bạn vẫn tự quyết định cách phân quyền, vận hành máy chủ, đặt application ID, ký ứng dụng và xây dựng trải nghiệm cho người dùng. Các ứng dụng mẫu minh họa một cách kết nối hai phần này.

| Thành phần | Trách nhiệm |
| --- | --- |
| SDK máy chủ Go | Tạo mã đăng ký, quản lý thiết bị, gán ứng dụng và đặt chính sách. |
| Bộ lưu trữ tích hợp sẵn | Lưu hồ sơ thiết bị, chính sách và APK đã xác minh bằng SQLite cùng hệ thống tệp. |
| Thư viện Android | Đăng ký thiết bị, nhận yêu cầu, cài ứng dụng và gửi trạng thái thiết bị. |
| Ứng dụng của bạn | Quản lý phân quyền nghiệp vụ, vòng đời máy chủ, định danh và chữ ký ứng dụng, giao diện người dùng. |

## Địa chỉ máy chủ và giao thức kết nối

Bạn có thể dùng địa chỉ IP hoặc tên miền với HTTP hay HTTPS. Kylok dùng đúng giao thức trong địa chỉ đã cấu hình và không tự hạ từ HTTPS xuống HTTP. HTTPS xác minh chứng chỉ và tên máy chủ; HTTP không mã hóa token đăng ký hay thông tin xác thực của thiết bị khi truyền. Máy chủ API và máy chủ APK ban đầu có thể dùng hai địa chỉ khác nhau.

SDK Android giao tiếp qua HTTP/2. Nếu dùng HTTP, listener Go cần hỗ trợ h2c; nếu dùng HTTPS, listener phải hỗ trợ HTTP/2. Trước khi QR tải ứng dụng Android, Setup Wizard phải tin cậy chứng chỉ của máy chủ APK. Cấu hình tin cậy bên trong ứng dụng không thay đổi được bước xác minh trước khi cài đặt đó.

## Định danh bền vững và thay đổi trên thiết bị

Mỗi QR đăng ký chứa một token chỉ dùng một lần và gắn với phạm vi quản lý. Nếu quá trình thiết lập bị gián đoạn, thiết bị có thể gửi lại cùng yêu cầu để khôi phục đúng định danh. Thiết bị đã bị thu hồi không thể kết nối lại bằng thông tin xác thực cũ.

Kylok phân biệt ứng dụng và chính sách được yêu cầu với những gì Android báo đã cài hoặc đã áp dụng. Tải xong, nhận callback cài đặt hay gửi lệnh chính sách chưa đủ để báo thành công: SDK kiểm tra gói/phiên bản thực tế và trả kết quả riêng cho từng quy tắc chính sách. Công việc đã lưu có thể tiếp tục sau khi mất mạng hoặc ứng dụng bị dừng.

Nếu dùng bộ lưu trữ riêng, bạn cần bảo đảm dữ liệu giữa các thiết bị không lẫn với nhau và các thao tác được lưu bền vững. Callback trạng thái/sự kiện chỉ là thông báo bổ sung; hãy lưu bằng hệ thống của ứng dụng nếu cần đưa thông tin đó vào nhật ký nghiệp vụ hoặc audit lâu dài.
