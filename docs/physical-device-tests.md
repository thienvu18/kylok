# Physical device test runbook

Run this procedure for **each model, firmware and transport configuration you intend to support**. Start with the POCO Pad X1 / Android 15 and server `192.168.101.5`. A pass on that tablet does not certify API 33–37 or another firmware. Current source includes N1–N4. The POCO run is summarized in the [device report](validation/2026-10-27-poco-pad-x1.md); it covers the source build and online LAN conditions recorded there.

Run the commands in order and stop on any error. Use `PASS`, `FAIL`, `NOT RUN` or `BLOCKED`, with actual observations. Never convert an unavailable fixture or an OEM-denied action into a pass. The [result template](validation/physical-device-template.md) covers the complete checklist; [compatibility](compatibility.md) contains recorded outcomes only. Run against a resettable lab tablet. Do not replace an existing deployment's database or keys.

## 1. Prepare the computer and a private run directory

Keep internet available on the computer while building. From the repository root:

```sh
export JAVA_HOME=$(/usr/libexec/java_home -v 17)
export ANDROID_HOME="$HOME/Library/Android/sdk"
export PATH="$JAVA_HOME/bin:$ANDROID_HOME/platform-tools:$ANDROID_HOME/build-tools/37.0.0:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
export RUN="$HOME/.local/share/kylok-physical/poco-http-$(date +%Y%m%d-%H%M%S)"
umask 077
mkdir -p "$(dirname "$RUN")"
mkdir "$RUN"
KYLOK_HOST_VERSION_CODE=1 ./scripts/prepare-local-server.sh 192.168.101.5 http
. "$HOME/.local/share/kylok-local-server/192.168.101.5/http.env"
export KYLOK_DATA="$RUN/data"
export KYLOK_SCOPE=poco-physical
export DPC_PACKAGE=io.github.thienvu18.kylok.example
export TEST_PACKAGE=dev.kylok.configuration
mkdir "$KYLOK_DATA"
cp docs/validation/physical-device-template.md "$RUN/results.md"
```

Never run the old `run-local-server.sh` for this run: the commands below use the fresh run database while retaining the existing signing key and immutable APK files.

Record the source and keep a rebuildable snapshot, including uncommitted development changes:

```sh
git rev-parse HEAD > "$RUN/base-commit.txt"
git status --short > "$RUN/worktree-status.txt"
git diff HEAD --binary > "$RUN/working-tree.patch"
python3 - "$RUN" <<'PY'
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
import hashlib, json, os, shlex, sys, tarfile
run = Path(sys.argv[1])
spec = spec_from_file_location('kylok_package', 'scripts/package-release.py')
package = module_from_spec(spec); spec.loader.exec_module(package)
paths = package.source_paths()
digest = hashlib.sha256()
with tarfile.open(run / 'source.tar.gz', 'w:gz') as archive:
    for path in paths:
        digest.update(str(path).encode() + b'\0' + hashlib.sha256(path.read_bytes()).digest())
        archive.add(path, arcname=str(path), recursive=False)
(run / 'source-tree.sha256').write_text(digest.hexdigest() + '\n')
keys = ('RUN', 'JAVA_HOME', 'ANDROID_HOME', 'KYLOK_PUBLIC_URL', 'KYLOK_APK', 'KYLOK_DATA', 'KYLOK_SCOPE', 'DPC_PACKAGE', 'TEST_PACKAGE', 'KYLOK_LOCAL_DIR')
(run / 'run.env').write_text(''.join('export '+key+'='+shlex.quote(os.environ[key])+'\n' for key in keys))
PY
cp "$KYLOK_APK" "$RUN/dpc-v1.apk"
tools/apk-metadata -apk "$RUN/dpc-v1.apk" > "$RUN/dpc-v1.json"
printf 'Private evidence directory: %s\n' "$RUN"
```

The source snapshot intentionally includes public source only, using the release packager's exclusions. The evidence directory stays outside Git. In another terminal, set `RUN` to the printed **same path**, then source `"$RUN/run.env"` and set the PATH above; do not generate a second run directory.

## 2. Build signed application/update fixtures before blocking WAN

