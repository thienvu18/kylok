# Gói phát hành

Go phát hành qua tag của `github.com/thienvu18/kylok/go`; Android phát hành lên Maven Central với tọa độ `io.github.thienvu18.kylok:kylok-dpc`. Chọn phiên bản đã có tại [GitHub Releases](https://github.com/thienvu18/kylok/releases). Cặp SDK phát triển dùng cùng một mã nguồn và có thể đổi hợp đồng mà không di trú.

## Quy trình phát hành

1. Tạo PR vào `main` được bảo vệ. Branch phải cập nhật theo `main` và vượt **SDK tests** cùng **Documentation build**.
2. Merge tự động build và ký cả hai SDK, không chạy test trong bước deploy. Phiên bản thêm GitHub run ID, ví dụ `0.1.0-dev.123`.
3. Workflow lưu gói trong bản nháp, phát hành bundle Maven đã ký qua Central Portal, tạo tag bất biến `go/vVERSION` và `android/vVERSION`, yêu cầu Go lập chỉ mục rồi công khai một GitHub prerelease.
4. Hai tag SDK và `release/RUN_ID` trỏ cùng commit nguồn phát hành, có commit cha là commit vừa merge. Chỉ tệp phiên bản, ví dụ tương ứng trong tài liệu và metadata build thay đổi.
5. Workflow lỗi sẽ tiếp tục với artifact và deployment ID đã lưu. Nếu mất phản hồi upload, phải lấy ID thực tế từ Central Portal; không tự upload lại cùng phiên bản.

Consumer công khai không cần tài khoản đọc Maven. Cấu hình `mavenCentral()` và dùng phiên bản đã phát hành. Có thể dùng namespace Maven mới đã xác minh cho bản tương lai; tọa độ cũ không thay đổi.

## Gói cục bộ

```sh
make distribution
make test-consumers
make test-release
```

Gói đầy đủ `out/release-assets/kylok-distribution.zip` gồm Go proxy, AAR/mã nguồn/KDoc/POM/module metadata Maven, mã nguồn tương ứng, giấy phép, manifest dependency và checksum. `kylok-release.json` ghi phiên bản và định danh nguồn. Không phát hành gói từ nguồn chưa commit.

Sau khi phát hành, `KYLOK_REMOTE_SDK=all ./scripts/test-consumers.sh` kiểm tra gói công khai từ nguồn và bản phân phối tương ứng. Đây là bước kiểm tra riêng của maintainer. Bản ổn định v1 vẫn cần toàn bộ điều kiện phần mềm và thiết bị vật lý. Xem [hướng dẫn vận hành phát hành](https://github.com/thienvu18/kylok/blob/main/docs/releases.md) để cấu hình secret, ký và khôi phục.
