# Local server and application deployment

API and APK origins support **HTTP or HTTPS, IP addresses or DNS names**. A domain/certificate is unnecessary for HTTP. HTTPS requires matching certificate trust; there is no trust-all or automatic downgrade. HTTP does not encrypt tokens or credentials in transit.

This example builds from the checkout; [new product integrations](sdk.md) consume Go/Maven packages. SDK release versions and host APK version codes are separate. No packaging/release test restarts this server or replaces an enrolled-device deployment.

## Build and start

Set the [JDK/SDK environment](development.md), then run from the repository root:

```sh
./scripts/prepare-local-server.sh 192.168.101.5 http
./scripts/run-local-server.sh 192.168.101.5 http
```

This builds a signed host and serves APIs/APKs at `http://192.168.101.5:8080`. Preparation reuses signing material and stores immutable APK copies, private environment/data files under `$HOME/.local/share/kylok-local-server/192.168.101.5`. Use fresh development data when schemas change. Do not replace a running enrolled-device deployment automatically.

Allow the listener through the computer firewall and use the same LAN without Wi-Fi client isolation.

## Web console and QR

The helper starts the console at `http://127.0.0.1:8081`. Enter the private token from `data/console-token` under the directory above; startup prints its path. Choose a scope, open **Enrollment**, select an interface or custom IP/domain, and click **Create enrollment QR**. Switching the destination redraws the same token; an optional separate APK origin is supported. QR material is private and expires after the chosen lifetime.

To listen on all IPv4 interfaces while retaining a concrete advertised URL:

```sh
KYLOK_AGENT_LISTEN=0.0.0.0:8080 ./scripts/run-local-server.sh 192.168.101.5 http
```

Only accepted local addresses appear as interface choices. Custom names must resolve on the tablet; HTTPS certificates must match the chosen name/IP. Redrawing a QR does not change an enrolled device's endpoint. Loopback choices work only on the server computer.

The console registers signed APKs, assigns updates (including host self-update), edits full policy replacements, observes operations/inventory/compliance and revokes identities. It uses public SDK methods with example-owned token authorization. The agent listener has no administrator routes. `KYLOK_UI_LISTEN=` disables the helper's console; direct `serve` enables it only with `-ui-listen`. Keep the default loopback bind unless remote administration is intentional.

The Android example shows **Overview**, **Applications**, **Policies**, **Operations** and **About**. Sync schedules work; local refresh reads a snapshot. OS confirmation and LAN permission are explicit user actions. Desired state or installer commit is not observed success.

## CLI enrollment

In another terminal:

```sh
export PATH="$ANDROID_HOME/build-tools/37.0.0:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
. "$HOME/.local/share/kylok-local-server/192.168.101.5/http.env"
./out/minimal-server enroll -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
  -data "$KYLOK_DATA" -scope poco-lab -ttl 2h -out "$KYLOK_LOCAL_DIR/enrollment"
```

Display the generated `.png` locally and scan from factory-reset enterprise setup (commonly six taps at the welcome screen). CLI QR files contain a one-device token and are not served by the agent/APK listener. The command refuses to overwrite output. Preserve the signed APK bytes after QR issuance. Follow the [physical checklist](provisioning.md).

## Manage applications

CLI management uses the host's authorized shell/data access. Common flags below refer to the environment loaded above. The optional console has its own authorization and listener.

```sh
./out/minimal-server list -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
  -data "$KYLOK_DATA" -scope poco-lab
./out/minimal-server register -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
  -data "$KYLOK_DATA" -scope poco-lab -application-apk /absolute/path/application.apk
```

Use the device ID from `list` and SHA-256 from `register`:

```sh
./out/minimal-server assign -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
  -data "$KYLOK_DATA" -scope poco-lab -device "$DEVICE_ID" -digest "$ARTIFACT_SHA256" \
  -request-key install-001 -timeout 24h
./out/minimal-server operations -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
  -data "$KYLOK_DATA" -scope poco-lab -device "$DEVICE_ID"
```

Assignment advances desired state; it is not installation success. Wait for observed results. A repeated request key returns the original assignment; a different artifact with that key conflicts. A fresh key supersedes older pending work for the package. `remove -digest ...` rejects desired/active references; physical content is retained safely. `revoke -device ...` rejects further device access without resetting Android.

## Manage policies

After assigning required APKs, save a [policy document](policies.md) to a local JSON file:

```sh
./out/minimal-server set-policy -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
  -data "$KYLOK_DATA" -scope poco-lab -device "$DEVICE_ID" \
  -request-key policy-001 -policy /absolute/path/policy.json
./out/minimal-server policy -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
  -data "$KYLOK_DATA" -scope poco-lab -device "$DEVICE_ID"
```

Inspect `desired`, `reported`, and per-rule `compliance`; submission is not enforcement. To release managed settings and exit kiosk, submit a file containing `{}` with a fresh request key. The device must receive that replacement; previously cached policy remains effective while offline. The CLI rejects unknown JSON fields and reads at most 64 KiB.

## HTTPS and custom CA

For a normal trusted certificate, pass an HTTPS `-public-url`, `-tls-cert` and `-tls-key` to `serve`. A DNS name must resolve on the device; an IP certificate needs that IP in Subject Alternative Name.

`prepare-local-server.sh IP_OR_HOSTNAME https` creates a private CA, matching leaf certificate and signed `customCA` host variant. It uses `KYLOK_CA_CERT` / `KYLOK_TRUST_HOST` during the build; private keys stay outside Git. The installed app trusts that CA only for the selected host. Setup Wizard cannot use app trust before downloading the APK.

Where needed, use a separate HTTP APK origin with the HTTPS API:

```sh
./out/minimal-server serve -public-url https://192.168.101.5:8443 -apk "$KYLOK_APK" \
  -data "$KYLOK_DATA" -listen 192.168.101.5:8443 \
  -tls-cert "$KYLOK_TLS_CERT" -tls-key "$KYLOK_TLS_KEY" \
  -bootstrap-url http://192.168.101.5:8080 -bootstrap-listen 192.168.101.5:8080
```

Pass the same bootstrap origin when issuing the QR. `Options.PublicURL` and optional `BootstrapURL` express these independent choices in the SDK. `Handler` serves APIs/APKs; `BootstrapHandler` serves only public bootstrap APKs. Post-enrollment app downloads always require device/operation authorization.
