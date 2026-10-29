<div class="hero"><p class="eyebrow">MÃ NGUỒN MỞ · GO + KOTLIN · LGPL-3.0-ONLY</p><h1>Tích hợp quản lý Android.<br>Tập trung vào sản phẩm của bạn.</h1><p>Kylok cung cấp nền tảng để đăng ký thiết bị, phân phối ứng dụng và áp dụng chính sách. SDK Go kết hợp với thư viện Device Policy Controller trên Android, vận hành trên hạ tầng bạn sở hữu, để đội ngũ tập trung xây trải nghiệm phù hợp với người dùng.</p><div class="hero-actions"><a class="button" href="install.html">Bắt đầu với Kylok →</a><a class="button secondary" href="examples.html">Khám phá ứng dụng mẫu</a></div><div class="network"><span>Dịch vụ Go của bạn</span><b>HTTP / HTTPS · mạng của bạn</b><span>Thiết bị Android được quản lý</span></div></div>

## Nền tảng cho trải nghiệm quản lý thiết bị

<div class="features"><article><h3>Đăng ký thiết bị nhanh chóng</h3><p>Tạo QR dùng một lần, gắn thiết bị vào đúng phạm vi quản lý và kết nối tới địa chỉ IP hoặc tên miền qua HTTP/HTTPS.</p></article><article><h3>Phân phối đúng ứng dụng</h3><p>Gán APK đã ký, tiếp tục tải khi gián đoạn và xác minh package, phiên bản cùng chữ ký sau cài đặt.</p></article><article><h3>Biết policy đã áp dụng đến đâu</h3><p>Quản lý ứng dụng bắt buộc, Lock Task, quyền runtime, một số giới hạn và cấu hình ứng dụng có kiểu; xem kết quả riêng cho từng quy tắc.</p></article><article><h3>Giữ quyền kiểm soát trong tay bạn</h3><p>Tích hợp API Go và Android cùng lưu trữ cục bộ bền vững hoặc PostgreSQL tùy chọn. Host giữ quyền phân quyền, định danh, giao diện và vòng đời dịch vụ.</p></article><article><h3>Điều phối thiết bị hiệu quả</h3><p>Nhóm thiết bị, gán ứng dụng hàng loạt có giới hạn, lên lịch triển khai và theo dõi lệnh reboot hoặc khóa bảo mật.</p></article><article><h3>Chủ động về chính sách mạng</h3><p>Tùy chọn quản lý Wi-Fi và chứng chỉ riêng với bước kiểm tra khôi phục; host vẫn làm chủ cấu hình kết nối và độ tin cậy mạng.</p></article></div>

## Xây sản phẩm của bạn trên nền tảng có sẵn

Khám phá web console mẫu để xem cách đăng ký thiết bị, phân phối ứng dụng và quản lý policy. Dashboard Android cung cấp điểm khởi đầu cho trạng thái và thao tác trên thiết bị. Dùng ví dụ để bắt đầu nhanh, rồi phát triển giao diện vận hành và ứng dụng theo sản phẩm của bạn.

[Chạy ứng dụng mẫu](examples.html) · [Tích hợp SDK](install.html) · [Xem tham chiếu API](api.html)

> **Đang thử nghiệm.** Các gói công khai được liệt kê tại [GitHub Releases](https://github.com/thienvu18/kylok/releases). Khi build từ mã nguồn, dùng hai SDK cùng revision. Mức hỗ trợ phụ thuộc thiết bị, firmware và mạng đã kiểm định; xem [tương thích](compatibility.html).

## Mở rộng cho thiết bị chuyên dụng

Mã nguồn hiện tại đã triển khai kiosk tự khởi chạy và phục hồi, kiểm tra tuân thủ trên thiết bị, đồng bộ WorkManager, thực thi chính sách ở tầng OS, và cài đặt APK qua `PackageInstaller`. Lộ trình tiếp theo tập trung vào kiểm định trên thiết bị thực tế, đối soát policy khóa Wi-Fi, hoãn cập nhật ứng dụng an toàn, messaging cục bộ tùy chọn và các cổng phát hành stable. Xem [lộ trình](roadmap.html), [thiết kế messaging](https://github.com/thienvu18/kylok/blob/main/docs/local-messaging.md) và [kế hoạch triển khai](https://github.com/thienvu18/kylok/blob/main/docs/implementation-plan.md).
