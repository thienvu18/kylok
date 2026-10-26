# Kiến trúc và kết nối

Kylok là cặp SDK, không ép dùng framework nghiệp vụ. Ứng dụng Go quản lý phân quyền, listener và vòng đời. Ứng dụng Android sở hữu application ID, khóa ký, cấu hình tin cậy mạng và giao diện. Ví dụ chỉ dùng API công khai; SDK không phụ thuộc ví dụ hoặc ứng dụng nghiệp vụ.

| Thành phần | Trách nhiệm |
| --- | --- |
| Go facade | Đăng ký/QR, danh bạ, danh mục/gán ứng dụng, tác vụ, chính sách và sự kiện tùy chọn. |
| SQLite/hệ thống tệp mặc định | Giao dịch bền vững, định danh theo phạm vi, nội dung APK bất biến đã xác minh. |
| Android AAR | Callback provisioning, trạng thái mã hóa, đồng bộ, tải/cài đặt và áp dụng chính sách. |
| Protobuf dùng chung | Hợp đồng có kiểu cho yêu cầu/phản hồi, phiên bản, báo cáo, lỗi và tuân thủ. |

## Origin trong LAN

HTTP/HTTPS và IP/tên miền đều là cấu hình thông thường. Scheme cấu hình quyết định giao thức; không tự hạ cấp. HTTPS kiểm tra chứng chỉ/tên máy chủ. HTTP không mã hóa thông tin đăng ký hoặc bearer credential khi truyền. API và APK bootstrap có thể dùng hai origin riêng.

Android dùng gRPC trên HTTP/2. HTTP cần listener Go hỗ trợ h2c. Tin cậy của Setup Wizard trước khi cài APK khác với tin cậy cấu hình trong ứng dụng đã cài. CA riêng nhúng trong ứng dụng không thể khiến lần tải trước đó của Setup Wizard trở nên tin cậy.

## Định danh và tác động bền vững

Token đăng ký chỉ dùng một lần và gắn với phạm vi. Android lưu attempt/khóa trước khi nhận định danh. Máy chủ tiêu thụ token, tạo định danh và lưu biên nhận khôi phục mã hóa gắn với khóa trong một giao dịch. Retry đúng nhận lại định danh cũ; replay khác binding và định danh đã thu hồi bị từ chối.

Đồng bộ lưu yêu cầu trước khi gửi, gắn số thứ tự với đúng nội dung. Phiên bản yêu cầu, phiên bản báo cáo và quan sát thực tế được tách riêng. Phiên cài đặt và lần áp dụng chính sách được lưu trước tác động bên ngoài. Gói/phiên bản/chữ ký quan sát và đọc lại chính sách thực tế quyết định kết quả; chỉ callback hoặc setter chưa đủ.

Provider tùy chỉnh phải giữ các bất biến, hành vi đồng thời/context và quyền sở hữu vòng đời. Observer trạng thái/sự kiện chỉ đọc và best-effort. Nghiệp vụ/audit cần bảo đảm giao nhận phải dùng outbox giao dịch do ứng dụng chủ quản lý.