The independent configuration app has no Kylok dependency. Build two release APKs, signing both with the same existing lab key:

```sh
(
set -e
for version in 1 2; do
  ./android/gradlew -p android :configuration-app:assembleRelease \
    "-PconfigurationVersionCode=$version"
  apksigner sign --ks "$KYLOK_LOCAL_DIR/host.p12" --ks-key-alias kylok-host \
    --ks-pass "file:$KYLOK_LOCAL_DIR/signing-password" \
    --out "$RUN/config-v$version.apk" \
    examples/configuration-app/build/outputs/apk/release/configuration-app-release-unsigned.apk
  tools/apk-metadata -application -apk "$RUN/config-v$version.apk" > "$RUN/config-v$version.json"
done
KYLOK_HOST_VERSION_CODE=2 ./scripts/prepare-local-server.sh 192.168.101.5 http
cp examples/minimal-dpc/build/outputs/apk/release/minimal-dpc-release.apk "$RUN/dpc-v2.apk"
tools/apk-metadata -apk "$RUN/dpc-v2.apk" > "$RUN/dpc-v2.json"
)
```

Stop if any build/sign/inspection command failed. Check all four metadata files: ordinary app versions 1/2 share a package and certificate; DPC versions 1/2 share a package, certificate and receiver, and all APKs are non-debuggable. The preparation helper rewrites its global `http.env` for version 2; **continue using the saved run.env and v1 bootstrap**, not that global environment.

```sh
python3 - "$RUN" <<'PY'
from pathlib import Path
import json,sys
r=Path(sys.argv[1])
for prefix in ('config','dpc'):
    one=json.loads((r/f'{prefix}-v1.json').read_text())
    two=json.loads((r/f'{prefix}-v2.json').read_text())
    assert (one['version_code'],two['version_code'])==(1,2)
    for key in ('package_name','signing_certificates_sha256'):
        assert one[key]==two[key], (prefix,key)
    assert not one['debuggable'] and not two['debuggable']
    if prefix=='dpc': assert one['receiver']==two['receiver']
print('Signed version/package/signer pairs verified; no device action performed.')
PY
```

## 3. Start the server and define management commands

In terminal A, with the saved run.env loaded:

```sh
./out/minimal-server serve -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
  -data "$KYLOK_DATA" -listen 192.168.101.5:8080 -ui-listen 127.0.0.1:8081 \
  2>&1 | tee -a "$RUN/server.log"
```

Keep this terminal running. In terminal B, load the saved environment and define:

```sh
kylok() {
  ./out/minimal-server "$@" -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
    -data "$KYLOK_DATA" -scope "$KYLOK_SCOPE"
}
snapshot() {
  label="$1"
  kylok list > "$RUN/$label-devices.json"
  kylok operations -device "$DEVICE_ID" > "$RUN/$label-operations.json"
  kylok policy -device "$DEVICE_ID" > "$RUN/$label-policy.json"
}
printf '{}\n' > "$RUN/release.json"
```

Open `http://127.0.0.1:8081`. Read the administrator token locally from `"$KYLOK_DATA/console-token"`; do not paste it into chat, screenshots or public reports. Select scope `poco-physical`. Allow inbound API traffic through the computer firewall, and disable Wi-Fi client isolation. The console remains on the computer; tablets use port 8080.

## 4. Provision with WAN blocked

1. On the router, block WAN for the tablet's test LAN/SSID **before scanning**, while preserving access to `192.168.101.5:8080` and any needed LAN DNS/DHCP. A dedicated test SSID/VLAN avoids rules tied to a MAC address that may change during reset. Save the router rule and counters as private evidence. A Wi-Fi icon or failed website alone does not prove WAN isolation.
2. Factory-reset the test tablet if it is not already at the welcome screen. Enter the enterprise QR flow, commonly by tapping the same spot six times. Join the test Wi-Fi if prompted.
3. In the console create a new enrollment QR with API/APK origin `http://192.168.101.5:8080` and enough lifetime, then scan it. Keep the WAN block throughout QR scanning, APK download, managed setup and first authenticated sync.
4. Observe completion and the Kylok app. Expect Device Owner reported true and one `MANAGED` device. If OEM setup needs internet or cannot download its scanner, record the exact failing stage; do not enable WAN and call that an offline pass. An online comparison is a separate reset/run.
5. In terminal B run `kylok list`. Copy the new opaque ID into `export DEVICE_ID='...'`; confirm there is exactly one device in this fresh scope. Run `snapshot enrollment`.
6. Record device model, Android/API, exact firmware/fingerprint, transport, timestamps and WAN evidence in `results.md`. The ID should remain constant in every later check.

