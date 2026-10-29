# Managed networks and private CAs

Current network policy optionally manages saved Wi-Fi profiles and private CA certificates through Device Owner APIs. These policies are independent from the host's network deployment. Physical network effects and recovery qualification remain open. Bootstrap APK trust stays separate from installed-app trust.

Wi-Fi lockdown is planned as two independent policy modes: restrict user configuration/toggles and disable the radio. A host can select either mode alone or both in the same policy. Android exposes distinct controls for each behavior, so the DPC must report and observe them separately. Before radio disable, the agent must preserve a safe way to restore management access if connectivity is lost.

## Host contract

Set `Options.NetworkSecrets` to a durable host-owned `NetworkSecretProvider` for protected Wi-Fi. `ReadNetworkSecret(ctx, scope, reference)` returns caller-owned bytes from an immutable scoped version. A reference must resolve to the same bytes across retries and restarts. Rotate with a new reference and request key. Retain versions while desired state or cached sync/recovery checkpoints can reference them; the SDK does not garbage-collect the host vault.

```go
sdk, err := kylok.New(ctx, kylok.Options{
    PublicURL: origin, Bootstrap: apk, DataDir: dataDir, NetworkSecrets: vault,
})
// Handle err before assigning a policy.
desired, err := sdk.SetNetworkPolicy(ctx, scope, deviceID, "office-v2", kylok.NetworkPolicy{
    PrimaryID: "office",
    Profiles: []kylok.WifiProfile{{
        ID: "office", SSID: "Office", Security: "WPA2_PSK", SecretRef: "office-key-v2",
    }},
})
```

`SetNetworkPolicy` replaces the complete document. An identical key/document returns its original receipt; conflicting reuse fails. `GetNetworkPolicy` and `GetDeviceState` separate desired, reported and current compliance. `ReadProjection` includes the same reference-only network status, and mutations produce transactional `device_changed` records. Secrets are resolved only after sync authentication; server checkpoints, host reads, events and changes never contain delivered passwords. Provider failures fail closed with sanitized diagnostics. Android persists delivered credentials in encrypted Room/Keystore checkpoints. HTTP does not encrypt credentials on the network.

| Field | Contract |
| --- | --- |
| Profiles / CAs | At most eight of each; profile/CA IDs start with an ASCII letter and contain letters, digits, `_`, `.`, `-`, at most 128 characters. |
| SSID | Literal UTF-8, 1–32 bytes; unique per document. Unmanaged collisions are rejected. |
| `OPEN` | No secret reference/passphrase. |
| `WPA2_PSK` | 8–63 printable ASCII bytes or a 64-digit hexadecimal PSK. |
| `WPA3_SAE` | 1–63 valid UTF-8 bytes without ASCII control characters; hardware support is checked. |
| CA | Complete DER CA certificate, at most 16 KiB, with CA constraints and current validity; optional Key Usage must permit certificate signing; no private key. |
| Primary | One configured profile must be selected when profiles exist. Other profiles are configured; only the primary's management connectivity is confirmed. |
| Release | An empty document releases only managed resources after recovery connectivity is verified. |

Network lifecycle currently covers personal/open Wi-Fi and CA trust. Enterprise EAP/client-key management, VPN and proxy management require separate contracts. Hosts grant **precise location** and **Nearby Wi-Fi devices** runtime permissions before network effects; the AAR declares the required permissions. Missing permission or an unobservable recovery route fails before mutation. Hosts must not concurrently create/change the managed SSIDs under the same Android application UID.

## Connectivity and ownership

1. Capture an independent saved connected recovery network before the first effect. It stays outside managed profile ownership and cannot share a desired SSID. Verify authenticated management access over it before each migration. Keep this route configured and its access point reachable throughout changes.
2. Journal the prior managed credentials, expected Android network IDs and exact CA baseline ownership before effects. Creator/SSID/security checks reject unmanaged collisions and ownership changes. Interrupted creation inspects only its expected caller-owned resource.
3. Stage new profiles and CAs on the recovery route. Credential rotation for the same managed SSID retains its prior encrypted profile for rollback.
4. Select the primary and authenticate a fresh nonce-bound `/network-probe` over that exact Android `Network`. Check the enrolled IP/domain origin using exactly the proposed remaining HTTPS trust, with certificate/hostname validation and no redirects or downgrade. WAN access, association and setter returns cannot establish success.
5. Persist verified replacement, then release omitted managed resources. Preserve every unmanaged network and pre-existing CA. Network removal rechecks ownership and refuses the active route.

The agent binds DNS, gRPC sockets and authenticated APK downloads to the selected Wi-Fi route when management networking is active. TLS trust composes the host's default base anchors with the desired private CAs and is rebuilt per channel/download without changing global trust or hostname verification. Configure independent bootstrap anchors in the host's base network security configuration; additional domain-specific pinning is not composed into managed TLS and requires a separate host integration contract. The agent separately records existing base trust and user-store ownership, so releasing a user-store copy preserves an anchor supplied by the APK. Device CA installation affects Android's user trust store; other apps choose whether to trust it through their own network security configuration.

On failure before verification, restore prior managed credentials and select recovery before cleanup. If recovery cannot be verified, keep the journal/resources and report `PENDING`; cleanup waits. After a verified checkpoint, restart rechecks connectivity and completes cleanup. An unverified interrupted migration reports failure after rollback. A failed revision is not repeatedly switched/replayed; submit a new request key for an explicit retry. Loss of a committed primary route also attempts authenticated recovery before normal sync while retaining its configuration.

CA rotation needs an overlap: deliver both roots while the current server chain remains trusted, rotate the server chain, then submit a document retaining the replacement root. Release is refused if remaining trust cannot authenticate management. Expiry never authorizes ignoring TLS validation. Removed owned-root fingerprints stay excluded from management trust despite Android cache delays; at most 256 distinct owned active/retired fingerprints are retained, and further new roots fail before effects when this limit is reached.

`APPLIED` records the latest received observation for that desired revision; hosts use device last-seen time to assess freshness. `NetworkStatus` reports `APPLIED` only when the primary/recovery probe and managed-resource cleanup are confirmed. A policy can remain desired while a failed migration has recovered to the baseline. Android's `AgentStatus.network` exposes sanitized revision/state/reason, profile ID and verification flags. The example dashboard offers the optional permission prompt; the [local CLI](local-server.md) submits/reads policy through public APIs.

Platform references: [WifiManager Device Owner APIs](https://developer.android.com/reference/android/net/wifi/WifiManager), [DevicePolicyManager certificate APIs](https://developer.android.com/reference/android/app/admin/DevicePolicyManager). See [testing](testing.md) and [physical checks](physical-device-tests.md) for evidence boundaries.
