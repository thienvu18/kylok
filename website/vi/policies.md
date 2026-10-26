# Chính sách và kiosk

Dùng `SetPolicy` để thay chính sách yêu cầu của thiết bị được phân quyền và `GetPolicy` để xem cấu hình yêu cầu, báo cáo và tuân thủ từng quy tắc. Dùng lại khóa yêu cầu với cùng tài liệu chuẩn hóa là idempotent; nội dung khác dưới cùng khóa sẽ xung đột.

```json
{
  "required_packages": ["io.example.kiosk"],
  "lock_task": {"packages": ["io.example.kiosk"], "kiosk_package": "io.example.kiosk"},
  "screen_capture_disabled": true,
  "restrictions": [{"name": "no_modify_accounts", "enabled": true}]
}
```

Đăng ký và gán APK bắt buộc trước. Đúng phiên bản/chữ ký được yêu cầu là điều kiện để vào kiosk. Khi chúng biến mất hoặc sai lệch, tác nhân tạm dừng khóa kiosk đến khi quan sát được kết quả sửa chữa.

| Thiết lập | Ý nghĩa |
| --- | --- |
| `required_packages` | Các ứng dụng đã được gán, tối đa 64. Bỏ khỏi chính sách không gỡ cài đặt. |
| `lock_task` | Danh sách cho phép và ứng dụng khởi chạy; phải quan sát được khóa quản lý thực, không chỉ ghim màn hình. |
| `camera_disabled`, `screen_capture_disabled` | Boolean tùy chọn; false là một yêu cầu rõ ràng. |
| `maximum_time_to_lock_millis` | 0–86400000; 0 bỏ hạn chế thời gian khóa do admin này đặt. |
| `restrictions` | `no_add_user`, `no_debugging_features`, `no_modify_accounts`, `no_uninstall_apps`, mỗi mục có boolean rõ ràng. |
| `permissions` | Tối đa 64 quy tắc runtime cho ứng dụng bắt buộc: `package_name`, `permission` và `DEFAULT`/`GRANTED`/`DENIED`. Android có thể từ chối quyền không hỗ trợ. |

Đây là **thay thế đầy đủ**. Bỏ một thiết lập từng quản lý sẽ khôi phục giá trị ban đầu đã lưu. `{}` trả toàn bộ điều khiển của Kylok. `DEFAULT` bỏ chính sách quyền, không thu hồi quyền đã cấp. Ứng dụng chủ không được đồng thời thay cùng thiết lập admin khi Kylok quản lý.

Từng quy tắc báo `APPLIED`, `PENDING`, `FAILED` hoặc `UNSUPPORTED`, kèm lý do. Nhật ký giá trị ban đầu/lần thử xử lý gián đoạn và khôi phục. Chính sách đã xác thực vẫn được đối chiếu khi mất máy chủ. Callback hệ thống của lần khởi động hiện tại cùng khóa quản lý thực xác nhận kiosk. Hiệu lực trên firmware OEM vẫn cần kiểm tra thiết bị.
