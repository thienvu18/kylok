# Stable release readiness

This checklist gates **stable v1 and later** publication in `scripts/validate-release.py`. Development prereleases may ship while items remain open. It tracks physical and operational qualification alongside the [roadmap](../ROADMAP.md). Check an item only after linking candidate-specific evidence in [current validation](compatibility.md).

## Acceptance

- [x] **Ready-to-use Go SDK:** validated durable defaults; automated source/package consumers recorded in [compatibility](compatibility.md).
- [x] **Android AAR integration:** independent debug/minified hosts build with merged components; [automated evidence](compatibility.md).
- [ ] **Offline QR provisioning:** target firmware completes factory-reset provisioning with WAN blocked throughout.
- [x] **Secure enrollment:** atomic scoped identity/token/recovery/revocation contracts have [automated evidence](compatibility.md); physical reconnect remains part of recovery.
- [x] **Device management:** public device/app/policy/observed-state APIs have [automated evidence](compatibility.md).
- [ ] **APK deployment:** signed v1 install and same-signer v1→v2 update passed on one POCO development run; incompatible, rejected, corrupted and other failure paths still need physical evidence.
- [ ] **DPC self-update:** actual replacement preserves owner, identity, journals and connectivity.
- [ ] **Basic policies:** camera, screenshot and account restriction apply/release passed on one POCO run; uninstall prevention failed despite `APPLIED` readback. Kiosk, runtime permission, secure timeout, Add user and other firmware checks remain open.
- [ ] **Offline recovery:** same-data server restart, LAN disconnect/reconnect, offline reboot and later automatic sync passed on one POCO run; process force-stop recovery and WAN-isolated provisioning remain open.
- [ ] **Custom hooks:** current integration contracts and declared durable providers/extensions require candidate-specific conformance and lifecycle evidence.
- [ ] **Host neutrality:** package consumers and host integration have automated coverage; the independent configuration app passed physical receipt/application/release on current source. Physical `HostOptions` consumer coverage remains open.
- [ ] **Compatibility:** supported candidate device/firmware and server/agent pairing matrix is complete.
- [x] **Public packages:** Go/AAR packages are available through the [Go module proxy](https://proxy.golang.org/github.com/thienvu18/kylok/go), [Maven Central](https://central.sonatype.com/artifact/io.github.thienvu18.kylok/kylok-dpc) and [GitHub Releases](https://github.com/thienvu18/kylok/releases); the [documentation site](https://thienvu18.github.io/kylok/) is available.

Before stable publication, review all thirteen items against the exact candidate and supported device window. The release validator requires thirteen checked entries with evidence links. Hardware outcomes remain maintainer-owned; see [testing](testing.md) and [physical checks](provisioning.md).
