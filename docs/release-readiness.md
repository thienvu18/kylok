# Stable release readiness

This checklist gates **stable v1 and later** publication in `scripts/validate-release.py`. Development prereleases may ship while items remain open. It tracks physical and operational qualification alongside the [roadmap](../ROADMAP.md). Check an item only after linking candidate-specific evidence in [current validation](compatibility.md).

## Acceptance

- [x] **Ready-to-use Go SDK:** validated durable defaults; automated source/package consumers recorded in [compatibility](compatibility.md).
- [x] **Android AAR integration:** independent debug/minified hosts build with merged components; [automated evidence](compatibility.md).
- [ ] **Offline QR provisioning:** target firmware completes factory-reset provisioning with WAN blocked throughout.
- [x] **Secure enrollment:** atomic scoped identity/token/recovery/revocation contracts have [automated evidence](compatibility.md); physical reconnect remains part of recovery.
- [x] **Device management:** public device/app/policy/observed-state APIs have [automated evidence](compatibility.md).
- [ ] **APK deployment:** qualify install, same-signer update and failure handling across the declared device matrix; automated integrity and installer contracts do not establish OEM behavior.
- [ ] **DPC self-update:** actual replacement preserves owner, identity, journals and connectivity.
- [ ] **Basic policies:** qualify actual camera, screenshot, account, uninstall, kiosk, permission and secure-timeout behavior on each supported firmware. A reported policy setter/readback is not sufficient evidence of enforcement.
- [ ] **Offline recovery:** qualify server restart, LAN loss/return, offline reboot, agent process recovery and WAN-isolated provisioning on the declared network/device matrix.
- [ ] **Custom hooks:** current integration contracts and declared durable providers/extensions require candidate-specific conformance and lifecycle evidence.
- [ ] **Host neutrality:** qualify independent host configuration and managed-app receipt/application/release on the declared hardware; package consumers and host integration also need maintained automated coverage.
- [ ] **Compatibility:** supported candidate device/firmware and server/agent pairing matrix is complete.
- [x] **Public packages:** Go/AAR packages are available through the [Go module proxy](https://proxy.golang.org/github.com/thienvu18/kylok/go), [Maven Central](https://central.sonatype.com/artifact/io.github.thienvu18.kylok/kylok-dpc) and [GitHub Releases](https://github.com/thienvu18/kylok/releases); the [documentation site](https://thienvu18.github.io/kylok/) is available.

Scheduled APK windows, reboot/secure-lock observation and managed Wi-Fi/CA lifecycle also require exact candidate/device evidence; their automated models do not close physical gates.

Before stable publication, review all thirteen items against the exact candidate and supported device window. Verify the uninstall control on each supported firmware; if the platform cannot enforce it, make that limitation and remediation path explicit. The release validator requires thirteen checked entries with evidence links. Hardware outcomes remain maintainer-owned; see [testing](testing.md) and [physical checks](provisioning.md).
