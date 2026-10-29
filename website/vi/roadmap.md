# Lộ trình

## SDK hiện tại

Kylok cung cấp thư viện Go và Android cho đăng ký theo scope, phân phối APK đã ký, policy có readback, cấu hình ứng dụng được quản lý, thao tác fleet/lệnh bền vững, PostgreSQL tùy chọn, change feed và chính sách Wi-Fi/CA riêng. Các năng lực cho thiết bị chuyên dụng đã có trong mã nguồn gồm launcher HOME bền vững (`KylokHomeActivity`), phục hồi sau boot và crash (`LauncherRecovery`), đồng bộ nền WorkManager (`EnrollmentWorker`), màn hình báo cáo tuân thủ trên thiết bị (`PolicyComplianceActivity`), và cài đặt APK qua `PackageInstaller`. Chính sách được thực thi trực tiếp ở tầng OS qua API nền tảng chuẩn của Android. Ứng dụng chủ quản lý danh tính, phân quyền, giao diện, vòng đời và nghiệp vụ.

## Công việc tiếp theo

Giao diện phần mềm và bộ kiểm thử tự động cho các mốc N7 đến N12 đã được hoàn thiện trong mã nguồn hiện tại. Điều kiện nghiệm thu đòi hỏi kiểm định thực tế trên thiết bị và đáp ứng các cổng phát hành stable.

| Thứ tự | Năng lực | Trạng thái |
| --- | --- | --- |
| N7 | HOME launcher bền vững, phục hồi sau boot và khởi chạy lại ứng dụng khi bị dừng. | Đã có trong mã nguồn; chờ kiểm định boot/phục hồi thực tế. |
| N8 | Chế độ bảo trì và kiểm tra tuân thủ trên thiết bị có xác thực. | Đã có trong mã nguồn; chờ kiểm định offline thực tế. |
| N9 | Hợp đồng provisioning kết hợp bootstrap Device Owner và metadata liên kết host. | Đã có trong mã nguồn; chờ kiểm định quét một lần thực tế. |
| N10 | Kiosk/Lock Task và hạn chế Wi-Fi thực thi ở tầng OS. | Đã có trong mã nguồn; chờ kiểm định thực thi trên OEM. |
| N11 | Khôi phục đồng bộ khi mạng quản lý kết nối lại và hoãn cập nhật an toàn. | Đã có trong mã nguồn; chờ kiểm định ngắt/nối mạng thực tế. |
| N12 | Messaging nội bộ có interface trung lập và lệnh thiết bị một lần. | Lệnh và hợp đồng đã có trong mã nguồn; chờ đánh giá trạng thái chờ. |
| N13 | Kiểm định hỗ trợ thiết bị/firmware/mạng và phát hành stable. | Chờ hoàn tất các cổng kiểm định stable. |

Khi thiết bị không có route mạng, máy chủ không thể gửi push tới đó. Sau khi kết nối lại, sự kiện trong hàng đợi có giới hạn và đối soát trạng thái bền vững phải phục hồi an toàn. Tốc độ nhận nền còn phụ thuộc Android và firmware OEM.

Xem [thứ tự triển khai](https://github.com/thienvu18/kylok/blob/main/docs/implementation-plan.md), [thiết kế messaging](https://github.com/thienvu18/kylok/blob/main/docs/local-messaging.md) và [tương thích hiện tại](compatibility.html).
