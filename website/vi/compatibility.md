# Tương thích và bằng chứng

Nguồn nhắm Android API 33–37, với min SDK 33 và compile/target 37. Khoảng này không phải ma trận thiết bị/firmware đã chứng nhận. Gói Go/Android phát triển ghép từ cùng một mã nguồn; [GitHub Releases](https://github.com/thienvu18/kylok/releases) ghi các phiên bản đã công khai.

[Kiểm tra SDK trên GitHub](https://github.com/thienvu18/kylok/actions/runs/40282303400) và [build tài liệu](https://github.com/thienvu18/kylok/actions/runs/40282303423) đã vượt qua ngày 2026-10-26 với nguồn `4e0730f`. Kết quả chứng minh tích hợp tự động, không chứng nhận thiết bị Android hay xác nhận gói đã có trên registry.

| Bằng chứng | Chứng minh điều gì |
| --- | --- |
| Go race/giao dịch | Binding đăng ký, lưu trữ theo phạm vi, replay, lease và hợp đồng chính sách/tác vụ. |
| Fixture JVM HTTP/HTTPS | Giao thức Go↔Kotlin thực, đăng ký mã hóa, tải có xác thực và báo cáo; tác động nền tảng được mô phỏng. |
| Build/minify/lint Android | Tích hợp nguồn và gói, gồm tài nguyên DPC đã merge và thành phần reflection được giữ. |
| Consumer gói độc lập | Host Go/Android mới tích hợp qua gói, không chép SDK hoặc thay bằng project nguồn. |
| POCO Pad X1, Android 15 / API 35, HyperOS OS2.0.205.0.VOZMIXM | APK phát triển ban đầu: thiết lập QR và đăng ký LAN HTTP có xác thực, **khi WAN vẫn mở**. |

## Kiểm tra thiết bị do maintainer thực hiện

- Đăng ký QR sau factory reset khi chặn WAN suốt thiết lập/tải/đăng ký, nhưng giữ LAN truy cập được.
- Cài/cập nhật ứng dụng im lặng thực, APK bị từ chối/hỏng/không tương thích và luồng cần người dùng xác nhận.
- Mất callback, dừng tiến trình, mất LAN, khởi động lại máy chủ/thiết bị, giữ định danh/checkpoint.
- Thay DPC bằng phiên bản cao hơn cùng khóa ký, giữ Device Owner, định danh và kết nối.
- Vào/thoát kiosk thực, xử lý ứng dụng bắt buộc sai lệch, quyền, hạn chế và khôi phục giá trị ban đầu.

Ghi chính xác nguồn/SDK/APK, digest/chữ ký, model/API/firmware, giao thức và điều kiện mạng. Không công khai QR đăng ký, token, credential, khóa riêng hoặc định danh cá nhân. Bảng trạng thái hiển thị bằng chứng; không tự tạo bằng chứng tương thích thiết bị.
