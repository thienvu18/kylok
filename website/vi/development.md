# Phát triển và tài liệu tự sinh

Dự án dùng Go 1.27.1, JDK 17, Gradle 9.3.1, Android Gradle Plugin 9.1.1 và Android SDK/build-tools 37. Đặt `JAVA_HOME`, `ANDROID_HOME` và đường dẫn công cụ Android trước. Dependency được ghim bằng Go sums và Gradle lockfile.

```sh
make verify
python3 scripts/check-repository.py
```

Kiểm tra gồm Go race/adapter, hành vi tác nhân JVM, liên thông HTTP/HTTPS, bản cập nhật host đã ký, host Go mới và host Android minify dùng gói SDK. Simulator/build thành công không chứng minh hiệu lực trên Android thật.

## Sinh website này

```sh
python3 -m venv .tools/docs-venv
.tools/docs-venv/bin/python -m pip install -r website/requirements.txt
make docs
```

Tham chiếu Go được trích qua AST từ chữ ký và chú thích hiện tại. Dokka sinh API Android từ KDoc. Hướng dẫn tiếng Anh/Việt và tham chiếu tự sinh được ghép tại `out/site`; workflow GitHub Pages build và triển khai artifact đó. Không sửa HTML đã sinh bằng tay.

Mỗi phương thức public phải có mục đích và cách dùng cụ thể; private/internal phải có chú thích mục đích. Giải thích giá trị giao thức, giới hạn và thứ tự tác động khó suy ra. Chú thích mô tả hành vi, không dùng nhãn giai đoạn phát triển. Không sửa nguồn tự sinh/bên thứ ba để đáp ứng quy tắc chú thích thủ công.

## Đóng góp

Giữ SDK độc lập với ví dụ và framework nghiệp vụ. Bảo toàn xác thực, cô lập phạm vi, tiêu thụ token nguyên tử, tính toàn vẹn APK bất biến và idempotency tác vụ. Tách cấu hình yêu cầu/quan sát/tuân thủ. API và cơ sở dữ liệu phát triển có thể thay đổi; không thêm adapter tương thích chỉ cho bản phát triển cũ. Giữ nguyên bản gốc `design.md` và thông báo bên thứ ba. Mã gốc dùng LGPL-3.0-only.