Do not use `adb dpm set-device-owner` as a provisioning substitute. QR output is private and must not be included in shared results. See [Android provisioning](https://developers.google.com/android/management/provision-device) for the platform flow; Kylok uses its own DPC/server rather than Google's management service.

## 5. Optional USB evidence and the idle baseline

After setup, enable USB debugging if the firmware permits it, connect USB and authorize the computer. If more than one device is attached, set `ANDROID_SERIAL` to the intended device's serial privately. These observations are optional when OEM restrictions prevent ADB; record their absence rather than inferring them.

```sh
adb devices -l
adb shell getprop ro.product.model > "$RUN/device-model.txt"
adb shell getprop ro.build.version.sdk > "$RUN/device-api.txt"
adb shell getprop ro.build.fingerprint > "$RUN/device-fingerprint.txt"
adb shell dumpsys device_policy > "$RUN/device-policy-initial.txt"
adb shell dumpsys package "$DPC_PACKAGE" > "$RUN/dpc-initial.txt"
```

Verify the Device Owner component matches `io.github.thienvu18.kylok.example/io.github.thienvu18.kylok.dpc.KylokDeviceAdminReceiver`. Record original camera, screen capture, lock timeout and target-app permission behavior before changing policies. Leave policies empty initially. Hide the app for at least one normal scheduling interval (default 15 minutes; allow scheduling delay), then confirm `last_seen_at` advances without opening it. Record actual latency; do not assume realtime delivery. On HyperOS, first test default power settings; record any later battery/autostart exceptions as a separate condition.

The Android dashboard has **Sync now** (schedules work) and **Refresh view** (reads local state); tapping either is not a success result.

## 6. Install the ordinary app, then apply configuration

Register and assign version 1:

```sh
kylok register -application-apk "$RUN/config-v1.apk" > "$RUN/register-config-v1.json"
export APP_V1_SHA=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["sha256"])' "$RUN/register-config-v1.json")
kylok assign -device "$DEVICE_ID" -digest "$APP_V1_SHA" -request-key app-v1-001 -timeout 1h \
  > "$RUN/assign-config-v1.json"
```

Request a sync and wait for observed installation. Verify: app is present and launchable, version code is 1, signer matches, and server operation is `SUCCEEDED`. Record whether Android required interaction; if confirmation was required, record the pending state **before** approving or rejecting it. Do not install with `adb install` to claim Kylok deployment success.

```sh
adb shell dumpsys package "$TEST_PACKAGE" > "$RUN/config-installed-v1.txt"
snapshot app-v1
cat > "$RUN/configuration.json" <<'JSON'
{
  "required_packages": ["dev.kylok.configuration"],
  "managed_configurations": [{
    "package_name": "dev.kylok.configuration",
    "values": {"entries": [
      {"key": "display_name", "value": {"string": "Reception tablet"}},
      {"key": "allow_refresh", "value": {"boolean": false}}
    ]}
  }]
}
JSON
kylok set-policy -device "$DEVICE_ID" -request-key config-001 -policy "$RUN/configuration.json"
```

After sync, open the independent configuration app. Confirm the actual title is **Reception tablet** and its Refresh button is disabled. Separately verify `configuration:dev.kylok.configuration` is `APPLIED` with only a `sha256:` digest in observed status. Capture an external photo or a screenshot of these nonsecret fixture values.

Change the JSON title to **Reception updated**, set `allow_refresh` to `true`, and submit with key `config-002`. Expect the title/button to change while the app is open. Close/reopen the app (or force-stop **this ordinary app**, then launch it) and verify settings persist. Reboot later in step 8 and confirm again.

Finally, submit `release.json` with a fresh key and wait for cleanup. Expect the app's original/default title **Configuration example** and enabled Refresh button, plus confirmed cleanup. A digest match proves DPC readback; the title/button observations prove the app consumed/applied its own settings.

## 7. Update, retry and downgrade

```sh
kylok register -application-apk "$RUN/config-v2.apk" > "$RUN/register-config-v2.json"
export APP_V2_SHA=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["sha256"])' "$RUN/register-config-v2.json")
kylok assign -device "$DEVICE_ID" -digest "$APP_V2_SHA" -request-key app-v2-001 -timeout 1h \
  > "$RUN/assign-config-v2.json"
```

Wait for version 2 and observed success. Repeat the **same assignment key** with the same digest: expect the same operation ID, not a second installation. Reusing that key with the v1 digest must conflict. An intentional downgrade uses a **new** key and v1 digest: expect truthful failure rather than v1 being silently installed. Afterwards assign v2 with another new key to restore desired state before proceeding.

Record package/version/signer and `snapshot app-v2`. Use new request keys for intentional changes/retries; repeating an old key retrieves its original operation.

## 8. Recovery and interrupted work

Test one disturbance at a time; capture before/after snapshots and identity. Leave WAN blocked when testing LAN independence.

| Test | Actions | Required observation |
| --- | --- | --- |
| Server restart | Stop terminal A with Ctrl-C, wait, restart the **same command/data/APK**. Tap Sync after restart. | Same device ID and desired state; committed reports/operations retained. |
| LAN unavailable | Turn tablet Wi-Fi off; submit a changed configuration policy while disconnected; re-enable Wi-Fi and request sync. | Offline device does not claim the unseen revision; it receives/applies it after reconnect. |
| Cached policy repair | With a policy already received, disconnect LAN and restart/reboot; inspect actual policy. | Previously cached managed policy remains/restores without server access. |
| Ordinary app restart | Force-stop `dev.kylok.configuration`, then launch it. | Configured values are read again; DPC identity/work is unchanged. |
| Tablet reboot | Reboot normally; unlock once if required; leave the DPC unopened initially. | Owner/identity retained; background recovery and fresh server inventory; no duplicate completed effects. |
| Idle/background | Hide apps, let tablet idle with its normal OEM power settings. | Record whether/when authenticated sync recovers; distinguish automatic recovery from manually opening Sync. |
| Interrupted download | Assign a sufficiently large valid signed APK; while Operations shows active download, disconnect LAN, then reconnect. | Download progress/retry is truthful; final bytes/version/signer verified. If the fixture finishes before interruption, mark `NOT RUN`. |
| Process interruption | If permitted, background the DPC and use `adb shell am kill "$DPC_PACKAGE"`; record whether its PID actually changes, then recover. | Same identity/checkpoints; no duplicate committed installer effect. A denied/no-op kill is `BLOCKED`, not a tested process death. |
| Force-stop | Only if OEM permits stopping the DPC, record it separately; explicitly launch it afterwards. | Recovery after relaunch, without credential rotation. Force-stop can suppress background execution and is not equivalent to ordinary process death. |
| Around installer commit/callback | Interrupt a real install near commit using permitted process/reboot actions and record the exact observed stage. | Existing session/installed state reconciles; no false success or duplicate commit. If timing cannot be established, `NOT RUN`. |
| Enrollment response interruption | On a separate fresh reset/run, interrupt LAN during enrollment and restore it. | Same bound attempt recovers one identity. Without evidence the response was lost, record only interrupted setup, not proven response-loss recovery. |

ADB commands follow [Android's adb documentation](https://developer.android.com/tools/adb). Do not claim process death from a command's exit code alone. Do not erase protected app data to simulate a restart.

## 9. Basic policies, release and permissions

Use the console's policy editor, or JSON files plus `kylok set-policy`. These are **full replacements**; test one independent setting at a time, then submit `{}` with a fresh key and observe restoration before the next setting.

| Setting | Desired value | Physical check |
| --- | --- | --- |
| `camera_disabled` | `true` | Camera use is blocked; release restores the recorded baseline. |
| `screen_capture_disabled` | `true` | OS screenshot/screen recording is blocked; use an external photo for evidence. Release restores baseline. |
| `maximum_time_to_lock_millis` | `30000` | With an existing secure screen lock, idle device locks within the effective timeout. Record other admin/user limits; release restores baseline. |
| `restrictions` | `no_modify_accounts`, `no_add_user`, `no_uninstall_apps`, each enabled individually | Corresponding user action is restricted and release restores only Kylok's changes. |
| `no_debugging_features` | enabled | Test **last among restrictions**, with working server/UI release available, because it can disable USB debugging. |
| `permissions` | GRANTED → DENIED → DEFAULT for a declared runtime permission | Verify actual permission/use, plus per-rule state. DEFAULT releases policy and does not promise revocation. |
| Invalid permission | An undeclared/unknown runtime permission on an assigned required app | Truthful failed/unsupported/pending result, no fabricated grant. |

Recorded POCO/API 35 outcomes: camera and screenshot blocking/restoration and `no_modify_accounts` were observed; `no_debugging_features` hid Developer options and USB debugging remained off after release. `no_uninstall_apps` failed because Android still removed the sample app while the rule reported `APPLIED`. Secure timeout could not be checked without a PIN/password/pattern; no Add user action existed. Kiosk and runtime permissions were not run. See the [sanitized run report](validation/2026-10-27-poco-pad-x1.md); these results do not qualify other firmware.

The configuration sample intentionally requests no runtime permissions. A **separate signed test app that declares the permission** is required for positive permission tests; until supplied/tested, record those rows `NOT RUN`. A rejected sensor grant on this firmware must be recorded as such, not coerced through adb. See [policy semantics](policies.md).

Repeat one received policy and its release across LAN loss, reboot and a permitted process interruption. Verify both actual behavior and server compliance; a setter returning successfully is insufficient. Keep factory reset unrestricted.

## 10. Kiosk entry, prerequisites and exit

Only start after testing `{}` release successfully. Keep terminal B/server connectivity available.

```sh
cat > "$RUN/kiosk.json" <<'JSON'
{
  "required_packages": ["dev.kylok.configuration"],
  "lock_task": {
    "packages": ["dev.kylok.configuration"],
    "kiosk_package": "dev.kylok.configuration"
  }
}
JSON
kylok set-policy -device "$DEVICE_ID" -request-key kiosk-001 -policy "$RUN/kiosk.json"
```

1. Wait for exact required app/version/signer prerequisites. Confirm the app enters **managed Lock Task**, not user screen pinning. Check actual Home/Recents escape behavior and matching per-rule report.
2. If ADB is available, save `adb shell dumpsys activity > "$RUN/kiosk-activity.txt"` and inspect the Lock Task state (`LOCKED`, not `PINNED`; dump format varies by firmware).
3. Reboot/disconnect LAN and record recovery/actual locking.
4. Exercise prerequisite drift by assigning a different required app version while installation is interrupted. Expect kiosk to be withheld/suspended until verified prerequisites return; restore v2 afterwards. If drift cannot be produced, mark it untested.
5. If testing kiosk replacement, use a second verified launchable assigned app; otherwise mark replacement `NOT RUN`.
6. Release remotely:

```sh
kylok set-policy -device "$DEVICE_ID" -request-key kiosk-release-001 -policy "$RUN/release.json"
```

Wait for observed cleanup and confirm Home/Recents work again, with prior unmanaged settings preserved. A policy cannot release before the tablet receives it; maintain connectivity. [Android Lock Task documentation](https://developer.android.com/work/dpc/dedicated-devices/lock-task-mode) explains the platform distinction.

## 11. DPC self-update

Exit kiosk/release restrictions first. Keep the running server on its original verified v1 bootstrap APK; register v2 as a **managed app**, not as a replacement for the running server's bootstrap pointer:

```sh
kylok register -application-apk "$RUN/dpc-v2.apk" > "$RUN/register-dpc-v2.json"
export DPC_V2_SHA=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["sha256"])' "$RUN/register-dpc-v2.json")
kylok assign -device "$DEVICE_ID" -digest "$DPC_V2_SHA" -request-key dpc-update-001 -timeout 1h
```

Observe the host process/package replacement. Expect host version code 2, unchanged package/receiver/signer, Device Owner retained, the **same** device ID, automatic reconnect and observed `SUCCEEDED`. Save package/owner evidence and `snapshot dpc-v2`. Reboot and check again. Reusing the assignment key must not update again. Do not clear DPC data or switch signing keys.

## 12. Failure and transport qualification

Use these rows to record the rest of the physical checklist honestly. They require dedicated fixtures or another reset and are not implied by the happy path:

| Case | Procedure / boundary |
| --- | --- |
| Wrong signer | Use a higher-version APK with the **same ordinary app package** signed by an unrelated lab key. Register/assign it and verify signer mismatch rejection, original app intact and truthful failure. Never replace the DPC's signing key. |
| Corrupt APK upload | Corrupt a disposable copy and register it; expect signature/metadata rejection. This proves server intake rejection, **not device download checksum enforcement**. |
| Corrupt downloaded content | Needs a controlled lab fault fixture returning bytes different from verified metadata. Do not edit the live immutable store and infer that the device checked a digest when the server refused first. Record `NOT RUN` until the device boundary is actually exercised. |
| Incompatible SDK/ABI/features | Assign genuinely incompatible signed fixtures; record the exact incompatibility and truthful rejection without app replacement. |
| Low storage | Use a controlled low-storage lab device/fixture; capture available space, failed/pending install and recovery after freeing space. Do not fill an ordinary user's tablet. |
| Missing/user-action installer callback | Requires an actual observed OS path or controlled interruption. Record user rejection separately from missing callback; simulated JVM outcomes do not count. |
| Expired or reused QR | Separate reset attempts: expired/consumed token must not create a new managed identity. Record Setup Wizard stage and server outcome. |
| HTTPS API with private CA | Separate fresh reset, fresh server data and enrollment; build the customCA DPC and use HTTP bootstrap plus HTTPS API as described below. |
| DNS origin | Separate enrollment with a real LAN-resolvable hostname; verify that resolution and, for HTTPS, matching certificate trust. Do not invent a domain if the LAN has none. |
| Untrusted HTTPS | Separate disposable attempt with a certificate the relevant client does not trust; connection/setup fails without HTTP downgrade. Setup Wizard and installed-app trust are separate. |
| API 37 LAN permission | Requires a physical API 37 target. The POCO/API 35 cannot qualify this path. |

For mixed private-CA enrollment, run `prepare-local-server.sh 192.168.101.5 https`, load its environment into a **new run**, and use fresh `KYLOK_DATA`. Serve the customCA APK with `-public-url https://192.168.101.5:8443 -listen 192.168.101.5:8443 -tls-cert "$KYLOK_TLS_CERT" -tls-key "$KYLOK_TLS_KEY" -bootstrap-url http://192.168.101.5:8080 -bootstrap-listen 192.168.101.5:8080`. Issue the QR with that same `-bootstrap-url`. Keep HTTP bootstrap while the installed app uses verified private-CA HTTPS. Do not reuse the already enrolled HTTP device or assume redrawing its QR changes its endpoint. See [local HTTPS commands](local-server.md#https-and-custom-ca).

The standard CLI uses SQLite. PostgreSQL/change-feed host tests are automated server integration checks; this physical run does not prove a PostgreSQL-backed host deployment. The default DPC uses default scheduling; testing custom `HostOptions` on hardware requires an APK that calls `configure` in its Application, such as the independent Maven consumer host. Record that build separately rather than attributing default-host results to it.

## Fleet and network qualification

Use paired SDKs from the candidate source and fresh development server data. Do not attribute the recorded POCO build to these APIs. Keep the signing identity and record the candidate APK/version. The [CLI](local-server.md#fleet-commands-and-network-lifecycle) and [fleet](fleet-operations.md)/[network](managed-network.md) contracts define submission and observed outcomes.

| Case | Manual action and required evidence |
| --- | --- |
| Group deployment | Create a group containing the test device, freeze a fleet request and advance it. Record the original group revision, per-device operation ID and observed install. Change membership; retrying that batch must not add targets or repeat the completed operation. More than one physical target needs its own record. |
| Absolute window | Schedule a compatible higher-version signed app with a future start/deadline. Sync before start: no installation effect. Sync inside the window: observe the assigned version. Queue another future request and cancel it before dispatch: it stays cancelled with no installation. A delivered/committed effect is a separate cancellation race and may finish. |
| Secure lock | With swipe-only unlock, submit `LOCK`: expect `SECURE_LOCK_REQUIRED`, never success. Set a lab PIN/password yourself, then submit a new key. Confirm the screen is locked and the server records secure/locked readback. Keep the credential available; the SDK does not create or recover it. |
| Reboot | Submit `REBOOT`, observe an actual restart, unlock, then record the same identity and a successful command with increased boot count. Reuse its key: no second reboot. Reconnection alone cannot pass this case. |
| Wi-Fi prerequisites | Use a baseline/recovery SSID and a different managed SSID; both must reach the same server IP/domain. Keep baseline reachable. Grant precise location and Nearby Wi-Fi devices in the example dashboard when prompted. Record unrelated saved networks/CAs without exposing credentials. |
| Apply and credential rotation | Assign a protected profile using a private immutable secret reference. Sync; confirm primary association and authenticated management/cleanup flags. Change the AP credential in a controlled lab, submit a new reference/key and confirm recovery-first rotation, reconnect and restart persistence. |
| Bad credentials / unreachable target | Submit an intentionally wrong managed credential or block the new route's server access. Confirm return to verified recovery, prior managed credentials retained and truthful failure/pending state. Retry the same revision: no repeated migration loop. Restore connectivity and use a new key to retry. |
| Lost primary / reboot | Remove primary connectivity while leaving recovery reachable. Confirm authenticated automatic recovery without deleting managed or unrelated configurations. Restart with LAN temporarily unavailable; record recovery after reconnect. |
| CA overlap and release | Use a trusted HTTPS origin with correct IP/DNS SANs. Deliver overlapping roots, rotate the server chain, then remove the old managed root. Verify management and APK download after each step. A CA already present or embedded in host base trust must remain usable after release. Expired/untrusted or hostname-mismatched chains must fail without HTTP downgrade. |
| Policy release | Submit an empty network document. Confirm the baseline route with remaining trust before managed resources disappear. If the server is reachable only through the root being removed, release must fail/persist recovery rather than claim cleanup. Verify unmanaged settings remain. |

Run network cases separately for LAN IP and a real LAN-resolvable hostname over HTTP/HTTPS. Mark unavailable combinations `NOT RUN`; do not invent DNS or TLS evidence. HTTP has no transport confidentiality. Revoke only after network/policy cleanup and evidence capture.

## 13. Revoke last

Release all managed policy first and confirm cleanup while the tablet can still sync. Then:

```sh
snapshot before-revoke
kylok revoke -device "$DEVICE_ID"
```

Request sync and reboot/retry. Expect rejected authenticated activity, no new identity and no new successful APK downloads. A previously cached policy/OS effect can remain after revocation; revocation does not wipe or automatically release a device. Record the last successful sync and later rejection independently of the local lifecycle label. Reusing a consumed QR must not restore access. Further enrollment tests require another reset/new token.

## 14. Finish the record

For each case fill the template with start/end times (include timezone), exact action, expected vs actual, IDs of relevant operations/revisions, and evidence filenames. Screenshot/photo filenames should be case IDs, for example `CFG-01-title-button.png`. Use sanitized screenshots and reviewed log excerpts; never share raw QR images, tokens, credentials, keystores, passwords or complete runtime databases.

Keep originals in the private run directory. Copy only the **sanitized completed report** and safe evidence into `docs/validation/`, then update [current validation](compatibility.md). The POCO run records online enrollment, app/configuration behavior, background sync, server/LAN/reboot recovery and selected policy checks; uninstall prevention failed, and WAN-blocked setup and several controls remain blocked or untested. Keep the corresponding roadmap/readiness gates open until exact build evidence closes them. A firmware update or source revision change needs a separate record.
