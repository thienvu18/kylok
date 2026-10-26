# Ứng dụng mẫu

## Khởi động máy chủ mẫu

```sh
./scripts/prepare-local-server.sh 192.168.101.5 http
./scripts/run-local-server.sh 192.168.101.5 http
```

Ứng dụng mẫu cung cấp Agent API và APK tại `http://192.168.101.5:8080`, đồng thời mở bảng quản trị riêng tại `http://127.0.0.1:8081`. Token của bảng quản trị nằm trong thư mục dữ liệu; log khởi động cho biết đường dẫn. Nhập token rồi chọn phạm vi để bắt đầu. HTTP phù hợp cho thử nghiệm trên LAN tin cậy nhưng không mã hóa dữ liệu. Đây là giao diện mẫu; sản phẩm của bạn cần tự xác thực người dùng và kiểm tra quyền.

Để thiết bị kết nối qua nhiều giao diện mạng IPv4, chạy script với `KYLOK_AGENT_LISTEN=0.0.0.0:8080` hoặc truyền trực tiếp `-listen 0.0.0.0:8080`. Hãy đặt `-public-url` thành địa chỉ mà các thiết bị đó truy cập được. Bảng điều khiển hiển thị những địa chỉ máy chủ nhận kết nối.

## Đăng ký thiết bị và cập nhật địa chỉ QR

1. Mở **Enrollment**, chọn giao diện mạng hoặc nhập IP/tên miền, đặt thời hạn token rồi bấm **Create enrollment QR**.
2. Chọn giao diện hoặc tên miền khác để cập nhật QR nhưng vẫn dùng token một lần ban đầu. Có thể đặt địa chỉ APK riêng nếu máy chủ bootstrap ở nơi khác.
3. Trên máy tính bảng đã khôi phục cài đặt gốc, mở quy trình Android Enterprise bằng QR, kết nối LAN rồi quét mã. Nếu dùng HTTPS, địa chỉ phải khớp chứng chỉ mà Setup Wizard tin cậy trước khi tải APK.
4. Xác nhận thiết bị đã đăng ký xuất hiện trong bảng điều khiển. Hãy giữ bí mật QR và chỉ chia sẻ với thiết bị cần đăng ký.

Đổi địa chỉ QR chỉ ảnh hưởng những lần đăng ký sau; thiết bị hiện có tiếp tục dùng địa chỉ máy chủ đã lưu. Nếu địa chỉ đó vẫn truy cập được, thiết bị tiếp tục tải các APK được gán.

## Thử phân phối ứng dụng và chính sách

- **Overview:** chọn thiết bị để xem model, báo cáo từ agent, danh sách ứng dụng và phiên bản mong muốn/đã báo cáo; có thể chủ động thu hồi quyền truy cập.
- **Applications:** tải APK đã ký, xem metadata đã xác minh, gán APK kèm khóa yêu cầu và thời hạn, hoặc xóa những APK không còn được tham chiếu. DPC có thể tự cập nhật theo luồng này nếu bản mới giữ cùng application ID và khóa ký, đồng thời có version code cao hơn.
- **Policies & kiosk:** chọn mẫu hoặc sửa toàn bộ tài liệu chính sách. Phải gán ứng dụng trước khi yêu cầu chúng. Gửi tài liệu rỗng để trả lại các thiết lập do Kylok quản lý.
- **Operations:** theo dõi lượt tải, tiến độ cài đặt, yêu cầu xác nhận từ người dùng, lỗi và kết quả trên thiết bị. Gửi yêu cầu gán APK hay bắt đầu cài đặt chưa có nghĩa ứng dụng đã cài thành công.

Bảng quản trị dùng token để minh họa một cách bảo vệ giao diện quản lý. Trong sản phẩm của bạn, hãy tự xác định ai được đăng ký thiết bị, gán ứng dụng hoặc đổi chính sách, rồi thực thi quyền đó ở ứng dụng chủ.

## Bảng trạng thái trên Android

Mở ứng dụng mẫu sau khi đăng ký thiết bị. **Overview** hiển thị định danh và trạng thái kết nối. **Applications** phân biệt phiên bản được yêu cầu với phiên bản đã cài. **Policies** cho biết kết quả từng quy tắc. **Operations** hiển thị tiến độ cài đặt và nút mở xác nhận của Android khi cần. **About** hiển thị thông tin ứng dụng, SDK và thiết bị. **Sync now** lên lịch đồng bộ; **Refresh view** đọc trạng thái mới nhất đã lưu. Tác vụ nền vẫn chạy khi không mở màn hình ứng dụng.
