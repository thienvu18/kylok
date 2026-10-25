# KYLOK

Architecture & Implementation Plan — v1.0 Draft

Kylok — Self-hosted Android Device Management SDK

Kylok là một framework quản lý thiết bị Android theo hướng library-first, local-first, host-neutral và extensible, sử dụng các Android Enterprise APIs chính thức và lấy [Google Test DPC](https://github.com/googlesamples/android-testdpc?utm_source=chatgpt.com) làm reference implementation.

Kylok được xây dựng trong một monorepo, với hai sản phẩm phát hành chính và hai ứng dụng minh họa:

| Thành phần       | Loại                        | Vai trò                                                                     |
| ---------------- | --------------------------- | --------------------------------------------------------------------------- |
| Kylok Go         | Go module                   | Management engine, enrollment, policies, deployment, device registry        |
| Kylok Android    | Android AAR                 | DPC lifecycle, provisioning, enrollment, policy execution, APK installation |
| `minimal-server` | Example Go application      | Server hoàn chỉnh tối giản, nhúng Kylok Go                                  |
| `minimal-dpc`    | Example Android application | Host APK tối giản, nhúng Kylok AAR                                          |

fDCMS là ứng dụng đầu tiên dự kiến sử dụng Kylok, nhưng không thuộc monorepo và không được trở thành dependency của Kylok.

## 1. Các nguyên tắc kiến trúc

### 1.1. Ready to use, không phải zero-config

Đây là nguyên tắc quan trọng nhất.

Một SDK ready-to-use phải cung cấp sẵn implementation cho những thành phần có thể chuẩn hóa. Nhưng mọi cấu hình bắt buộc phải được yêu cầu rõ ràng, không được tự đoán hoặc đặt default thiếu an toàn.

Ví dụ:

```
manager, err := kylok.New(kylok.Options{
    PublicURL: "https://mdm.example.com",
    DataDir:   "/var/lib/kylok",
    Bootstrap: kylok.BootstrapAPK{
        Path:     "./minimal-dpc-release.apk",
        Metadata: "./minimal-dpc-release.json",
    },
})
```

Sau khi constructor thành công, Kylok phải có sẵn enrollment engine, SQLite persistence, QR generator, device registry, policy engine, APK storage và Agent HTTP handlers.

Không cần host tự implement bất kỳ interface nào cho đường sử dụng mặc định.

Tuy nhiên, `PublicURL`, nơi lưu trạng thái bền vững và artifact DPC bootstrap là những đầu vào bắt buộc cho cấu hình all-in-one này.

Public URL phải là địa chỉ mà Android Setup Wizard và Agent truy cập được qua HTTPS. Go SDK không thể tự bảo đảm TLS trust nếu host chưa cấu hình HTTPS server hoặc reverse proxy hợp lệ.

### 1.2. Convention over configuration

Thứ gì có default hợp lý thì tự cấu hình, chẳng hạn database migrations, policy reconciler, retry strategy, local artifact store, logging và protocol handling.

Thứ gì không thể xác định an toàn thì bắt buộc cung cấp, chẳng hạn địa chỉ server, signed DPC APK, vùng lưu dữ liệu hoặc custom backend thay thế.

### 1.3. Extensible without mandatory interfaces

Người sử dụng có thể thay default implementations bằng những interface riêng, nhưng không buộc phải làm điều đó.

```
manager, err := kylok.New(kylok.Options{
    PublicURL: "https://mdm.example.com",
    Bootstrap: bootstrap,

    Store:       myPostgresStore,
    Artifacts:   myArtifactStore,
    Events:      myEventSink,
    Authorizer:  myAuthorizer,
})
```

Ví dụ này sử dụng custom storage; vì vậy `DataDir` không bắt buộc nếu các dependency cần persistence đã được cung cấp đầy đủ. Constructor phải kiểm tra sự đầy đủ đó, thay vì ngầm tạo một database khác.

### 1.4. Native-first

- Android dùng `DevicePolicyManager` và `PackageInstaller`.
- Provisioning sử dụng Android QR provisioning contract.
- Kotlin sử dụng Android Keystore, Room và WorkManager khi phù hợp.
- Go sử dụng `context.Context`, `net/http`, `database/sql` và Go errors.
- Giao thức chung sử dụng Protobuf.

Không tạo một abstraction mới chỉ để bọc lại một API chuẩn mà không mang lại lợi ích rõ ràng.

### 1.5. Offline-first

Không yêu cầu Firebase, Google Cloud, Google Play hoặc kết nối WAN trong quá trình hoạt động thường xuyên.

Điều này khác với cam kết mọi firmware đều provision được hoàn toàn offline. Android Setup Wizard vẫn do Android/OEM kiểm soát; hỗ trợ môi trường mạng đóng phải được kiểm chứng trên thiết bị thật.

### 1.6. Secure by default

Không sử dụng insecure TLS, anonymous device access hoặc signing verification bypass làm default.

Không tự tạo lại các cơ chế mã hóa tiêu chuẩn nếu nền tảng đã có implementation phù hợp.

### 1.7. Independent lifecycle

- Go module và Android AAR có version riêng.
- Giao thức dùng chung phải tương thích qua các phiên bản phát hành được công bố hỗ trợ.
- Host Android sở hữu application ID và signing key.
- Kylok không sở hữu vòng đời phát hành ứng dụng nghiệp vụ.

### 1.8. Small public API, complete behavior

Public API cần đơn giản nhưng cung cấp đầy đủ chức năng quản lý thiết bị. Nội bộ có thể phức tạp; độ phức tạp không được chuyển hết cho host.

## 2. Phạm vi hỗ trợ và các ràng buộc

Minimum Android API

# 33

Android 13

Target / Compile API

# 37

Android 17

Management mode

## DO

Fully Managed

API 33–37 là phạm vi thiết kế và kiểm thử mục tiêu của v1. Các model/firmware được hỗ trợ chính thức phải dựa trên kết quả kiểm thử thực tế.

### Vì sao chọn API 33?

Android 13 giới thiệu `EXTRA_PROVISIONING_ALLOW_OFFLINE`, đáp ứng yêu cầu triển khai trong closed-network environment. Android 12 đã thay đổi quy trình provisioning sang các activity contracts mới mà Kylok cũng phải tuân thủ.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://developer.android.com\&sz=32)

developer.android.com

+1



Phạm vi này đủ để tránh duy trì các legacy provisioning flows từ Android cũ, đồng thời vẫn hỗ trợ nhiều thiết bị hơn so với chỉ Android 17.

### Những ràng buộc kỹ thuật đã biết

| Ràng buộc                                                              | Thiết kế xử lý                                                                                |
| ---------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| Device Owner phải là một APK                                           | AAR nhúng vào host APK; host APK trở thành Device Owner                                       |
| Device Owner thường cần thiết lập trong quá trình provisioning ban đầu | QR provisioning trên thiết bị factory-reset hoặc đủ điều kiện                                 |
| DPC chưa được cài ở bước đầu                                           | Android Setup Wizard tải signed host APK từ local server                                      |
| Android 17 bảo vệ truy cập LAN                                         | Kylok khai báo và xử lý `ACCESS_LOCAL_NETWORK` trước Agent RPC                                |
| Android 17 bật Certificate Transparency theo mặc định                  | Hỗ trợ cấu hình trust/CT theo domain trên Agent; bootstrap vẫn phải được Setup Wizard tin cậy |
| Offline provisioning tùy firmware                                      | Có compatibility matrix và bài kiểm thử WAN-blocked                                           |
| Silent installation cần Device Owner                                   | Validate role trước khi triển khai APK                                                        |
| Server không luôn kết nối trực tiếp tới tablet                         | Agent chủ động sync, không phụ thuộc inbound TCP                                              |
| Android hạn chế background execution                                   | Event-driven sync kết hợp WorkManager; không cam kết realtime                                 |
| Host app có thể custom sâu                                             | Extension points có semantics, error handling và lifecycle rõ ràng                            |

Android 17 cho phép Device Owner cấp trước `ACCESS_LOCAL_NETWORK` qua `setPermissionGrantState()`. Kylok phải xử lý bootstrap permission trước lần giao tiếp mạng đầu tiên và kiểm thử đường xử lý này trên API 37.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://developer.android.com\&sz=32)

developer.android.com

+1



Một ràng buộc mới khác là Android developer verification. Theo tài liệu hiện tại của Google, private apps trên fully managed devices được miễn yêu cầu xác minh dành cho unmanaged devices. Tuy nhiên, bước cài DPC trước khi enrollment hoàn tất vẫn cần kiểm thử riêng trên firmware được hỗ trợ.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://support.google.com\&sz=32)

Android Enterprise Help



## 3. Kiến trúc tổng thể

Go Host Application

fDCMS / minimal-server / third-party

Kylok Go SDK

Management API

Enrollment · Policies · Deployment

Engine · Default Adapters · Hooks

Android Host APK

minimal-dpc / custom host app

Kylok Android AAR

Provisioning · Enrollment

Policy Reconciler · APK Installer

Android Platform Adapters

Protobuf + gRPC / HTTPS · Agent-initiated

Server Infrastructure

SQLite / Custom Store · APK Repository · TLS

Android OS

DevicePolicyManager · PackageInstaller · Keystore

### Boundary của từng thành phần

| Thành phần         | Sở hữu                                                                         | Không được phụ thuộc                     |
| ------------------ | ------------------------------------------------------------------------------ | ---------------------------------------- |
| Protobuf contracts | Wire messages, Agent RPC, versioning                                           | Go implementation, Kotlin implementation |
| Kylok Go           | Device management semantics, state, enrollment, policy, artifact orchestration | Android app internals, fDCMS             |
| Kylok Android      | Device Owner lifecycle, provisioning, policy execution, local installation     | Go internals, fDCMS                      |
| Go host            | Business authorization, server lifecycle, UI, business data                    | Android implementation                   |
| Android host       | APK identity, signing, optional UI và integrations                             | Go implementation                        |
| Android OS         | Device Owner enforcement, APK installation, restrictions                       | Kylok business logic                     |

Protobuf là shared contract, không phải shared business implementation.

Go và Android đều có domain model riêng phù hợp với ngôn ngữ của mình. Wire DTO không cần trở thành public model của mọi API.

## 4. Monorepo directory structure

Tôi đề xuất giữ cấu trúc tương đối phẳng, tránh chia nhỏ Go modules và Android Gradle modules quá sớm.

```
kylok/
│
├── proto/
│   └── kylok/v1/
│       ├── common.proto
│       ├── enrollment.proto
│       ├── device.proto
│       ├── policy.proto
│       ├── application.proto
│       ├── artifact.proto
│       ├── operation.proto
│       ├── agent_service.proto
│       └── management_service.proto
│
├── go/
│   ├── go.mod
│   ├── kylok.go
│   ├── options.go
│   ├── errors.go
│   │
│   ├── enrollment/
│   ├── provisioning/
│   │   └── qr/
│   ├── device/
│   ├── policy/
│   ├── application/
│   ├── operation/
│   │
│   ├── ports/
│   │   ├── storage.go
│   │   ├── artifact.go
│   │   ├── authorization.go
│   │   └── events.go
│   │
│   ├── adapters/
│   │   ├── sqlite/
│   │   ├── memory/
│   │   └── filesystem/
│   │
│   ├── transport/
│   │   └── connect/
│   │
│   ├── gen/
│   │   └── kylok/v1/
│   │
│   └── internal/
│       ├── engine/
│       ├── identity/
│       ├── state/
│       └── reconcile/
│
├── android/
│   ├── build.gradle.kts
│   ├── settings.gradle.kts
│   ├── gradle/
│   │
│   └── kylok-dpc/
│       ├── build.gradle.kts
│       └── src/
│           ├── main/
│           │   ├── AndroidManifest.xml
│           │   └── kotlin/.../
│           │       ├── api/
│           │       ├── provisioning/
│           │       ├── enrollment/
│           │       ├── policy/
│           │       ├── application/
│           │       ├── sync/
│           │       ├── storage/
│           │       ├── transport/
│           │       └── platform/
│           ├── test/
│           └── androidTest/
│
├── examples/
│   ├── minimal-server/
│   │   ├── go.mod
│   │   ├── main.go
│   │   ├── config.example.yaml
│   │   └── README.md
│   │
│   └── minimal-dpc/
│       ├── build.gradle.kts
│       ├── src/main/
│       │   ├── AndroidManifest.xml
│       │   └── kotlin/.../
│       └── README.md
│
├── tests/
│   ├── contract/
│   ├── integration/
│   ├── provisioning/
│   ├── compatibility/
│   └── fixtures/
│
├── tools/
│   └── apk-metadata/
│
├── docs/
│   ├── architecture.md
│   ├── public-api.md
│   ├── protocol.md
│   ├── provisioning.md
│   ├── security.md
│   ├── compatibility.md
│   ├── integration-guide.md
│   ├── roadmap.md
│   └── adr/
│
├── .github/
│   └── workflows/
│
├── buf.yaml
├── buf.gen.yaml
├── go.work
├── Makefile
├── README.md
├── CONTRIBUTING.md
├── LICENSE
└── NOTICE
```

### Quy tắc tổ chức

Go: Một Go module chính trong `go/`. `examples/minimal-server` có module riêng dành cho example và cùng tham gia `go.work` khi phát triển. Production consumer không phụ thuộc workspace này.

Android: Ban đầu chỉ có một library Gradle module `:kylok-dpc`, xuất bản một AAR. Source code có thể tách thành các package nội bộ nhưng chưa cần nhiều AAR. `minimal-dpc` được include như một application module trong Gradle settings.

Protobuf: Chỉ một schema source trong `proto/`. Generate Go code vào `go/gen`, Kotlin protobuf vào Android build directory. CI kiểm tra generation và backward compatibility.

Tools: `apk-metadata` là công cụ hỗ trợ build/CI, xác minh APK đã ký và tạo manifest metadata để server không phải nhận checksum hay component name được nhập thủ công.

Examples: Hai ứng dụng phải sử dụng đúng public APIs đã phát hành, không import bất kỳ package `internal/` nào và không có special-case trong Kylok chỉ để example chạy được.

### Dependency direction

`proto/kylok/v1`

Single source of wire contracts

Go SDK

Generated Go · Engine · Adapters

`minimal-server`

Android AAR

Generated Kotlin · DPC · OS APIs

`minimal-dpc`

Không có dependency từ Go SDK sang Android AAR, từ Android AAR sang Go SDK, hay từ cả hai sang examples.

## 5. Go SDK — Public API và constructor

### 5.1. Required options

Tôi đề xuất phân chia constructor options thành ba nhóm.

| Option            | Bắt buộc                              | Default / Validation                           |
| ----------------- | ------------------------------------- | ---------------------------------------------- |
| `PublicURL`       | Có                                    | Phải là HTTPS URL truy cập được từ tablet      |
| `Bootstrap`       | Có đối với full MDM instance          | Signed host DPC APK và metadata hợp lệ         |
| `DataDir`         | Có nếu dùng default storage           | Thư mục persistent writable                    |
| `Store`           | Nếu không dùng default store          | Custom implementation đầy đủ                   |
| `Artifacts`       | Nếu không dùng default artifact store | Custom implementation đầy đủ                   |
| `Logger`          | Không                                 | `slog.Default()`                               |
| `Clock`           | Không                                 | System clock                                   |
| `Events`          | Không                                 | No-op sink                                     |
| `Authorizer`      | Không đối với in-process API          | HTTP management exposure phải có authorization |
| `EnrollmentTTL`   | Không                                 | Default an toàn, ví dụ 15 phút                 |
| `SyncInterval`    | Không                                 | Default định sẵn, có giới hạn                  |
| `PolicyValidator` | Không                                 | Built-in validator                             |
| `Transport`       | Không                                 | Built-in gRPC-compatible HTTP handler          |

Quy tắc: Nếu đã cung cấp đủ custom providers thì không yêu cầu `DataDir`. Nếu thiếu một dependency không thể tự dựng an toàn, constructor trả lỗi rõ ràng.

Không ngầm tạo temporary database, không tự sinh một HTTP public endpoint, không tự chọn localhost làm địa chỉ provisioning.

### 5.2. Default initialization

API định hướng:

```
manager, err := kylok.New(kylok.Options{
    PublicURL: "https://mdm.example.com",
    DataDir:   "/var/lib/kylok",
    Bootstrap: kylok.BootstrapAPK{
        APKPath:      "./minimal-dpc-release.apk",
        MetadataPath: "./minimal-dpc-release.json",
    },
})
if err != nil {
    return err
}
defer manager.Close()
```

Constructor sẽ thực hiện các bước đồng bộ:

1. Validate required options và conflict giữa providers.
2. Kiểm tra bootstrap APK, checksum, package identity, metadata.
3. Khởi tạo SQLite và chạy migrations cần thiết.
4. Khởi tạo local content-addressed artifact store.
5. Khởi tạo enrollment authority và credential persistence.
6. Wire management services, policy engine, device registry và handlers.
7. Trả về ready instance.

Constructor không tự động bind network socket, tự động bắt đầu một HTTPS server hay cài certificate lên hệ điều hành. Đó là trách nhiệm của host.

Nếu trạng thái lưu trữ hỏng, migration lỗi hoặc bootstrap artifact sai, `New` phải fail-fast.

### 5.3. Host-facing API

Tôi đề xuất một facade duy nhất:

```
manager.Enrollments()
manager.Provisioning()
manager.Devices()
manager.Policies()
manager.Applications()
manager.Operations()
```

Ví dụ:

```
enrollment, err := manager.Enrollments().
    Create(ctx, kylok.CreateEnrollmentRequest{
        ScopeID: "default",
    })

qr, err := manager.Provisioning().
    GenerateQR(ctx, enrollment.ID)

devices, err := manager.Devices().
    List(ctx, kylok.DeviceFilter{})

policy, err := manager.Policies().
    Create(ctx, policyRequest)

err = manager.Devices().
    AssignPolicy(ctx, deviceID, policy.ID)
```

Đây là public API draft, không phải code đã implement. Trước khi chốt v1 cần hoàn thiện types, validation, pagination, idempotency và error contracts.

Nên sử dụng Go idiomatic domain types cho phần host-facing. Protobuf-generated messages nằm ở transport boundary để không khóa public API của Go vào một representation cụ thể.

### 5.4. HTTP integration

Framework cung cấp handler có sẵn:

```
mux := http.NewServeMux()

mux.Handle(
    "/kylok/",
    manager.AgentHandler(),
)

mux.Handle(
    "/kylok/artifacts/",
    manager.ArtifactHandler(),
)
```

Các path là ví dụ API; implementation nên cung cấp route registration helper để host không phải tự phối hợp từng route.

Management operations sử dụng Go public API trực tiếp. Nếu cần HTTP Management API, Kylok có thể cung cấp handler tùy chọn, nhưng không tự động mở management endpoints ra mạng.

Khi mount Management API, bắt buộc có authentication và authorization provider phù hợp. Nếu thiếu, handler construction phải thất bại thay vì mở anonymous administrative endpoints.

### 5.5. Extension interfaces

| Interface / Hook         | Default                       | Customization                                          |
| ------------------------ | ----------------------------- | ------------------------------------------------------ |
| `StateStore`             | SQLite                        | PostgreSQL, custom DB                                  |
| `ArtifactStore`          | Local filesystem              | S3, MinIO, custom HTTP backend                         |
| `EnrollmentPolicy`       | Token-based enrollment        | Quy tắc chấp thuận thiết bị tùy chỉnh                  |
| `ManagementAuthorizer`   | Không dùng cho in-process API | RBAC, Cerbos, custom ACL                               |
| `EventSink`              | No-op                         | Queue, event bus, audit integration                    |
| `Logger`                 | `slog`                        | Host logger                                            |
| `Clock`                  | System clock                  | Deterministic testing                                  |
| `PolicyValidator`        | Built-in validation           | Bổ sung business rules                                 |
| `DeviceMetadataProvider` | Empty/custom-free             | Map device với tài sản, site hoặc business identifiers |

Mỗi extension point phải có hợp đồng riêng về concurrency, timeout, error propagation và lifecycle.

Các hooks không được phép vô hiệu hóa những security invariants như xác minh enrollment token, device identity hoặc tính toàn vẹn APK.

Không nên xây một plugin framework tổng quát quá sớm. Chỉ tạo interface khi có một use case thực tế cần thay implementation.

## 6. Android AAR — Public API và host integration

### 6.1. Default integration

Android SDK cần có khả năng hoạt động với một host APK tối giản:

```
dependencies {
    implementation("io.kylok:kylok-dpc:<version>")
}
```

Maven coordinates trên là định hướng, chỉ được chính thức hóa sau khi chọn publishing namespace.

AAR sẽ đóng góp qua manifest merger:

- `DeviceAdminReceiver`.
- Provisioning mode Activity.
- Admin policy compliance Activity.
- Package installation result Receiver.
- Boot/lifecycle receivers cần thiết.
- Các permissions và resources phù hợp.

Kylok phải khởi tạo được từ Android component lifecycle mà không phụ thuộc host Activity được mở trước.

Thư viện sẽ có built-in initialization, đồng thời cho phép host cấu hình sớm trong `Application.onCreate()`.

### 6.2. Public Android API

Đường sử dụng mặc định:

```
val kylok = Kylok.get(applicationContext)
```

Không cần cấu hình server URL thủ công nếu thiết bị được provision từ Kylok QR hợp lệ. URL và enrollment bootstrap data được cung cấp qua Android provisioning extras.

Host muốn tùy chỉnh:

```
class MyApplication : Application() {
    override fun onCreate() {
        super.onCreate()

        Kylok.configure(this) {
            events(myEventListener)
            logger(myLogger)
            policyExtensions(myPolicyExtensions)
        }
    }
}
```

Các hàm trên là API sketch. `configure` phải chạy trước khi SDK bắt đầu các hoạt động cần hooks đó; Kylok cần định nghĩa rõ điểm freeze cấu hình và tránh race giữa automatic initialization với host customization.

Sau khi initialized, host có thể truy cập:

```
val status = Kylok.get(context).status.current()
val enrollment = Kylok.get(context).enrollment.current()
val policy = Kylok.get(context).policy.current()
```

Public Android API chủ yếu là read-only state, lifecycle, diagnostics và hooks. Không khuyến khích host gọi trực tiếp các privileged DevicePolicyManager operations xuyên qua Kylok nếu có policy tương đương.

### 6.3. Android SDK internal structure

| Package        | Trách nhiệm                                     |
| -------------- | ----------------------------------------------- |
| `api`          | Kylok facade, options, lifecycle                |
| `provisioning` | Device Admin receiver, provisioning activities  |
| `enrollment`   | Token exchange, credential persistence          |
| `policy`       | Desired-state reconciler, capability validation |
| `application`  | Download, verify, PackageInstaller sessions     |
| `sync`         | Agent RPC, backoff, status reporting            |
| `storage`      | Room database, durable local state              |
| `transport`    | Generated protocol và gRPC client               |
| `platform`     | Android native API integrations                 |

Google Test DPC được dùng để tham khảo hành vi và implementation của các privileged operations, không được đưa toàn bộ sample application vào AAR. Test DPC được Google mô tả là implementation reference cho các DPC khác và có ví dụ QR provisioning với APK tự host.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://github.com\&sz=32)

GitHub



### 6.4. Required Android host responsibilities

Một thư viện AAR không thể tự cung cấp tất cả các điều kiện bên ngoài ứng dụng. Host Android vẫn bắt buộc chịu trách nhiệm:

| Yêu cầu                              | Chủ sở hữu                 |
| ------------------------------------ | -------------------------- |
| Unique application ID                | Host                       |
| Stable APK signing key               | Host                       |
| Build signed APK                     | Host                       |
| Publish APK tới bootstrap repository | Host / deployment pipeline |
| Validate merged manifest             | Kylok tooling + Host CI    |
| Device Owner implementation          | Kylok AAR                  |
| Provisioning callbacks               | Kylok AAR                  |
| Policy reconciliation                | Kylok AAR                  |
| Silent APK installation              | Kylok AAR                  |
| Business UI                          | Host, optional             |

Kylok nên cung cấp Gradle validation task để phát hiện các lỗi integration: thiếu required components, manifest conflicts, minSdk sai, thiếu permissions hoặc metadata không khớp với signed APK.

### 6.5. AAR không đồng nghĩa ứng dụng đã được quản lý

`Kylok.get(context)` có thể tạo một SDK instance hợp lệ trong ứng dụng bình thường, nhưng instance đó chưa có Device Owner privileges.

Phải phân biệt trạng thái:

```
UNMANAGED
PROVISIONING
DEVICE_OWNER
ENROLLING
MANAGED
SUSPENDED
REVOKED
ERROR
```

Ví dụ, nếu APK được cài thủ công rồi gọi `Kylok.get()`, SDK không được tự tuyên bố đã managed hoặc âm thầm cố chiếm Device Owner.

Không có cơ chế nào trong AAR cho phép vượt qua giới hạn provisioning của Android.

## 7. Protobuf contracts và resource model

### 7.1. Resource model

Tôi đề xuất lấy các khái niệm của Android Management API làm reference, nhưng chỉ triển khai các semantics thực sự cần thiết.

| Resource           | Vai trò                                                       |
| ------------------ | ------------------------------------------------------------- |
| `Scope`            | Namespace quản lý độc lập, có thể map tới tenant của host     |
| `Enrollment`       | Enrollment token, TTL, bootstrap metadata, status             |
| `Device`           | Identity, OS, Agent version, lifecycle, reported state        |
| `Policy`           | Desired state, revision, supported configurations             |
| `Application`      | Package identity, required version, installation requirements |
| `Artifact`         | Immutable APK content, digest, signing metadata               |
| `Operation`        | Durable operation và kết quả thực thi                         |
| `ComplianceReport` | Policy status, failures, unsupported fields                   |

`Scope` là namespace của Kylok, không nhất thiết là tenant. Host quyết định map các khái niệm business của mình sang scope.

### 7.2. Hai API surfaces

AgentService do Android AAR sử dụng:

```
syntax = "proto3";

package kylok.v1;

service AgentService {
  rpc Enroll(EnrollRequest)
      returns (EnrollResponse);

  rpc Sync(SyncRequest)
      returns (SyncResponse);

  rpc ReportStatus(ReportStatusRequest)
      returns (ReportStatusResponse);

  rpc ReportOperation(ReportOperationRequest)
      returns (ReportOperationResponse);
}
```

ManagementService là remote interface tùy chọn cho host hoặc management tooling:

```
service ManagementService {
  rpc CreateEnrollment(CreateEnrollmentRequest)
      returns (CreateEnrollmentResponse);

  rpc GetDevice(GetDeviceRequest)
      returns (GetDeviceResponse);

  rpc ListDevices(ListDevicesRequest)
      returns (ListDevicesResponse);

  rpc UpsertPolicy(UpsertPolicyRequest)
      returns (UpsertPolicyResponse);

  rpc AssignPolicy(AssignPolicyRequest)
      returns (AssignPolicyResponse);

  rpc RegisterArtifact(RegisterArtifactRequest)
      returns (RegisterArtifactResponse);
}
```

Public Go API không bắt buộc gọi vòng qua `ManagementService`. Engine được sử dụng trực tiếp trong cùng process. Management RPC chỉ là transport adapter.

### 7.3. Protocol design rules

- Protobuf message fields phải được typed rõ ràng.
- Không serialize toàn bộ Policy bằng generic JSON.
- Có `protocolVersion`, `agentVersion` và capability report.
- Dùng policy revisions thay cho việc so sánh JSON tùy ý.
- Dùng stable IDs cho devices, enrollments và operations.
- Có structured error codes và retryability classification.
- Command/operation phải có idempotency key.
- Không tái sử dụng Protobuf field numbers đã bị xóa.
- Mỗi breaking change phải được Buf kiểm tra.
- Version protocol độc lập với version Go và Android libraries.

### 7.4. Transport

Default transport:

| Hạng mục          | Lựa chọn                           |
| ----------------- | ---------------------------------- |
| Wire format       | Protobuf                           |
| Agent RPC         | gRPC unary                         |
| Go server         | Connect-Go, hỗ trợ gRPC            |
| Android client    | gRPC-Java / OkHttp / Protobuf Lite |
| APK transfer      | HTTPS streaming/download           |
| Network direction | Agent → Server                     |
| Push notification | Không bắt buộc ở v1                |

Không truyền APK bytes trong protobuf management operations. Artifact download sử dụng HTTPS endpoint riêng, tối ưu cho streaming, checksum verification và retry.

### 7.5. Policy model

Policy và Operation phải độc lập.

Policy là desired state. Kylok liên tục cố gắng đưa thiết bị về trạng thái được yêu cầu.

Operation là một thao tác có kết quả và vòng đời riêng. Ví dụ reboot hay lock trong roadmap, hoặc deployment operation nội bộ.

Đối với policy, cần có ít nhất ba trạng thái tách biệt:

- `Desired`: Server yêu cầu gì.
- `Observed`: Android hiện đang ở trạng thái nào.
- `Compliance`: Desired và observed có khớp hay không.

Các kết quả `APPLIED`, `PENDING`, `FAILED` và `UNSUPPORTED` phải phản ánh đúng tình trạng thực tế. RPC thành công không đồng nghĩa với Android đã áp dụng policy.

## 8. Sequence diagrams

### 8.1. Flow A — Build, publish DPC và tạo QR

Đây là quá trình chuẩn bị trước khi provisioning tablet.

Kylok GoLocal APK RepositoryGradle + APK ToolingKylok GoLocal APK RepositoryGradle + APK Tooling#chatgpt-mermaid-\_r_sr\_{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;font-size:16px;fill:rgb(237, 237, 237);}@keyframes edge-animation-frame{from{stroke-dashoffset:0;}}@keyframes dash{to{stroke-dashoffset:0;}}#chatgpt-mermaid-\_r_sr\_ .edge-animation-slow{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 50s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_sr\_ .edge-animation-fast{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 20s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_sr\_ .error-icon{fill:rgb(48, 48, 48);}#chatgpt-mermaid-\_r_sr\_ .error-text{fill:rgb(237, 237, 237);stroke:rgb(237, 237, 237);}#chatgpt-mermaid-\_r_sr\_ .edge-thickness-normal{stroke-width:1px;}#chatgpt-mermaid-\_r_sr\_ .edge-thickness-thick{stroke-width:3.5px;}#chatgpt-mermaid-\_r_sr\_ .edge-pattern-solid{stroke-dasharray:0;}#chatgpt-mermaid-\_r_sr\_ .edge-thickness-invisible{stroke-width:0;fill:none;}#chatgpt-mermaid-\_r_sr\_ .edge-pattern-dashed{stroke-dasharray:3;}#chatgpt-mermaid-\_r_sr\_ .edge-pattern-dotted{stroke-dasharray:2;}#chatgpt-mermaid-\_r_sr\_ .marker{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_sr\_ .marker.cross{stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_sr\_ svg{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;font-size:16px;}#chatgpt-mermaid-\_r_sr\_ p{margin:0;}#chatgpt-mermaid-\_r_sr\_ .actor{stroke:rgb(31, 78, 148);fill:rgb(9, 23, 44);stroke-width:1;}#chatgpt-mermaid-\_r_sr\_ rect.actor.outer-path[data-look="neo"]{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_sr\_ rect.note[data-look="neo"]{stroke:rgb(58, 132, 63);fill:rgb(48, 48, 48);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_sr\_ text.actor>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_sr\_ .actor-line{stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_sr\_ .innerArc{stroke-width:1.5;stroke-dasharray:none;}#chatgpt-mermaid-\_r_sr\_ .messageLine0{stroke-width:1.5;stroke-dasharray:none;stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_sr\_ .messageLine1{stroke-width:1.5;stroke-dasharray:2,2;stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_sr\_ [id$="-arrowhead"] path{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_sr\_ .sequenceNumber{fill:#505050;}#chatgpt-mermaid-\_r_sr\_ [id$="-sequencenumber"]{fill:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_sr\_ [id$="-crosshead"] path{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_sr\_ .messageText{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_sr\_ .labelBox{stroke:rgba(255, 255, 255, 0.15);fill:rgb(0, 0, 0);filter:none;}#chatgpt-mermaid-\_r_sr\_ .labelText,#chatgpt-mermaid-\_r_sr\_ .labelText>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_sr\_ .loopText,#chatgpt-mermaid-\_r_sr\_ .loopText>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_sr\_ .sectionTitle,#chatgpt-mermaid-\_r_sr\_ .sectionTitle>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_sr\_ .loopLine{stroke-width:2px;stroke-dasharray:2,2;stroke:rgba(255, 255, 255, 0.15);fill:rgba(255, 255, 255, 0.15);}#chatgpt-mermaid-\_r_sr\_ .note{stroke:rgb(58, 132, 63);fill:rgb(48, 48, 48);}#chatgpt-mermaid-\_r_sr\_ .noteText,#chatgpt-mermaid-\_r_sr\_ .noteText>tspan{fill:rgb(237, 237, 237);stroke:none;font-weight:normal;}#chatgpt-mermaid-\_r_sr\_ .activation0{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_sr\_ .activation1{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_sr\_ .activation2{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_sr\_ .actorPopupMenu{position:absolute;}#chatgpt-mermaid-\_r_sr\_ .actorPopupMenuPanel{position:absolute;fill:rgb(9, 23, 44);box-shadow:0px 8px 16px 0px rgba(0,0,0,0.2);filter:drop-shadow(3px 5px 2px rgb(0 0 0 / 0.4));}#chatgpt-mermaid-\_r_sr\_ .actor-man circle,#chatgpt-mermaid-\_r_sr\_ line{fill:rgb(9, 23, 44);stroke-width:2px;}#chatgpt-mermaid-\_r_sr\_ g rect.rect{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));stroke:rgb(31, 78, 148);}#chatgpt-mermaid-\_r_sr\_ .node .neo-node{stroke:rgb(31, 78, 148);}#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].node rect,#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].cluster rect,#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].node polygon{stroke:url(#chatgpt-mermaid-\_r_sr\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].swimlane.cluster rect{filter:none;}#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].node path{stroke:url(#chatgpt-mermaid-\_r_sr\_-gradient);stroke-width:1px;}#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].node .outer-path{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].node .neo-line path{stroke:rgb(31, 78, 148);filter:none;}#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].node circle{stroke:url(#chatgpt-mermaid-\_r_sr\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].node circle .state-start{fill:#000000;}#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].icon-shape .icon{fill:url(#chatgpt-mermaid-\_r_sr\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_sr\_ [data-look="neo"].icon-shape .icon-neo path{stroke:url(#chatgpt-mermaid-\_r_sr\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_sr\_ :root{--mermaid-font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;}Android Host DeveloperAdminBuild signed host DPC APK1Validate manifest and signing2Generate APK metadata3Publish immutable signed APK4Register bootstrap artifact5Verify digest and metadata6Create enrollment7Generate one-time token8Generate Android QR payload9QR code10Android Host DeveloperAdmin

APK metadata tối thiểu gồm:

```
{
  "packageName": "com.example.controller",
  "adminReceiver": "io.kylok.dpc.KylokAdminReceiver",
  "versionCode": 1,
  "sha256": "<sha256-hex>",
  "signingCertificateSHA256": "<certificate-fingerprint>",
  "artifactPath": "minimal-dpc-release.apk"
}
```

`adminReceiver` là class component bên trong host APK; component name đầy đủ cho QR được tạo từ package name và class name. Các giá trị trong JSON là minh họa.

Kylok chỉ được tạo QR sau khi xác minh bootstrap APK artifact hợp lệ, có thể phục vụ qua HTTPS và metadata không mâu thuẫn với artifact.

Không nên cho phép QR trỏ vào một mutable URL mà nội dung APK có thể bị thay đổi sau khi QR được phát hành.

### 8.2. Flow B — QR provisioning hoàn toàn trong LAN

Đây là luồng quan trọng nhất của v1.

Kylok GoHost APK + Kylok AARKylok APK EndpointAndroid Setup WizardKylok GoHost APK + Kylok AARKylok APK EndpointAndroid Setup Wizard#chatgpt-mermaid-\_r_ta\_{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;font-size:16px;fill:rgb(237, 237, 237);}@keyframes edge-animation-frame{from{stroke-dashoffset:0;}}@keyframes dash{to{stroke-dashoffset:0;}}#chatgpt-mermaid-\_r_ta\_ .edge-animation-slow{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 50s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_ta\_ .edge-animation-fast{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 20s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_ta\_ .error-icon{fill:rgb(48, 48, 48);}#chatgpt-mermaid-\_r_ta\_ .error-text{fill:rgb(237, 237, 237);stroke:rgb(237, 237, 237);}#chatgpt-mermaid-\_r_ta\_ .edge-thickness-normal{stroke-width:1px;}#chatgpt-mermaid-\_r_ta\_ .edge-thickness-thick{stroke-width:3.5px;}#chatgpt-mermaid-\_r_ta\_ .edge-pattern-solid{stroke-dasharray:0;}#chatgpt-mermaid-\_r_ta\_ .edge-thickness-invisible{stroke-width:0;fill:none;}#chatgpt-mermaid-\_r_ta\_ .edge-pattern-dashed{stroke-dasharray:3;}#chatgpt-mermaid-\_r_ta\_ .edge-pattern-dotted{stroke-dasharray:2;}#chatgpt-mermaid-\_r_ta\_ .marker{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ta\_ .marker.cross{stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ta\_ svg{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;font-size:16px;}#chatgpt-mermaid-\_r_ta\_ p{margin:0;}#chatgpt-mermaid-\_r_ta\_ .actor{stroke:rgb(31, 78, 148);fill:rgb(9, 23, 44);stroke-width:1;}#chatgpt-mermaid-\_r_ta\_ rect.actor.outer-path[data-look="neo"]{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ta\_ rect.note[data-look="neo"]{stroke:rgb(58, 132, 63);fill:rgb(48, 48, 48);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ta\_ text.actor>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_ta\_ .actor-line{stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ta\_ .innerArc{stroke-width:1.5;stroke-dasharray:none;}#chatgpt-mermaid-\_r_ta\_ .messageLine0{stroke-width:1.5;stroke-dasharray:none;stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ta\_ .messageLine1{stroke-width:1.5;stroke-dasharray:2,2;stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ta\_ [id$="-arrowhead"] path{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ta\_ .sequenceNumber{fill:#505050;}#chatgpt-mermaid-\_r_ta\_ [id$="-sequencenumber"]{fill:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ta\_ [id$="-crosshead"] path{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ta\_ .messageText{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_ta\_ .labelBox{stroke:rgba(255, 255, 255, 0.15);fill:rgb(0, 0, 0);filter:none;}#chatgpt-mermaid-\_r_ta\_ .labelText,#chatgpt-mermaid-\_r_ta\_ .labelText>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_ta\_ .loopText,#chatgpt-mermaid-\_r_ta\_ .loopText>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_ta\_ .sectionTitle,#chatgpt-mermaid-\_r_ta\_ .sectionTitle>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_ta\_ .loopLine{stroke-width:2px;stroke-dasharray:2,2;stroke:rgba(255, 255, 255, 0.15);fill:rgba(255, 255, 255, 0.15);}#chatgpt-mermaid-\_r_ta\_ .note{stroke:rgb(58, 132, 63);fill:rgb(48, 48, 48);}#chatgpt-mermaid-\_r_ta\_ .noteText,#chatgpt-mermaid-\_r_ta\_ .noteText>tspan{fill:rgb(237, 237, 237);stroke:none;font-weight:normal;}#chatgpt-mermaid-\_r_ta\_ .activation0{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_ta\_ .activation1{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_ta\_ .activation2{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_ta\_ .actorPopupMenu{position:absolute;}#chatgpt-mermaid-\_r_ta\_ .actorPopupMenuPanel{position:absolute;fill:rgb(9, 23, 44);box-shadow:0px 8px 16px 0px rgba(0,0,0,0.2);filter:drop-shadow(3px 5px 2px rgb(0 0 0 / 0.4));}#chatgpt-mermaid-\_r_ta\_ .actor-man circle,#chatgpt-mermaid-\_r_ta\_ line{fill:rgb(9, 23, 44);stroke-width:2px;}#chatgpt-mermaid-\_r_ta\_ g rect.rect{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));stroke:rgb(31, 78, 148);}#chatgpt-mermaid-\_r_ta\_ .node .neo-node{stroke:rgb(31, 78, 148);}#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].node rect,#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].cluster rect,#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].node polygon{stroke:url(#chatgpt-mermaid-\_r_ta\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].swimlane.cluster rect{filter:none;}#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].node path{stroke:url(#chatgpt-mermaid-\_r_ta\_-gradient);stroke-width:1px;}#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].node .outer-path{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].node .neo-line path{stroke:rgb(31, 78, 148);filter:none;}#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].node circle{stroke:url(#chatgpt-mermaid-\_r_ta\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].node circle .state-start{fill:#000000;}#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].icon-shape .icon{fill:url(#chatgpt-mermaid-\_r_ta\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ta\_ [data-look="neo"].icon-shape .icon-neo path{stroke:url(#chatgpt-mermaid-\_r_ta\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ta\_ :root{--mermaid-font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;}AdminScan provisioning QR1Connect to configured Wi-Fi2Download signed DPC APK via HTTPS3APK4Verify APK checksum5Install DPC6Provisioning mode callback7Fully Managed Device8Configure Device Owner9Compliance / provisioning callback10Initialize Agent state11Establish required runtime permissions12Enroll with one-time token13Validate and consume token atomically14Create device identity15Device credentials + initial config16Persist credentials securely17Initial Sync18Desired policy revision19Begin reconciliation20Report management status21Admin

Những ràng buộc cần bảo đảm:

- `EXTRA_PROVISIONING_ALLOW_OFFLINE=true` được đặt trong QR.
- Android Setup Wizard có thể kết nối mạng nội bộ trước khi DPC tồn tại.
- DPC APK URL được hệ thống Android tin cậy về TLS.
- QR chứa token enrollment ngắn hạn.
- Token được sử dụng một lần và có xử lý retry/idempotency an toàn.
- Device Owner được xác minh trước khi nhận policy yêu cầu privileged APIs.
- Nếu enrollment chưa hoàn tất, DPC giữ trạng thái có thể khôi phục thay vì báo thành công giả.

Google Test DPC mô tả flow tương tự, bao gồm việc dùng APK tự host trong QR provisioning. Android cũng xác định rằng APK checksum trong QR phải khớp SHA-256 của file tải xuống.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://github.com\&sz=32)

GitHub

+1



Giới hạn: Sequence này là target behavior. Kylok không thể cam kết bước Android Setup Wizard thành công trên mọi OEM hoặc mọi firmware.

### 8.3. Flow C — Tự động triển khai APK

Android PackageInstallerKylok AndroidAPK RepositoryKylok GoAndroid PackageInstallerKylok AndroidAPK RepositoryKylok Go#chatgpt-mermaid-\_r_tj\_{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;font-size:16px;fill:rgb(237, 237, 237);}@keyframes edge-animation-frame{from{stroke-dashoffset:0;}}@keyframes dash{to{stroke-dashoffset:0;}}#chatgpt-mermaid-\_r_tj\_ .edge-animation-slow{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 50s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_tj\_ .edge-animation-fast{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 20s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_tj\_ .error-icon{fill:rgb(48, 48, 48);}#chatgpt-mermaid-\_r_tj\_ .error-text{fill:rgb(237, 237, 237);stroke:rgb(237, 237, 237);}#chatgpt-mermaid-\_r_tj\_ .edge-thickness-normal{stroke-width:1px;}#chatgpt-mermaid-\_r_tj\_ .edge-thickness-thick{stroke-width:3.5px;}#chatgpt-mermaid-\_r_tj\_ .edge-pattern-solid{stroke-dasharray:0;}#chatgpt-mermaid-\_r_tj\_ .edge-thickness-invisible{stroke-width:0;fill:none;}#chatgpt-mermaid-\_r_tj\_ .edge-pattern-dashed{stroke-dasharray:3;}#chatgpt-mermaid-\_r_tj\_ .edge-pattern-dotted{stroke-dasharray:2;}#chatgpt-mermaid-\_r_tj\_ .marker{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_tj\_ .marker.cross{stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_tj\_ svg{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;font-size:16px;}#chatgpt-mermaid-\_r_tj\_ p{margin:0;}#chatgpt-mermaid-\_r_tj\_ .actor{stroke:rgb(31, 78, 148);fill:rgb(9, 23, 44);stroke-width:1;}#chatgpt-mermaid-\_r_tj\_ rect.actor.outer-path[data-look="neo"]{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_tj\_ rect.note[data-look="neo"]{stroke:rgb(58, 132, 63);fill:rgb(48, 48, 48);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_tj\_ text.actor>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_tj\_ .actor-line{stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_tj\_ .innerArc{stroke-width:1.5;stroke-dasharray:none;}#chatgpt-mermaid-\_r_tj\_ .messageLine0{stroke-width:1.5;stroke-dasharray:none;stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_tj\_ .messageLine1{stroke-width:1.5;stroke-dasharray:2,2;stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_tj\_ [id$="-arrowhead"] path{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_tj\_ .sequenceNumber{fill:#505050;}#chatgpt-mermaid-\_r_tj\_ [id$="-sequencenumber"]{fill:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_tj\_ [id$="-crosshead"] path{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_tj\_ .messageText{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_tj\_ .labelBox{stroke:rgba(255, 255, 255, 0.15);fill:rgb(0, 0, 0);filter:none;}#chatgpt-mermaid-\_r_tj\_ .labelText,#chatgpt-mermaid-\_r_tj\_ .labelText>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_tj\_ .loopText,#chatgpt-mermaid-\_r_tj\_ .loopText>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_tj\_ .sectionTitle,#chatgpt-mermaid-\_r_tj\_ .sectionTitle>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_tj\_ .loopLine{stroke-width:2px;stroke-dasharray:2,2;stroke:rgba(255, 255, 255, 0.15);fill:rgba(255, 255, 255, 0.15);}#chatgpt-mermaid-\_r_tj\_ .note{stroke:rgb(58, 132, 63);fill:rgb(48, 48, 48);}#chatgpt-mermaid-\_r_tj\_ .noteText,#chatgpt-mermaid-\_r_tj\_ .noteText>tspan{fill:rgb(237, 237, 237);stroke:none;font-weight:normal;}#chatgpt-mermaid-\_r_tj\_ .activation0{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_tj\_ .activation1{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_tj\_ .activation2{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_tj\_ .actorPopupMenu{position:absolute;}#chatgpt-mermaid-\_r_tj\_ .actorPopupMenuPanel{position:absolute;fill:rgb(9, 23, 44);box-shadow:0px 8px 16px 0px rgba(0,0,0,0.2);filter:drop-shadow(3px 5px 2px rgb(0 0 0 / 0.4));}#chatgpt-mermaid-\_r_tj\_ .actor-man circle,#chatgpt-mermaid-\_r_tj\_ line{fill:rgb(9, 23, 44);stroke-width:2px;}#chatgpt-mermaid-\_r_tj\_ g rect.rect{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));stroke:rgb(31, 78, 148);}#chatgpt-mermaid-\_r_tj\_ .node .neo-node{stroke:rgb(31, 78, 148);}#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].node rect,#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].cluster rect,#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].node polygon{stroke:url(#chatgpt-mermaid-\_r_tj\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].swimlane.cluster rect{filter:none;}#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].node path{stroke:url(#chatgpt-mermaid-\_r_tj\_-gradient);stroke-width:1px;}#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].node .outer-path{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].node .neo-line path{stroke:rgb(31, 78, 148);filter:none;}#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].node circle{stroke:url(#chatgpt-mermaid-\_r_tj\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].node circle .state-start{fill:#000000;}#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].icon-shape .icon{fill:url(#chatgpt-mermaid-\_r_tj\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_tj\_ [data-look="neo"].icon-shape .icon-neo path{stroke:url(#chatgpt-mermaid-\_r_tj\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_tj\_ :root{--mermaid-font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;}AdminRegister APK + assign desired version1Validate artifact and update policy2Sync(current policy revision)3New policy + artifact metadata4Compare installed app version5Download APK over HTTPS6APK bytes7Verify digest and signing metadata8Create and commit install session9Installation result10Verify installed package and version11Report installation result12Persist observed state13Admin

Android có API `PackageInstaller` hỗ trợ cài đặt APK trong thiết bị fully managed mà không yêu cầu người dùng xác nhận, khi đáp ứng các điều kiện của hệ điều hành.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://developer.android.com\&sz=32)

developer.android.com



Kylok phải xử lý đầy đủ status callbacks của `PackageInstaller`, bao gồm `STATUS_PENDING_USER_ACTION`, thay vì giả định mọi install session sẽ silent trong mọi trường hợp.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://developer.android.com\&sz=32)

developer.android.com



### 8.4. Flow D — Policy reconciliation

DevicePolicyManagerKylok AndroidKylok GoDevicePolicyManagerKylok AndroidKylok Go#chatgpt-mermaid-\_r_ts\_{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;font-size:16px;fill:rgb(237, 237, 237);}@keyframes edge-animation-frame{from{stroke-dashoffset:0;}}@keyframes dash{to{stroke-dashoffset:0;}}#chatgpt-mermaid-\_r_ts\_ .edge-animation-slow{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 50s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_ts\_ .edge-animation-fast{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 20s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_ts\_ .error-icon{fill:rgb(48, 48, 48);}#chatgpt-mermaid-\_r_ts\_ .error-text{fill:rgb(237, 237, 237);stroke:rgb(237, 237, 237);}#chatgpt-mermaid-\_r_ts\_ .edge-thickness-normal{stroke-width:1px;}#chatgpt-mermaid-\_r_ts\_ .edge-thickness-thick{stroke-width:3.5px;}#chatgpt-mermaid-\_r_ts\_ .edge-pattern-solid{stroke-dasharray:0;}#chatgpt-mermaid-\_r_ts\_ .edge-thickness-invisible{stroke-width:0;fill:none;}#chatgpt-mermaid-\_r_ts\_ .edge-pattern-dashed{stroke-dasharray:3;}#chatgpt-mermaid-\_r_ts\_ .edge-pattern-dotted{stroke-dasharray:2;}#chatgpt-mermaid-\_r_ts\_ .marker{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ts\_ .marker.cross{stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ts\_ svg{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;font-size:16px;}#chatgpt-mermaid-\_r_ts\_ p{margin:0;}#chatgpt-mermaid-\_r_ts\_ .actor{stroke:rgb(31, 78, 148);fill:rgb(9, 23, 44);stroke-width:1;}#chatgpt-mermaid-\_r_ts\_ rect.actor.outer-path[data-look="neo"]{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ts\_ rect.note[data-look="neo"]{stroke:rgb(58, 132, 63);fill:rgb(48, 48, 48);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ts\_ text.actor>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_ts\_ .actor-line{stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ts\_ .innerArc{stroke-width:1.5;stroke-dasharray:none;}#chatgpt-mermaid-\_r_ts\_ .messageLine0{stroke-width:1.5;stroke-dasharray:none;stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ts\_ .messageLine1{stroke-width:1.5;stroke-dasharray:2,2;stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ts\_ [id$="-arrowhead"] path{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ts\_ .sequenceNumber{fill:#505050;}#chatgpt-mermaid-\_r_ts\_ [id$="-sequencenumber"]{fill:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ts\_ [id$="-crosshead"] path{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_ts\_ .messageText{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_ts\_ .labelBox{stroke:rgba(255, 255, 255, 0.15);fill:rgb(0, 0, 0);filter:none;}#chatgpt-mermaid-\_r_ts\_ .labelText,#chatgpt-mermaid-\_r_ts\_ .labelText>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_ts\_ .loopText,#chatgpt-mermaid-\_r_ts\_ .loopText>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_ts\_ .sectionTitle,#chatgpt-mermaid-\_r_ts\_ .sectionTitle>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_ts\_ .loopLine{stroke-width:2px;stroke-dasharray:2,2;stroke:rgba(255, 255, 255, 0.15);fill:rgba(255, 255, 255, 0.15);}#chatgpt-mermaid-\_r_ts\_ .note{stroke:rgb(58, 132, 63);fill:rgb(48, 48, 48);}#chatgpt-mermaid-\_r_ts\_ .noteText,#chatgpt-mermaid-\_r_ts\_ .noteText>tspan{fill:rgb(237, 237, 237);stroke:none;font-weight:normal;}#chatgpt-mermaid-\_r_ts\_ .activation0{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_ts\_ .activation1{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_ts\_ .activation2{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_ts\_ .actorPopupMenu{position:absolute;}#chatgpt-mermaid-\_r_ts\_ .actorPopupMenuPanel{position:absolute;fill:rgb(9, 23, 44);box-shadow:0px 8px 16px 0px rgba(0,0,0,0.2);filter:drop-shadow(3px 5px 2px rgb(0 0 0 / 0.4));}#chatgpt-mermaid-\_r_ts\_ .actor-man circle,#chatgpt-mermaid-\_r_ts\_ line{fill:rgb(9, 23, 44);stroke-width:2px;}#chatgpt-mermaid-\_r_ts\_ g rect.rect{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));stroke:rgb(31, 78, 148);}#chatgpt-mermaid-\_r_ts\_ .node .neo-node{stroke:rgb(31, 78, 148);}#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].node rect,#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].cluster rect,#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].node polygon{stroke:url(#chatgpt-mermaid-\_r_ts\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].swimlane.cluster rect{filter:none;}#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].node path{stroke:url(#chatgpt-mermaid-\_r_ts\_-gradient);stroke-width:1px;}#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].node .outer-path{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].node .neo-line path{stroke:rgb(31, 78, 148);filter:none;}#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].node circle{stroke:url(#chatgpt-mermaid-\_r_ts\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].node circle .state-start{fill:#000000;}#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].icon-shape .icon{fill:url(#chatgpt-mermaid-\_r_ts\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ts\_ [data-look="neo"].icon-shape .icon-neo path{stroke:url(#chatgpt-mermaid-\_r_ts\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_ts\_ :root{--mermaid-font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;}alt[Supported][Unsupported]loop[Each policy operation]Sync(lastAppliedRevision, capabilities)1Desired policy revision N2Persist desired policy3Calculate difference4Check capability and prerequisites5Apply policy6Result / callback7Verify observed state8Record unsupported reason9Report compliance and actual state10Persist compliance report11

Thiết kế reconciler:

1. Idempotent: apply lại cùng policy không làm thay đổi trạng thái ngoài mong muốn.
2. Resumable: khi process bị kill, Agent có thể tiếp tục từ trạng thái đã lưu.
3. Observable: biết được bước nào thất bại.
4. Capability-aware: không gọi API mà thiết bị không hỗ trợ.
5. Safe-ordering: cài app thành công trước khi chuyển sang kiosk mode yêu cầu app đó.
6. Partial compliance: một lỗi không tự động biến toàn bộ policy thành `APPLIED`.

Các Android policy APIs có thể cung cấp result callback riêng, ví dụ Lock Task Policy trên Android 14 trở lên. Kylok cần ưu tiên xác nhận kết quả thực tế thay vì chỉ tin vào việc lời gọi API không phát sinh exception.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://developer.android.com\&sz=32)

developer.android.com



### 8.5. Flow E — Mất kết nối và khôi phục

Kylok GoLocal StateKylok AndroidAndroid LifecycleKylok GoLocal StateKylok AndroidAndroid Lifecycle#chatgpt-mermaid-\_r_u5\_{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;font-size:16px;fill:rgb(237, 237, 237);}@keyframes edge-animation-frame{from{stroke-dashoffset:0;}}@keyframes dash{to{stroke-dashoffset:0;}}#chatgpt-mermaid-\_r_u5\_ .edge-animation-slow{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 50s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_u5\_ .edge-animation-fast{stroke-dasharray:9,5!important;stroke-dashoffset:900;animation:dash 20s linear infinite;stroke-linecap:round;}#chatgpt-mermaid-\_r_u5\_ .error-icon{fill:rgb(48, 48, 48);}#chatgpt-mermaid-\_r_u5\_ .error-text{fill:rgb(237, 237, 237);stroke:rgb(237, 237, 237);}#chatgpt-mermaid-\_r_u5\_ .edge-thickness-normal{stroke-width:1px;}#chatgpt-mermaid-\_r_u5\_ .edge-thickness-thick{stroke-width:3.5px;}#chatgpt-mermaid-\_r_u5\_ .edge-pattern-solid{stroke-dasharray:0;}#chatgpt-mermaid-\_r_u5\_ .edge-thickness-invisible{stroke-width:0;fill:none;}#chatgpt-mermaid-\_r_u5\_ .edge-pattern-dashed{stroke-dasharray:3;}#chatgpt-mermaid-\_r_u5\_ .edge-pattern-dotted{stroke-dasharray:2;}#chatgpt-mermaid-\_r_u5\_ .marker{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_u5\_ .marker.cross{stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_u5\_ svg{font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;font-size:16px;}#chatgpt-mermaid-\_r_u5\_ p{margin:0;}#chatgpt-mermaid-\_r_u5\_ .actor{stroke:rgb(31, 78, 148);fill:rgb(9, 23, 44);stroke-width:1;}#chatgpt-mermaid-\_r_u5\_ rect.actor.outer-path[data-look="neo"]{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_u5\_ rect.note[data-look="neo"]{stroke:rgb(58, 132, 63);fill:rgb(48, 48, 48);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_u5\_ text.actor>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_u5\_ .actor-line{stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_u5\_ .innerArc{stroke-width:1.5;stroke-dasharray:none;}#chatgpt-mermaid-\_r_u5\_ .messageLine0{stroke-width:1.5;stroke-dasharray:none;stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_u5\_ .messageLine1{stroke-width:1.5;stroke-dasharray:2,2;stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_u5\_ [id$="-arrowhead"] path{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_u5\_ .sequenceNumber{fill:#505050;}#chatgpt-mermaid-\_r_u5\_ [id$="-sequencenumber"]{fill:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_u5\_ [id$="-crosshead"] path{fill:rgb(175, 175, 175);stroke:rgb(175, 175, 175);}#chatgpt-mermaid-\_r_u5\_ .messageText{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_u5\_ .labelBox{stroke:rgba(255, 255, 255, 0.15);fill:rgb(0, 0, 0);filter:none;}#chatgpt-mermaid-\_r_u5\_ .labelText,#chatgpt-mermaid-\_r_u5\_ .labelText>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_u5\_ .loopText,#chatgpt-mermaid-\_r_u5\_ .loopText>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_u5\_ .sectionTitle,#chatgpt-mermaid-\_r_u5\_ .sectionTitle>tspan{fill:rgb(237, 237, 237);stroke:none;}#chatgpt-mermaid-\_r_u5\_ .loopLine{stroke-width:2px;stroke-dasharray:2,2;stroke:rgba(255, 255, 255, 0.15);fill:rgba(255, 255, 255, 0.15);}#chatgpt-mermaid-\_r_u5\_ .note{stroke:rgb(58, 132, 63);fill:rgb(48, 48, 48);}#chatgpt-mermaid-\_r_u5\_ .noteText,#chatgpt-mermaid-\_r_u5\_ .noteText>tspan{fill:rgb(237, 237, 237);stroke:none;font-weight:normal;}#chatgpt-mermaid-\_r_u5\_ .activation0{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_u5\_ .activation1{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_u5\_ .activation2{fill:rgb(48, 48, 48);stroke:hsl(0, 0%, 8.8235294118%);}#chatgpt-mermaid-\_r_u5\_ .actorPopupMenu{position:absolute;}#chatgpt-mermaid-\_r_u5\_ .actorPopupMenuPanel{position:absolute;fill:rgb(9, 23, 44);box-shadow:0px 8px 16px 0px rgba(0,0,0,0.2);filter:drop-shadow(3px 5px 2px rgb(0 0 0 / 0.4));}#chatgpt-mermaid-\_r_u5\_ .actor-man circle,#chatgpt-mermaid-\_r_u5\_ line{fill:rgb(9, 23, 44);stroke-width:2px;}#chatgpt-mermaid-\_r_u5\_ g rect.rect{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));stroke:rgb(31, 78, 148);}#chatgpt-mermaid-\_r_u5\_ .node .neo-node{stroke:rgb(31, 78, 148);}#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].node rect,#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].cluster rect,#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].node polygon{stroke:url(#chatgpt-mermaid-\_r_u5\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].swimlane.cluster rect{filter:none;}#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].node path{stroke:url(#chatgpt-mermaid-\_r_u5\_-gradient);stroke-width:1px;}#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].node .outer-path{filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].node .neo-line path{stroke:rgb(31, 78, 148);filter:none;}#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].node circle{stroke:url(#chatgpt-mermaid-\_r_u5\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].node circle .state-start{fill:#000000;}#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].icon-shape .icon{fill:url(#chatgpt-mermaid-\_r_u5\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_u5\_ [data-look="neo"].icon-shape .icon-neo path{stroke:url(#chatgpt-mermaid-\_r_u5\_-gradient);filter:drop-shadow( 1px 2px 2px rgba(185,185,185,1));}#chatgpt-mermaid-\_r_u5\_ :root{--mermaid-font-family:-apple-system-body,ui-sans-serif,-apple-system,system-ui,Segoe UI,Helvetica,Apple Color Emoji,Arial,sans-serif,Segoe UI Emoji,Segoe UI Symbol;}alt[Network unavailable][Network restored]Boot / WorkManager trigger1Read credentials and pending work2Sync / upload pending reports3Retain pending operations4Schedule bounded retry5Latest desired state6Update durable checkpoint7Resume reconciliation8Report observed state9

Kylok không được giữ trạng thái quan trọng chỉ trong memory. Device identity, policy revisions, pending deployment và kết quả chưa gửi phải tồn tại qua process restart/reboot.

## 9. Default storage và artifact management

### 9.1. Go default storage

Tôi đề xuất SQLite làm embedded storage mặc định.

Lý do:

- Không yêu cầu chạy thêm PostgreSQL cho vài tablet.
- Có transaction và persistent state.
- Phù hợp example standalone.
- Có thể kiểm thử bằng chính implementation được dùng khi chạy thực tế.
- Host muốn tích hợp PostgreSQL vẫn thay được qua storage adapter.

SQLite nên được cấu hình với migrations, WAL và transaction handling thích hợp.

Default này được hỗ trợ cho single-process, single-node deployment. Multi-instance hoặc distributed deployment phải sử dụng adapter có cơ chế concurrency/transaction phù hợp, ví dụ PostgreSQL.

Không nên hứa rằng một implementation `Store` dạng CRUD đơn giản đủ để bảo đảm enrollment atomicity và operation leasing. Storage contract phải quy định rõ các transactional guarantees cần có.

### 9.2. Android default storage

Đề xuất sử dụng:

- Room: Policy revisions, pending operations, installation state, compliance reports.
- Android Keystore: Bảo vệ key material dùng để bảo vệ device credentials.
- WorkManager: Durable background work và reconnect/retry orchestration.
- Application-private files: Download staging và APK cache.

Không tạo custom database engine hoặc custom job scheduler khi AndroidX đã cung cấp implementation phù hợp.

### 9.3. Local APK artifact store

Default artifact store sử dụng filesystem nhưng phải tuân thủ các quy tắc:

| Thuộc tính   | Yêu cầu                                                                                       |
| ------------ | --------------------------------------------------------------------------------------------- |
| Identity     | Content digest SHA-256                                                                        |
| Mutability   | Immutable sau khi publish                                                                     |
| Download     | HTTPS                                                                                         |
| Range/resume | Hỗ trợ khi có thể                                                                             |
| Verification | Digest và APK signing metadata                                                                |
| Access       | Bootstrap artifact public theo URL trong QR; các artifact khác có device-scoped authorization |
| Cleanup      | Garbage collection có kiểm tra references                                                     |
| History      | Giữ artifact còn được policy hoặc deployment sử dụng                                          |

APK file không cần phải nằm cùng server process. `ArtifactStore` có thể resolve URL tới local MinIO hoặc một HTTP repository khác.

Đối với bootstrap APK, URL phải khả dụng trước khi Agent có device credentials. Vì vậy không được sử dụng device-authenticated download endpoint cho chính APK đầu tiên.

### 9.4. APK compatibility

MVP bắt đầu với single APK/universal APK để giảm rủi ro triển khai.

Tuy nhiên, artifact model nên hỗ trợ một installation set gồm base APK và split APKs, tránh phải thay toàn bộ schema khi cần hỗ trợ App Bundles.

Với updates, Kylok phải kiểm tra package name, version code, signing identity và Android compatibility; không được coi mọi artifact mới hơn là có thể cài đè.

## 10. Security architecture

Security là một phần của framework, không phải trách nhiệm mà Kylok đẩy hoàn toàn sang host.

### 10.1. Identity và enrollment

MVP sử dụng mô hình:

1. QR chứa one-time enrollment token có entropy cao và expiration.
2. Agent gửi token qua HTTPS.
3. Server xác minh token, scope, trạng thái và request idempotency.
4. Server tạo device identity và credential riêng.
5. Agent bảo vệ credential bằng cơ chế dựa trên Android Keystore.
6. Mỗi Agent RPC xác thực theo device identity.
7. Server cho phép rotation, revocation và reenrollment có kiểm soát.

Không tái sử dụng JWT đăng nhập người dùng của host làm device credential.

Enrollment request có idempotency key để xử lý trường hợp server đã tạo device nhưng response bị mất. Retry hợp lệ không được tạo device thứ hai hoặc mở cơ hội sử dụng lại enrollment token từ một thiết bị khác.

V1 có thể sử dụng strong random device credentials qua TLS, lưu hashed credentials phía server. Key-bound authentication hoặc mTLS có thể phát triển thêm khi đã có yêu cầu quản lý chứng chỉ rõ ràng.

### 10.2. TLS và bootstrap

Tôi đề xuất hai trust phases:

Bootstrap phase: Android Setup Wizard tải APK. Certificate phải được hệ thống Android tin cậy ngay lúc đó.

Managed phase: DPC đã được cài và có thể sử dụng Network Security Configuration phù hợp cho server LAN.

Android cho phép ứng dụng chỉ định private trust anchors và cấu hình Certificate Transparency theo domain. Tuy nhiên, cấu hình này thuộc host APK, không thay thế trust configuration của Setup Wizard.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://developer.android.com\&sz=32)

developer.android.com



Default deployment nên sử dụng:

- Internal DNS trỏ một hostname thuộc domain hợp lệ tới server LAN.
- HTTPS với certificate được Android tin cậy.
- Không sử dụng `trust-all` hoặc tắt certificate verification.
- Không dùng HTTP để bootstrap production.

### 10.3. Authorization

Cần ba phạm vi kiểm soát khác nhau.

| Phạm vi         | Kiểm soát                                                           |
| --------------- | ------------------------------------------------------------------- |
| Host management | Host authorization hoặc management auth adapter                     |
| Enrollment      | One-time token, scope binding, expiry, replay protection            |
| Agent           | Device credential, device status, scope and operation authorization |

Agent không bao giờ được quyền tự assign policy, xem thiết bị khác hoặc tạo enrollment mới chỉ vì đã xác thực thành công.

### 10.4. Framework invariants

Custom hooks không được bypass:

- Device authentication.
- Enrollment token validation.
- Scope isolation.
- APK integrity verification.
- Required Device Owner checks.
- Operation idempotency.
- Signing compatibility.

Những điều này không phải optional policy settings.

## 11. MVP policy scope

Tôi đề xuất chọn các policy vừa hữu ích với tablet/kiosk, vừa dựa trên những Android APIs trưởng thành.

| Policy group        | Chức năng v1                                          | Thứ tự |
| ------------------- | ----------------------------------------------------- | ------ |
| Applications        | Required apps, minimum version, auto-update           | P0     |
| Kiosk               | Lock Task allowlist, designated kiosk app             | P0     |
| Display             | Screen capture restrictions                           | P0     |
| Security            | Camera restriction, debugging restriction             | P0     |
| Device restrictions | App uninstall, user/account modification restrictions | P0     |
| Device status       | OS version, model, agent version, connectivity status | P0     |
| Runtime permissions | Policy grants cho managed apps trong giới hạn Android | P0     |
| Screen lock         | Maximum time to lock                                  | P0     |
| Advanced network    | Managed Wi-Fi, network profiles                       | P1     |
| Certificates        | CA/client certificate management                      | P1     |
| Advanced enterprise | Password policies, logging, app restrictions          | P2     |

Kiosk mode phải cấu hình allowlist bằng `setLockTaskPackages()` và chỉ khởi động ứng dụng sau khi xác nhận package tồn tại và phù hợp.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://developer.android.com\&sz=32)

developer.android.com



Default policy ban đầu nên ít can thiệp: chưa bật kiosk, chưa khóa camera, chưa chặn debugging hoặc factory reset.

Những restriction gây khó khăn cho recovery chỉ áp dụng khi admin chủ động yêu cầu.

### Capability model

Agent báo capability thực tế, ví dụ:

```
{
  "apiLevel": 37,
  "managementMode": "DEVICE_OWNER",
  "capabilities": {
    "application.install": true,
    "application.update": true,
    "policy.kiosk": true,
    "policy.camera": true,
    "policy.screenCapture": true,
    "device.reboot": true
  }
}
```

Đây là dữ liệu minh họa, không phải capability report thực tế của một tablet.

Nếu một chính sách không hỗ trợ, Agent phải trả về structured reason. Server không được suy luận rằng chỉ cần API level đủ cao thì mọi OEM đều hỗ trợ chính xác cùng một hành vi.

## 12. Hai reference applications

### 12.1. `minimal-server`

Đây là integration reference cho Go SDK và cũng là standalone MDM server dùng để kiểm thử.

Mục tiêu: người dùng chỉ cần cung cấp những cấu hình thực sự bắt buộc để chạy end-to-end.

Ví dụ:

```
server:
  public_url: https://mdm.example.com
  listen: ":8443"

tls:
  cert_file: ./certs/server.crt
  key_file: ./certs/server.key

kylok:
  data_dir: ./data
  bootstrap_apk: ./artifacts/minimal-dpc.apk
  bootstrap_metadata: ./artifacts/minimal-dpc.json
```

Cần có management authentication cho các endpoint thao tác quản trị. Example không được mở anonymous management endpoints dù chỉ nhằm mục đích demonstration.

Chức năng example:

- Khởi tạo SDK.
- Serve Agent API.
- Serve APK bootstrap và application artifacts.
- Tạo enrollment và QR.
- Liệt kê thiết bị.
- Register artifact và assign policy.
- Hiển thị trạng thái cài đặt và compliance.
- Graceful shutdown.

Example có thể cung cấp CLI hoặc admin HTTP API đơn giản. Không bắt buộc có giao diện web.

### 12.2. `minimal-dpc`

Android reference application chỉ bao gồm:

```
minimal-dpc/
├── build.gradle.kts
├── AndroidManifest.xml
├── MinimalDpcApplication.kt
└── MainActivity.kt    # Optional status/diagnostics
```

Kylok AAR cung cấp Device Admin receiver, provisioning activities, policy reconciliation, installer và transport.

Host application chủ yếu quyết định `applicationId`, signing key và optional UI.

Example này phải chứng minh được các trường hợp:

- Chạy với default SDK integration.
- Có thể đăng ký event listeners.
- Có thể bổ sung custom policy handler.
- Có thể cập nhật chính bản host DPC APK.
- Không phải viết lại bất kỳ thành phần DPC cốt lõi nào.

Đây không phải một production DPC app được phân phối như sản phẩm chính. Nó tồn tại để người dùng hiểu và kiểm chứng cách nhúng AAR.

## 13. Roadmap theo độ hữu dụng và rủi ro

Tôi đề xuất không cố hoàn thiện từng layer riêng lẻ. Các milestone phải tạo ra khả năng sử dụng được từ server tới tablet.

### Foundation và MVP v1

M0 — Feasibility & Compatibility

Critical

Rủi ro cao nhất · Phụ thuộc Android/OEM

Thử QR provisioning offline trên các thiết bị mục tiêu, kiểm tra HTTPS bootstrap, Device Owner và các API cần thiết trên Android 13–17.

Exit gate: Chứng minh được một signed host APK có thể được tải và provision từ LAN, không cần Google Cloud hay WAN.

Nếu không vượt qua gate này, chưa mở rộng implementation.

M1 — Monorepo & Contracts

Khởi tạo Buf/Protobuf, Go module, Android AAR, hai examples, generation và CI. Định nghĩa enrollment, device và sync contracts đầu tiên.

Exit gate: Go và Kotlin compile từ cùng schema; examples chỉ import public APIs.

M2 — QR Provisioning & Enrollment

Xây QR generator, APK metadata, bootstrap artifact server, default Agent, enrollment identity và persistent storage.

Exit gate: Factory reset → scan QR → cài DPC → Device Owner → enroll → device xuất hiện trong registry.

M3 — Device Sync & Reliability

Hoàn thiện device registry, Agent identity, desired/reported state, policy revisions, local persistence và reconnect/retry.

Exit gate: Agent tự phục hồi sau mất LAN, process restart và reboot mà không mất identity hoặc áp dụng trùng một operation.

M4 — APK Lifecycle

Local artifact store, signed APK validation, download, silent install/update, self-update DPC và installation reporting.

Exit gate: Có thể yêu cầu cài hoặc cập nhật APK và quan sát kết quả thực tế trên tablet mà không cần người dùng xác nhận trong flow đã được thiết bị hỗ trợ.

M5 — Basic Policies & Kiosk

Hoàn thiện policies P0, capability reporting, compliance và safe-ordering.

Exit gate: Required apps, kiosk và basic restrictions áp dụng được, kiểm tra lại được và có failure reporting chính xác.

M6 — Public SDK Release

Hoàn thiện public API, hooks, tests, documentation, samples, API compatibility, release pipeline và Android AAR publishing.

Exit gate: Một Go host và một Android host hoàn toàn mới tích hợp được Kylok qua published packages, không cần copy source code hoặc custom bắt buộc.

### Roadmap sau MVP

Tôi xếp hạng dựa trên mức hữu dụng đối với tablet được quản lý qua LAN, thay vì số lượng API có thể implement.

| Ưu tiên | Tính năng                              | Lý do                                                                          |
| ------- | -------------------------------------- | ------------------------------------------------------------------------------ |
| P1      | Remote reboot / lock                   | Giá trị vận hành cao, Android có API chính thức                                |
| P1      | Device groups / bulk policy assignment | Hữu ích ngay khi số lượng thiết bị tăng                                        |
| P1      | Scheduled deployments                  | Tránh cập nhật trong giờ hoạt động                                             |
| P1      | Wi-Fi management                       | Đặc biệt hữu ích trong hệ thống LAN                                            |
| P1      | Deployment rollback strategy           | Giảm rủi ro update lỗi; phải tuân thủ Android downgrade/signature restrictions |
| P1      | PostgreSQL adapter                     | Phù hợp với host production như fDCMS                                          |
| P2      | Certificate lifecycle management       | Hữu ích cho private enterprise networks                                        |
| P2      | Advanced security policies             | Tùy nhu cầu vận hành                                                           |
| P2      | Remote diagnostics / bug reports       | Giảm công sức hỗ trợ thiết bị                                                  |
| P2      | Push over LAN                          | Giảm độ trễ so với polling                                                     |
| P2      | Managed application configurations     | Hữu ích cho enterprise applications                                            |
| P3      | Work Profile / COPE                    | Management mode khác, độ phức tạp cao                                          |
| P3      | OEM-specific integrations              | Chỉ thêm theo nhu cầu và thiết bị                                              |
| P3      | System OTA management                  | Phụ thuộc firmware/OEM                                                         |
| P3      | Remote screen viewing/control          | Quyền bị giới hạn, không phải chức năng Device Owner mặc định                  |

Một số protocol fields có thể được chuẩn bị cho roadmap, nhưng không triển khai operation engine hoặc extension framework phức tạp chỉ vì dự kiến sẽ cần sau này.

## 14. Testing và quality gates

### 14.1. Test strategy

| Layer            | Tests                                                   |
| ---------------- | ------------------------------------------------------- |
| Protobuf         | Buf lint, generation, breaking checks                   |
| Go core          | Unit tests, race tests, state machine tests             |
| Go storage       | Transaction/concurrency conformance tests               |
| Go transport     | RPC integration, authentication và error mapping        |
| Android core     | Kotlin unit tests, policy reconciliation                |
| Android platform | Instrumented tests trên API 33–37                       |
| Cross-language   | Go ↔ Kotlin protocol compatibility                      |
| Provisioning     | Physical-device QR tests                                |
| Security         | Token replay, credential revocation, artifact integrity |
| Examples         | End-to-end integration                                  |

### 14.2. Required test matrix

| Test                          | API 33 | API 34 | API 35 | API 36 | API 37 |
| ----------------------------- | ------ | ------ | ------ | ------ | ------ |
| Enrollment and sync           | ✓      | ✓      | ✓      | ✓      | ✓      |
| Device Owner policy execution | ✓      | ✓      | ✓      | ✓      | ✓      |
| APK installation/update       | ✓      | ✓      | ✓      | ✓      | ✓      |
| Kiosk                         | ✓      | ✓      | ✓      | ✓      | ✓      |
| Offline recovery              | ✓      | ✓      | ✓      | ✓      | ✓      |
| Local network permission      | N/A    | N/A    | N/A    | N/A    | ✓      |
| QR provisioning               | OEM    | OEM    | OEM    | OEM    | OEM    |

✓ là test coverage bắt buộc trong kế hoạch, không có nghĩa hiện đã pass. QR provisioning phải xác nhận theo firmware thực tế.

### 14.3. Mandatory failure scenarios

Kylok v1 không được phát hành nếu chưa kiểm tra:

- Token hết hạn, token sai, token bị sử dụng lại.
- Enrollment thành công nhưng mất response.
- Mất LAN giữa download hoặc installation.
- APK checksum mismatch.
- APK signing certificate mismatch.
- APK không tương thích với thiết bị.
- Policy thay đổi trong khi đang reconcile.
- DPC process bị kill hoặc reboot trong quá trình xử lý.
- Server restart giữa enrollment/operation.
- Android 17 không cấp quyền truy cập LAN.
- TLS certificate không được tin cậy.
- Artifact server trả về nội dung khác metadata.
- Self-update DPC xong nhưng Agent không kết nối lại.
- Host tùy chỉnh một hook rồi hook đó phát sinh lỗi hoặc timeout.

Các lỗi phải có structured error codes, retry policy thích hợp và không báo success giả.

## 15. Release và versioning

Tôi đề xuất phát hành hai SDK riêng nhưng kiểm soát tương thích ở cấp monorepo.

| Artifact       | Versioning                                            |
| -------------- | ----------------------------------------------------- |
| Kylok Go       | Semantic Versioning                                   |
| Kylok Android  | Maven artifact version + APK integration requirements |
| Protobuf       | `kylok.v1`, breaking changes tạo API version mới      |
| Example server | Dùng Go SDK public release                            |
| Example DPC    | Dùng Android SDK public release                       |

Với Go module trong `go/`, release tags cần tuân thủ quy tắc tagging cho module nằm trong subdirectory, ví dụ `go/v1.0.0`.

Android có thể dùng `android/v1.0.0` hoặc release naming tương đương, publish lên Maven repository đã chọn.

CI cần bảo đảm:

- Public SDK build độc lập.
- Không có dependency ngược tới examples.
- Generated code reproducible.
- Go và Android versions đang phát hành có compatibility matrix.
- Published SDK không import code nội bộ của sample applications.
- Android AAR metadata/manifest được kiểm tra.
- APK signing keys và publish secrets không nằm trong repository.

Nên công bố server/Agent compatibility window; không giả định toàn bộ phiên bản v1 trong tương lai đều tương thích với nhau nếu protocol behavior thay đổi.

## 16. Acceptance criteria của v1.0

Kylok v1.0 release checklist

0/13

SDK ready to use

Go constructor với required options hợp lệ hoạt động cùng default implementations, không cần host implement storage, transport hoặc management logic

Android AAR integration

Một host APK mới nhúng AAR, build được và có đầy đủ provisioning components qua manifest merge

Offline QR provisioning

Factory-reset device tải DPC từ LAN bằng QR và hoàn tất provisioning khi WAN bị chặn

Secure enrollment

Device có identity riêng, enrollment token single-use, có khả năng revoke và reconnect

Device management

Server quản lý devices, policies và reported state qua public Go API

APK deployment

Cài/cập nhật signed APK từ local server và xác minh installation result

DPC self-update

Host APK chứa DPC có thể cập nhật mà vẫn duy trì Device Owner identity và server connection

Basic policies

Required apps, kiosk, screen capture và restrictions đã xác định hoạt động và có compliance reports

Offline recovery

Không mất trạng thái quan trọng khi mất LAN, reboot hoặc process restart

Custom hooks

Host thay được storage, artifacts, events và mở rộng policy theo documented interfaces

Host neutrality

Hai examples end-to-end không phụ thuộc fDCMS hoặc một application framework cụ thể

Compatibility

Automated API matrix và physical-device provisioning tests được hoàn thành theo thiết bị hỗ trợ

Public release

Phát hành Go module, Android AAR, docs và examples với CI/release pipeline

&#x20;Copy checklist

## 17. Các quyết định kiến trúc chính thức đề xuất

| ID      | Decision                                                               |
| ------- | ---------------------------------------------------------------------- |
| ADR-001 | Monorepo, hai SDK products, hai examples                               |
| ADR-002 | Go SDK và Android AAR library-first                                    |
| ADR-003 | Required options được validate; optional dependencies có defaults      |
| ADR-004 | Android API 33–37, Fully Managed Device Owner                          |
| ADR-005 | Native Android Enterprise APIs + Google Test DPC reference             |
| ADR-006 | Protobuf contracts, gRPC-compatible transport                          |
| ADR-007 | SQLite default, custom store qua public interfaces                     |
| ADR-008 | Local filesystem artifact store default, hỗ trợ custom resolver        |
| ADR-009 | QR provisioning từ LAN là mandatory acceptance gate                    |
| ADR-010 | Agent-initiated synchronization, offline-first reconciliation          |
| ADR-011 | Device authentication tách biệt host management authorization          |
| ADR-012 | Public management facade, internal engine, optional transport adapters |
| ADR-013 | Minimal examples chỉ sử dụng public SDKs                               |
| ADR-014 | Capability-driven policy execution và observed-state reporting         |
| ADR-015 | Independent SDK versioning, protocol compatibility policy              |

## 18. Thứ tự triển khai thực tế

Tôi đề xuất bắt đầu từ M0 + phần tối thiểu của M1, thay vì scaffold mọi module ngay.

Trước tiên cần có một signed `minimal-dpc` APK thực sự nhúng được AAR, một local Go server tạo QR và phân phối APK, cùng một tablet Android 13–17 chạy được quá trình provisioning không có WAN. Đây là bài kiểm tra quan trọng nhất.

Khi đã vượt qua QR provisioning, tiếp tục chuẩn hóa enrollment và synchronization thành contract. Sau đó lần lượt thêm local APK deployment, policy reconciliation, customization interfaces và production hardening.

Tôi sẽ giữ bốn điều kiện không thay đổi trong suốt quá trình phát triển:

1. Default path phải hoạt động hoàn chỉnh với required options hợp lệ, không cần consumer viết implementation phụ trợ.
2. Custom path phải thay được dependencies quan trọng mà không fork Kylok hoặc chỉnh code nội bộ.
3. Mọi tính năng bắt buộc phải chứng minh hoạt động trên thiết bị thật, không chỉ pass unit tests.
4. Cả hai examples phải luôn build và chạy dựa trên public API, vì đó là bằng chứng rõ nhất rằng Kylok thực sự reusable.

Đây là baseline thiết kế tôi đề xuất dùng để bắt đầu triển khai Kylok v1.0.