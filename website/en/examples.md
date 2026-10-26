# Example applications

## Start the example service

```sh
./scripts/prepare-local-server.sh 192.168.101.5 http
./scripts/run-local-server.sh 192.168.101.5 http
```

The example serves the agent API and APK from `http://192.168.101.5:8080` and opens a separate management console at `http://127.0.0.1:8081`. The console token is saved in the configured data directory; the startup log shows where to find it. Enter the token and choose a management scope to begin. HTTP is useful for a trusted local test but does not encrypt traffic. The console is an example host interface; your product must provide its own authentication and authorization.

To let devices connect through multiple IPv4 interfaces, run the helper with `KYLOK_AGENT_LISTEN=0.0.0.0:8080` or pass `-listen 0.0.0.0:8080` directly. Keep `-public-url` set to an address those devices can reach. The console shows the addresses accepted by the server.

## Enroll a device and update its QR address

1. Open **Enrollment**, choose a network interface or enter an IP/domain, set the token lifetime and click **Create enrollment QR**.
2. Choose another interface/domain to update the QR while keeping the same single-use token. You can set a separate APK address if you host the bootstrap app elsewhere.
3. On a factory-reset tablet, open Android enterprise QR setup, join the LAN and scan. For HTTPS, the address must match a certificate trusted by Setup Wizard before the APK download.
4. Confirm the authenticated device appears in the console. Treat the QR as a secret and share it only with the device being enrolled.

Changing the QR address affects future enrollments; devices already enrolled continue using their saved server address. If the server address remains reachable, previously enrolled devices can continue downloading assigned APKs.

## Explore app delivery and policies

- **Overview:** select a device; inspect model, owner report, inventory and desired/reported revisions; revoke access deliberately.
- **Applications:** upload a signed APK, inspect verified metadata, assign it with a request key/deadline, and remove only unreferenced entries. A higher-version DPC with the same host identity/signing uses this path for self-update.
- **Policies & kiosk:** choose a preset or edit the full replacement document. Required apps must already be assigned. An empty document releases controls managed by Kylok.
- **Operations:** track downloads, install progress, requests for user action, errors and confirmed results. Sending an assignment or starting installation does not mean the app is installed.

The console token demonstrates one way a host can protect its management interface. In your product, decide who may enroll devices, assign applications or change policies, and enforce those permissions in your host application.

## Android status dashboard

Open the example app after provisioning. **Overview** shows the device identity and connection status. **Applications** distinguishes requested app versions from installed versions. **Policies** shows the result of each rule. **Operations** shows installation progress and offers Android's confirmation action when needed. **About** shows app, SDK and device details. **Sync now** schedules a sync; **Refresh view** reads the latest saved status. Background work does not depend on the app screen being open.
