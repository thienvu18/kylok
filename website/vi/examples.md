# Ứng dụng mẫu

## Khởi động bản triển khai cục bộ đã ký

```sh
./scripts/prepare-local-server.sh 192.168.101.5 http
./scripts/run-local-server.sh 192.168.101.5 http
```

API tác nhân và APK dùng `http://192.168.101.5:8080`. Script bật bảng quản trị riêng tại `http://127.0.0.1:8081`; khi chạy trực tiếp `minimal-server serve`, truyền `-ui-listen 127.0.0.1:8081` để bật. Tệp riêng `console-token` nằm trong thư mục dữ liệu; log khởi động hiển thị đường dẫn. Mở bảng quản trị, nhập token và chọn phạm vi quản lý. Đặt `KYLOK_UI_LISTEN=` để tắt bảng quản trị của script. Listener tác nhân công khai của SDK không có API quản trị.

Để nhận kết nối trên nhiều card mạng IPv4, chạy script với `KYLOK_AGENT_LISTEN=0.0.0.0:8080` hoặc truyền trực tiếp `-listen 0.0.0.0:8080`, vẫn giữ `-public-url` là địa chỉ cụ thể mà thiết bị truy cập được. Bảng quản trị liệt kê các địa chỉ thực sự được listener nhận.

## Đăng ký và đổi địa chỉ QR

1. Mở **Enrollment**, chọn card mạng hoặc nhập IP/tên miền, chọn thời hạn rồi bấm **Create enrollment QR**.
2. Chọn địa chỉ/tên miền khác để vẽ lại cùng token một lần. Có thể dùng địa chỉ APK riêng cho bootstrap độc lập.
3. Trên máy tính bảng đã đặt lại, mở thiết lập QR Android Enterprise, kết nối LAN rồi quét. IP/tên miền HTTPS phải khớp chứng chỉ tin cậy; cấu hình tin cậy trong ứng dụng không thay đổi Setup Wizard trước khi tải APK.
4. Kiểm tra bản ghi thiết bị đã xác thực. QR chứa thông tin bí mật; chỉ dùng bản tải riêng cho đăng ký.

Đổi QR chỉ ảnh hưởng lần thiết lập mới, không đổi endpoint của thiết bị đã đăng ký. Đường dẫn APK được giải theo origin đã đăng ký, nên alias card mạng/tên miền tiếp tục hoạt động sau thiết lập.

## Minh họa ứng dụng và chính sách

- **Overview:** chọn thiết bị; xem model, báo cáo chủ sở hữu, ứng dụng và phiên bản cấu hình; chủ động thu hồi quyền truy cập.
- **Applications:** tải APK đã ký, xem metadata xác minh, gán bằng khóa yêu cầu/thời hạn và chỉ xóa bản không còn tham chiếu. DPC phiên bản cao hơn, giữ định danh/khóa ký, dùng cùng luồng để tự cập nhật.
- **Policies & kiosk:** chọn mẫu hoặc sửa tài liệu thay thế đầy đủ. Ứng dụng bắt buộc phải được gán trước. Tài liệu rỗng trả các điều khiển do Kylok quản lý về trạng thái ban đầu.
- **Operations:** theo dõi tải, phiên cài đặt/yêu cầu người dùng, lỗi và kết quả gói đã xác nhận. Gán ứng dụng hoặc commit phiên cài đặt chưa phải thành công.

Bảng quản trị dùng bearer token rõ ràng, thao tác trình duyệt cùng origin và API SDK có phạm vi. Tài nguyên giao diện được nhúng, không phụ thuộc CDN. Token quản trị là logic minh họa của ứng dụng chủ; sản phẩm thực tế tự quản lý phân quyền nghiệp vụ.

## Bảng trạng thái Android

Mở ứng dụng sau provisioning. **Overview** hiển thị định danh, endpoint, quyền và checkpoint. **Applications** tách phiên bản yêu cầu/quan sát. **Policies** hiển thị từng quy tắc. **Operations** hiển thị tiến độ và nút xác nhận OS khi cần. **About** hiển thị định danh ứng dụng/SDK/thiết bị. **Sync now** lên lịch tác vụ mạng; **Refresh view** đọc trạng thái cục bộ. Nhãn ứng dụng Android dùng tiếng Anh; tác nhân nền không phụ thuộc việc mở giao diện.
