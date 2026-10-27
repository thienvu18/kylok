<div class="hero"><p class="eyebrow">MÃ NGUỒN MỞ · GO + KOTLIN · LGPL-3.0-ONLY</p><h1>Tích hợp quản lý Android<br>vào sản phẩm của bạn.</h1><p>Kylok kết hợp SDK Go cho máy chủ với thư viện điều khiển thiết bị Android viết bằng Kotlin. Bạn có thể thêm luồng đăng ký, phân phối ứng dụng và áp dụng chính sách mà không phải tự xây mọi thành phần quản lý từ đầu. Hệ thống chạy trên hạ tầng do bạn làm chủ.</p><div class="hero-actions"><a class="button" href="install.html">Bắt đầu với Kylok →</a><a class="button secondary" href="examples.html">Xem Kylok hoạt động</a></div><div class="network"><span>Dịch vụ Go của bạn</span><b>HTTP / HTTPS · mạng của bạn</b><span>Thiết bị Android bạn quản lý</span></div></div>

## Những thành phần để bạn xây sản phẩm

<div class="features"><article><h3>Đăng ký thiết bị theo cách của bạn</h3><p>Tạo QR riêng chỉ dùng một lần cho địa chỉ IP hoặc tên miền đã cấu hình. Mỗi thiết bị được gắn với đúng phạm vi quản lý.</p></article><article><h3>Phân phối ứng dụng, xác nhận kết quả</h3><p>Gán APK đã ký, tiếp tục tải khi gián đoạn và chỉ báo cài đặt thành công sau khi xác minh đúng gói, phiên bản và chữ ký.</p></article><article><h3>Áp dụng chính sách, theo dõi thực tế</h3><p>Quản lý ứng dụng bắt buộc, Android Lock Task, quyền runtime và một số giới hạn. Xem kết quả quan sát được cho từng quy tắc.</p></article><article><h3>Tích hợp vào hệ thống bạn đang dùng</h3><p>Dùng API công khai cho Go và Android, bắt đầu với SQLite/hệ thống tệp bền vững, đồng thời giữ quyền phân quyền và vận hành ở ứng dụng chủ.</p></article></div>

## Xem Kylok hoạt động

Ứng dụng web mẫu hướng dẫn luồng đăng ký, gán APK và thay đổi chính sách, rồi hiển thị kết quả trên thiết bị và tác vụ. Bảng trạng thái Android tách cấu hình được yêu cầu khỏi những gì thiết bị thực sự cài đặt hoặc áp dụng. Hãy dùng ứng dụng mẫu để đánh giá SDK; trải nghiệm quản lý cuối cùng thuộc về sản phẩm của bạn.

[Chạy ứng dụng mẫu](examples.html) · [Tích hợp SDK](install.html) · [Xem tham chiếu API](api.html)

## Tính năng đã kiểm tra trên thiết bị thật

<div class="features"><article><h3>Đăng ký Device Owner</h3><p>Thiết lập QR khi có Internet đã đăng ký POCO thành thiết bị được quản lý và đồng bộ xác thực qua LAN HTTP.</p></article><article><h3>Phân phối ứng dụng đã ký</h3><p>Ứng dụng đã ký được cài/cập nhật từ v1 lên v2 với đúng package và signer. Máy chủ từ chối lần thử lại đổi nội dung và yêu cầu hạ phiên bản.</p></article><article><h3>Cấu hình ứng dụng được quản lý</h3><p>Ứng dụng mẫu độc lập nhận giá trị có kiểu, đổi tiêu đề/nút làm mới và trở về mặc định sau khi gỡ chính sách.</p></article><article><h3>Đồng bộ và khôi phục</h3><p>Đã quan sát đồng bộ nền sau một đêm. Đồng bộ thủ công khôi phục sau khi khởi động lại máy chủ và kết nối lại LAN; sau khi khởi động lại offline, đồng bộ tiếp tục khi bật Wi-Fi mà không cần mở Kylok.</p></article><article><h3>Một số điều khiển thiết bị</h3><p>Camera, ảnh chụp màn hình và sửa tài khoản bị chặn rồi khôi phục; giới hạn debugging đã ẩn Developer options.</p></article></div>

Đây là bằng chứng trên một thiết bị với build nguồn được ghi trong báo cáo, không phải tuyên bố hỗ trợ mọi thiết bị. Android vẫn cho gỡ ứng dụng dù chính sách chặn gỡ đã được báo áp dụng. Đăng ký khi chặn WAN, kiosk và quyền runtime chưa được kiểm định. Xem [báo cáo đầy đủ](compatibility.html).

> **Đang thử nghiệm.** Các gói hiện có được liệt kê tại [GitHub Releases](https://github.com/thienvu18/kylok/releases). Khi build từ mã nguồn, dùng hai SDK cùng revision. Build tự động không chứng minh SDK hoạt động trên mọi thiết bị Android; xem [trạng thái kiểm định](compatibility.html).

Mã nguồn hiện tại có tích hợp host, PostgreSQL, đồng bộ bền vững và cấu hình ứng dụng được quản lý. [Lộ trình](roadmap.html) theo dõi vận hành fleet, quản lý mạng và kiểm định thiết bị.
