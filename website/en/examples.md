# Example applications

## Start a signed local deployment

```sh
./scripts/prepare-local-server.sh 192.168.101.5 http
./scripts/run-local-server.sh 192.168.101.5 http
```

The agent/APK origin is `http://192.168.101.5:8080`. The helper enables a separate administrator console at `http://127.0.0.1:8081`; direct `minimal-server serve` enables it with `-ui-listen 127.0.0.1:8081`. Its private `console-token` file is under the configured data directory; the startup log prints the location. Open the console, enter that token and choose a management scope. `KYLOK_UI_LISTEN=` disables the helper's console. No administrator route is added to the SDK's public agent listener.

To accept multiple IPv4 interfaces, run the helper with `KYLOK_AGENT_LISTEN=0.0.0.0:8080` or pass `-listen 0.0.0.0:8080` directly, retaining a concrete device-reachable `-public-url`. The console lists addresses actually accepted by that bind.

## Enroll and switch the QR address

1. Open **Enrollment**, select an interface or enter a custom IP/domain, choose the token lifetime and click **Create enrollment QR**.
2. Select another interface/domain to redraw that same single-use token. An optional separate APK origin supports independent bootstrap hosting.
3. On a factory-reset tablet, open Android enterprise QR setup, join the LAN and scan. HTTPS names/IPs must match trusted certificates; app trust cannot change Setup Wizard trust before APK download.
4. Inspect the authenticated device record. QR files contain secrets; use the private download only for provisioning.

Changing a QR affects new provisioning, not the endpoint of an already enrolled device. Managed APK paths resolve against the device's enrolled origin, so an interface/domain alias stays usable after enrollment.

## Demonstrate applications and policies

- **Overview:** select a device; inspect model, owner report, inventory and desired/reported revisions; revoke access deliberately.
- **Applications:** upload a signed APK, inspect verified metadata, assign it with a request key/deadline, and remove only unreferenced entries. A higher-version DPC with the same host identity/signing uses this path for self-update.
- **Policies & kiosk:** choose a preset or edit the full replacement document. Required apps must already be assigned. An empty document releases controls managed by Kylok.
- **Operations:** observe downloads, installer/user-action states, errors and confirmed package outcomes. Assignment or installer commit alone is not success.

The console uses explicit bearer authorization, same-origin browser actions and scoped public SDK methods. Its assets are embedded and work without an internet CDN. Token authorization is demonstration host logic; real products own business authorization.

## Android dashboard

Open the example app after provisioning. **Overview** shows identity, endpoint, permission and checkpoints. **Applications** separates desired and observed versions. **Policies** reports each rule. **Operations** shows durable progress and an OS-confirmation button when needed. **About** shows host/SDK/device identity. **Sync now** schedules network work; **Refresh view** reads current local state. The UI is not required for background initialization.
