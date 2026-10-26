# Physical verification checklist

Use the [local server guide](local-server.md) for build/start/QR commands. Physical tests are performed by the maintainer. The [recorded POCO M2 run](validation/2026-10-25-poco-enrollment.md) passed initial LAN HTTP enrollment with internet available; it proves no later install/recovery outcome.

The [independent package consumers](sdk.md) prove Go-host execution and Android build/metadata integration only. For device tests, record the Go/Android SDK pair/source revision in addition to the host APK identity. Package versions and a successful minified build are not OS enforcement evidence.

## Provisioning

1. Use a resettable test device. Record exact model, API, firmware fingerprint and signed APK digest/signer.
2. For M0, block the device's WAN before scanning and keep LAN IP/DNS/API/APK access reachable. Record that block through the entire run.
3. Enter the OEM enterprise QR flow from the factory-reset welcome screen, join the LAN and scan the private QR. Do not substitute `adb dpm set-device-owner` as QR evidence.
4. Confirm signed APK download, Fully Managed setup and Device Owner. For HTTPS APK hosting, Setup Wizard must already trust the certificate; app-bundled private trust cannot change that earlier download.
5. Confirm one `MANAGED` registry identity and authenticated inventory. Open Kylok for sanitized ownership/callback/status. Record failures at their actual stage.

## Sync and installation

| Scenario | Required observation |
| --- | --- |
| LAN loss, process termination, server restart and reboot | Same identity; persisted requests/reports/work recover without duplicate effects. |
| Revoked credentials | Device cannot sync, download or recover enrollment; it does not silently re-enroll. |
| Signed app installation/update | Supported Device Owner path installs without unintended user interaction; actual package/version/signers match desired state. |
| User-action-required / rejected install | Pending/failure is reported; host UI can open the OS confirmation; no false silent success. |
| Wrong signer, corrupt APK, incompatible SDK/ABI/features, low space, downgrade | Truthful failure without success report or unintended installation. |
| Lost callback / process death around commit | Existing session/installed state is reconciled; no duplicate install session effect. |
| DPC self-update | Higher-version host APK uses the same application ID and compatible signing, remains Device Owner, reconnects with the same identity and reports observed success. |

Build an update with `KYLOK_HOST_VERSION_CODE=2 ./scripts/prepare-local-server.sh HOST http` and the same signing key, or pass `-PhostVersionCode=2` directly to Gradle. Enroll the initial host using the current schema before that test. Use a fresh assignment key for intentional retries/desired changes; reusing an existing key returns its original operation. Keep tests separate from ordinary fleet devices.

## Policies and kiosk

Use a current build/fresh development enrollment and the [policy commands](local-server.md#manage-policies). Keep server access available for an explicit `{}` replacement to release settings.

| Scenario | Required observation |
| --- | --- |
| Required app missing, wrong version/signer, or install failure | Per-app pending/failure; kiosk does not enter. Repair/install leads to observed compliance. |
| Kiosk entry, replacement, release | Actual managed Lock Task and matching package callback; screen pinning is insufficient. Allowlist changes/empty replacement exit old locking; independent host settings remain intact. |
| Camera, screen capture, timeout and selected restrictions | Check the actual UI/OS behavior and per-rule readback, including restoring original settings after removal. |
| Runtime permission grant/deny/default, sensor limits, undeclared permission | Check effective grant and policy state; rejected/unsupported results remain truthful. Default releases policy without promising revocation. |
| Process death/reboot/LAN loss during policy change or restoration | Same identity and journal; cached policies reconcile offline; no duplicate kiosk launch; unobserved or failed cleanup stays pending/failed. |
| Required-app drift while kiosk is active | Existing kiosk is suspended until exact assigned app prerequisites are observed again. |

Record exact build/artifact/network conditions and results in [compatibility](compatibility.md). Do not publish QR images, credentials, Wi-Fi passwords, private keys or personal identifiers. An online provisioning run is not an offline result; a successful setter/commit is not observed-state proof.
