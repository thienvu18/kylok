# Chính sách và kiosk

Dùng `SetPolicy` để cập nhật chính sách cho thiết bị mà ứng dụng của bạn được phép quản lý. Gọi `GetPolicy` để xem chính sách đã gửi, báo cáo từ thiết bị và kết quả từng quy tắc. Chỉ dùng lại khóa yêu cầu khi gửi lại cùng một chính sách; cùng khóa đó không thể dùng cho nội dung khác.

```json
{
  "required_packages": ["io.example.kiosk"],
  "lock_task": {"packages": ["io.example.kiosk"], "kiosk_package": "io.example.kiosk"},
  "screen_capture_disabled": true,
  "restrictions": [{"name": "no_modify_accounts", "enabled": true}]
}
```

Hãy đăng ký và gán APK trước khi thêm ứng dụng vào `required_packages`. Kiosk chỉ khởi động sau khi thiết bị báo đã cài đúng phiên bản và chữ ký của ứng dụng bắt buộc. Nếu ứng dụng bị gỡ hoặc thay đổi, Kylok tạm dừng kiosk cho đến khi thiết bị xác nhận điều kiện đã được đáp ứng lại.

| Thiết lập | Ý nghĩa |
| --- | --- |
| `required_packages` | Tối đa 64 ứng dụng đã được gán. Bỏ ứng dụng khỏi chính sách không gỡ ứng dụng khỏi thiết bị. |
| `lock_task` | Danh sách ứng dụng được dùng trong kiosk và ứng dụng khởi chạy (nếu có). Kylok xác nhận kiosk sau khi Android báo đã bật Lock Task; ghim màn hình là chưa đủ. |
| `camera_disabled`, `screen_capture_disabled` | Có thể đặt true/false. `false` nghĩa là yêu cầu giữ tính năng ở trạng thái bật. |
| `maximum_time_to_lock_millis` | Giá trị từ 0 đến 86400000. `0` bỏ giới hạn thời gian khóa do admin này đặt. |
| `restrictions` | Các mục `no_add_user`, `no_debugging_features`, `no_modify_accounts`, `no_uninstall_apps`; đặt true hoặc false cho từng mục. |
| `permissions` | Tối đa 64 quy tắc quyền runtime cho ứng dụng bắt buộc: `package_name`, `permission` và `DEFAULT`/`GRANTED`/`DENIED`. Android có thể từ chối cấp quyền không hỗ trợ. |

Mỗi lần gửi `SetPolicy` là một **bản thay thế đầy đủ**. Nếu bỏ một thiết lập Kylok đang quản lý, SDK sẽ khôi phục giá trị ban đầu đã ghi nhận. Gửi `{}` để trả lại toàn bộ thiết lập do Kylok quản lý. `DEFAULT` ngừng quản lý quyền nhưng không thu hồi quyền đã được cấp trước đó. Không thay đổi cùng thiết lập admin ở nơi khác trong lúc Kylok đang quản lý.

Việc áp dụng chính sách có thể mất thời gian. Từng quy tắc báo `APPLIED`, `PENDING`, `FAILED` hoặc `UNSUPPORTED`, kèm lý do. Chính sách đã nhận vẫn có thể tiếp tục áp dụng khi thiết bị ngoại tuyến. Hãy xem phản hồi API là yêu cầu gửi đi, không phải bằng chứng Android đã áp dụng thiết lập. Hiệu lực tùy thiết bị và firmware; hãy kiểm tra trên các thiết bị mục tiêu.
