# Kylok roadmap

Experimental; see [GitHub Releases](https://github.com/thienvu18/kylok/releases) for published development versions. Development APIs, schemas and databases may change without backward compatibility. Status distinguishes software/automated evidence from [physical results](docs/compatibility.md).

| Milestone | Outcome | Status |
| --- | --- | --- |
| M0 | LAN-only factory-reset provisioning with WAN blocked. | LAN HTTP setup passed on one POCO; WAN-blocked test pending. |
| M1 | Shared contracts, SDKs/examples and reproducible builds. | Implemented and locally verified; hosted CI not claimed. |
| M2 | Scoped enrollment, identity, recovery and revocation. | Implemented; initial physical enrollment passed on one configuration. |
| M3 | Durable sync, desired/observed state, progress and recovery. | Implemented; automated checks pass; physical recovery checklist remains. |
| M4 | Signed APK installation/update and DPC self-update. | Implemented; automated checks pass; physical installer/self-update checklist remains. |
| M5 | Basic policies and kiosk. | Implemented; automated checks pass; physical enforcement/kiosk checklist remains. |
| M6 | Published, documented SDKs with compatibility evidence. | Release implementation verified locally; remote publication and physical exit gates remain. |

M0–M4 recheck (2026-10-26): a fresh source copy passed `make verify`. The physical gates below remain open; automated checks do not close them.

### M0 — Feasibility and compatibility

Signed APK, QR and HTTP/HTTPS LAN hosting are available. Exit: a physical target provisions with WAN blocked, with model/firmware and network evidence recorded. The [POCO enrollment run](docs/validation/2026-10-25-poco-enrollment.md) had internet access and does not close this gate.

### M1 — Monorepo and contracts

Go and Android build from typed Protobuf; public-only examples, pinned tooling, lint/generation and CI targets exist. Development schema checks validate current source; no frozen backward-compatibility baseline applies yet.

### M2 — QR provisioning and enrollment

Single-use scoped tokens, atomic identity/receipt creation, key-bound response recovery, protected Android state, registry and revocation. Exit: reset → QR → signed APK → Device Owner → authenticated registry entry, plus automated expiry/replay/retry checks. Initial physical path passed on the recorded POCO configuration.

### M3 — Device sync and reliability

Revisioned desired apps, observed inventory/compliance, idempotent operations, leases, durable sync/report checkpoints, periodic/event recovery and revocation checks. Automated fault tests cover interruption and duplicate/stale delivery; real reboot/process/LAN-loss recovery remains in the [manual checklist](docs/provisioning.md).

### M4 — APK lifecycle

Verified registration, authorized resumable downloads, staged metadata/signing/compatibility checks, durable installer sessions, user-action/failure callbacks and observed success. The self-update path preserves identity/signing and reconciles after package replacement. Silent OS installation, reboot recovery and actual DPC replacement need physical evidence.

### M5 — Basic policies and kiosk

Scoped full-replacement policies, required-app prerequisites, Lock Task allowlists/designated kiosk, runtime permission rules, camera/screen capture, selected user restrictions and maximum time to lock. Durable baseline/attempt journals, readback, partial compliance and offline reconciliation are implemented. Automated tests cover scope/replay/revision binding, missing apps, faults, release and both transports; actual OEM enforcement, entry/exit and reboot checks remain [manual](docs/provisioning.md#policies-and-kiosk). See [policy semantics](docs/policies.md).

### M6 — Public SDK release

Documented public/adapter contracts, independent SDK versions, Go module/Maven distributions, license/source archives, checksums, optional event/status hooks and automatic per-merge development publication are implemented. Fresh hosts resolve packages without workspace substitutions; Android also builds with minification. Examples include an authenticated web console with QR origin selection and a read-only Android dashboard. [API/site generation](docs/development.md#documentation) covers Go/Android references and English/Vietnamese guides. [Release tooling](docs/releases.md) validates source/tag identity, distribution integrity and v1 gates. Protected-main PR checks and signed Maven Central/Go/GitHub release automation are configured. Hosted execution, registry availability and remote-consumer verification require recorded release evidence; physical evidence is unchanged. Development APIs remain free to change before v1.

## v1 release acceptance

These gates cover the full product, not only implemented software. None is certified for release yet.

- [ ] **Ready-to-use Go SDK:** complete validated defaults.
- [ ] **Android AAR integration:** fresh host integration and lifecycle evidence.
- [ ] **Offline QR provisioning:** physical LAN provisioning with WAN blocked.
- [ ] **Secure enrollment:** identity, tokens, revocation and reconnect evidence.
- [ ] **Device management:** public device/policy/reported-state APIs.
- [ ] **APK deployment:** verified physical installs and updates.
- [ ] **DPC self-update:** actual replacement retains owner/identity/connectivity.
- [ ] **Basic policies:** accurate kiosk/restriction compliance.
- [ ] **Offline recovery:** physical network/process/reboot recovery.
- [ ] **Custom hooks:** documented conforming storage/artifact/event/policy extensions.
- [ ] **Host neutrality:** fresh consumers and both examples work end to end.
- [ ] **Compatibility:** published matrix matches automated and physical evidence.
- [ ] **Public release:** Go/AAR artifacts, docs and release pipeline available.

Future candidates: groups/bulk assignments, reboot/lock, managed Wi-Fi and PostgreSQL; later diagnostics, certificates, push and managed app configuration. Work Profile/COPE, OEM features, OTA and remote control require separate feasibility work.
