# Kylok roadmap

Kylok is building a self-hosted Android device management SDK for Go servers and Kotlin Android apps, starting with Fully Managed Device Owner deployments on local networks.

**Status: design stage.** Repository documentation is initialized. No SDK implementation, published package, or physical-device compatibility result is available. Every milestone below is **planned**; unchecked items are requirements, not completed features.

[Project overview](README.md) · [Architecture](docs/architecture.md) · [Implementation plan](docs/implementation-plan.md)

## How to read this roadmap

Milestones describe user-visible outcomes and the evidence required to ship them. They are ordered by dependency and risk, with no committed dates. The implementation plan breaks them into work packages. A milestone closes only when its exit gate has recorded evidence; a merged change alone does not establish device support.

The first work combines M0 with the smallest part of M1 needed to build and test provisioning. Full scaffolding and feature expansion follow the M0 gate. API 33–37 is the design target; actual model and firmware support will be published from test results.

## Milestones

| Milestone | Outcome | Depends on | Status |
| --- | --- | --- | --- |
| [M0 — Feasibility and compatibility](#m0--feasibility-and-compatibility) | A device provisions from LAN while WAN is blocked. | Minimal buildable Go/Android proof | Planned |
| [M1 — Monorepo and contracts](#m1--monorepo-and-contracts) | Both SDKs and examples build against shared contracts. | M0 gate for full expansion | Planned |
| [M2 — QR provisioning and enrollment](#m2--qr-provisioning-and-enrollment) | A provisioned device securely joins the registry. | M0, M1 | Planned |
| [M3 — Device sync and reliability](#m3--device-sync-and-reliability) | Devices recover state and work after interruptions. | M2 | Planned |
| [M4 — APK lifecycle](#m4--apk-lifecycle) | Signed applications and the DPC update with verified results. | M3 | Planned |
| [M5 — Basic policies and kiosk](#m5--basic-policies-and-kiosk) | Devices reconcile policies and report actual compliance. | M3, M4 | Planned |
| [M6 — Public SDK release](#m6--public-sdk-release) | Fresh hosts integrate published packages through public APIs. | M1–M5 and release gates | Planned |

### M0 — Feasibility and compatibility

- [ ] Build a signed minimal host APK embedding the initial Android AAR.
- [ ] Serve the APK and provisioning QR from a LAN HTTPS endpoint.
- [ ] Exercise provisioning callbacks, Device Owner assignment, and bootstrap certificate trust.
- [ ] Verify platform/toolchain availability and LAN access requirements across the target API range.
- [ ] Record device model, API level, firmware/build, test procedure, and WAN-blocking evidence.

**Exit gate:** at least one target physical device downloads the signed host APK and completes Fully Managed provisioning over LAN with WAN blocked. Record failures and limits explicitly. This proves feasibility for that configuration; it does not establish support for all target APIs or OEMs. If the gate fails, resolve the cause or revise the design before expanding implementation.

### M1 — Monorepo and contracts

- [ ] Establish Protobuf schemas, Buf tooling, one main Go module, one Android library module, and both examples.
- [ ] Define the first enrollment, device, synchronization, error, and idempotency contracts.
- [ ] Pin the build toolchain and make protocol generation reproducible.
- [ ] Add build, schema lint, generation, and compatibility checks in CI.

**Exit gate:** Go and Kotlin compile from the same schema, and both examples build using only public SDK APIs. No SDK depends on an example or consumer application.

### M2 — QR provisioning and enrollment

- [ ] Generate metadata from the signed APK and validate it before QR creation.
- [ ] Serve immutable bootstrap artifacts reachable before device authentication.
- [ ] Implement expiring, scope-bound, single-use enrollment tokens and atomic consumption.
- [ ] Persist device identity and credentials; support safe retry when an enrollment response is lost.
- [ ] Supply the default SQLite and filesystem path, plus required Android lifecycle components.

**Exit gate:** factory reset → scan QR → download DPC → Device Owner → enroll → one device appears in the registry. Expired tokens, replay, and response-loss retries produce the documented outcomes without duplicate identities.

### M3 — Device sync and reliability

- [ ] Add device inventory, capability reports, and revisioned desired/reported state.
- [ ] Persist pending operations, progress, and unsent reports on both sides as required.
- [ ] Implement bounded retries, recovery scheduling, credential revocation, and reconnect behavior.
- [ ] Define and test transaction/concurrency guarantees for storage adapters.

**Exit gate:** LAN loss, server restart, Android process termination, and device reboot preserve identity and resume pending work without duplicate operation effects. Devices with revoked credentials cannot resume authorized management access.

### M4 — APK lifecycle

- [ ] Register immutable signed APK artifacts and verify digest, package, version, signing identity, and device compatibility.
- [ ] Download and stage single/universal APKs with recoverable progress.
- [ ] Integrate PackageInstaller callbacks, including requests for user action and failure results.
- [ ] Verify installed package/version before reporting success.
- [ ] Update the host DPC while preserving ownership, identity, and synchronization.

**Exit gate:** an administrator requests installation and update, then observes the actual installed result on a supported physical device. The supported silent path completes without user confirmation. DPC self-update reconnects with the same identity. Corrupt or incompatible APKs produce truthful failures.

### M5 — Basic policies and kiosk

- [ ] Manage required apps, minimum versions, and automatic updates.
- [ ] Configure a Lock Task allowlist and designated kiosk app after installation is verified.
- [ ] Support screen capture, camera, debugging, uninstall, and user/account modification restrictions.
- [ ] Apply managed-app runtime permission policies within platform limits and maximum time to lock.
- [ ] Report `APPLIED`, `PENDING`, `FAILED`, and `UNSUPPORTED` per policy result.

**Exit gate:** P0 policies reconcile idempotently, respect device capabilities, and report observed compliance. Failure in one policy step cannot turn the full policy into a success. Restrictive policies require explicit administrator intent.

### M6 — Public SDK release

- [ ] Stabilize public APIs and document extension concurrency, errors, timeouts, and lifecycle.
- [ ] Finish API/firmware compatibility evidence and required failure scenarios.
- [ ] Publish integration, provisioning, protocol, security, and compatibility guides.
- [ ] Publish versioned Go and Maven artifacts, source, and license/notice material.
- [ ] Test both examples and fresh external consumers against published packages.
- [ ] Document the supported server/agent compatibility window and release process.

**Exit gate:** a new Go host and a new Android host integrate published SDKs without copying source or implementing mandatory replacement adapters. All v1 acceptance criteria below have evidence.

## Beyond the MVP

These are candidate priorities, not scheduled releases. Open a focused proposal with a use case, Android constraints, and an observable acceptance criterion before implementation.

| Priority | Candidate capabilities | Constraints |
| --- | --- | --- |
| P1 | Remote reboot/lock; device groups and bulk policy assignment; scheduled deployments | Build on durable operations and existing authorization. |
| P1 | Managed Wi-Fi; deployment rollback strategy; PostgreSQL adapter | Respect Android signature/downgrade rules and adapter transaction guarantees. |
| P2 | Certificate lifecycle; advanced security policies; remote diagnostics | Define trust, access, retention, and recovery behavior for each feature. |
| P2 | Push over LAN; managed application configuration | Keep the existing recovery path and capability reporting. |
| P3 | Work Profile/COPE; OEM extensions; system OTA; remote screen viewing/control | Require separate management-mode or OEM feasibility work; Device Owner does not guarantee these capabilities. |

The MVP starts with single/universal APK installation. The artifact model should leave room for installation sets; split APK execution is not a v1 acceptance requirement. Avoid speculative operation engines or plugin frameworks for future features.

## v1 release acceptance

These thirteen gates track the original design's v1 criteria. All are pending.

- [ ] **Ready-to-use Go SDK:** validated required options activate complete default implementations.
- [ ] **Android AAR integration:** a fresh host builds with the required merged manifest and lifecycle components.
- [ ] **Offline QR provisioning:** supported factory-reset devices provision from LAN with WAN blocked.
- [ ] **Secure enrollment:** device identity, single-use tokens, revocation, and reconnect behave as documented.
- [ ] **Device management:** public Go APIs manage devices, policies, and reported state.
- [ ] **APK deployment:** local signed APK installs/updates have verified observed results.
- [ ] **DPC self-update:** the host DPC retains ownership, device identity, and server connectivity.
- [ ] **Basic policies:** required apps, kiosk, screen capture, and defined restrictions have accurate reports.
- [ ] **Offline recovery:** network loss, reboot, and process restart retain important state.
- [ ] **Custom hooks:** storage, artifacts, events, and policy extensions work through documented contracts.
- [ ] **Host neutrality:** both examples work end to end without fDCMS or another consumer dependency.
- [ ] **Compatibility:** automated API coverage and physical provisioning tests match the published support matrix.
- [ ] **Public release:** Go module, Android AAR, documentation, examples, and release pipeline are available.

## Proposing and tracking work

Use the [contributing guide](CONTRIBUTING.md) to propose work. Group implementation issues by milestone and link their acceptance evidence. Distinguish a design target, an emulator result, and a physical-device result in every compatibility claim. Maintainers should update this document when priorities or milestone status change; detailed task status belongs in linked [implementation issues](https://github.com/thienvu18/kylok/issues) as work begins.
