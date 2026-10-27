# Agent protocol

The current contract is [agent.proto](../proto/kylok/v1/agent.proto), shared by Go and Kotlin. Build both SDKs from the same source revision. HTTP/HTTPS and IP/domain origins are supported; the configured scheme selects transport without automatic downgrade. HTTP leaves tokens and credentials unencrypted in transit.

Protocol version is 1. [Release manifests](releases.md) record the exact Go/Android package pair, source identity and automated/physical evidence scope. Host event/status observers do not participate in authentication, protocol acknowledgements or compliance decisions.

## Enrollment

A host-authorized scope creates a 32-byte random token, valid for 1 minute–24 hours. The QR contains the token, API origin, immutable APK URL and actual SHA-256. Only token verifiers are stored server-side. `QRWithOrigins` can redraw that token for another valid API/bootstrap origin; scope, expiry and APK binding remain the same. The resulting API origin is persisted by the new device, not changed remotely by later QR redraws.

Before sending, Android persists an attempt UUID and creates a Keystore P-256 key. Enrollment sends its public SPKI DER, the attempt, token and inventory. One transaction consumes the token, inserts the device/credential verifier and saves an encrypted response. Matching attempt ID **and** canonical key hash recover that exact response for 24 hours after consumption, including after the original token expiry. Another binding, expired recovery or revoked device fails.

The credential envelope uses ephemeral P-256 ECDH, HKDF-SHA256 (empty salt, UTF-8 info `kylok/enrollment/v1`, 32-byte key) and AES-256-GCM (12-byte nonce, 128-bit tag). Authenticated data is UTF-8 `kylok/v1\n<device_id>\n<scope_id>\n<attempt_id>` with actual newline separators. Only the original private key can decrypt the 43-character credential. Android commits it in encrypted Room state before authenticated sync and removes the token. A stolen unused token can still win the first claim; keep QR material private.

## Sync and operations

Sync sends a persisted monotonically increasing sequence, inventory/capabilities, received desired revision and unsent reports. Device bearer authentication applies to every request; scope is derived from identity. Revoked devices fail even when replaying an older request.

The server atomically processes the request and saves its complete snapshot. An identical sequence/body retry returns the same snapshot; conflicting payloads or older sequences fail. Android commits only the matching response, then advances its checkpoint and removes acknowledged reports. Reports created while a request is in flight remain pending.

Application assignment is idempotent per device/key. A different artifact under the same key conflicts; a fresh assignment advances desired revision and supersedes older pending work for that package. Up to eight operations are delivered per sync, with 30-minute renewable leases, at most eight claims, and a host-selected deadline (1 minute–7 days). New claims replace expired lease IDs. Reports carry a unique ID, operation/lease and increasing report sequence; duplicate/conflicting/stale cases have explicit receipts.

Desired state, observed inventory and compliance are distinct. Installer commit and `WAITING_USER` are not success. `SUCCEEDED` must match the desired package, version and signer set. Compliance compares current installed inventory and reports `APPLIED`, `PENDING`, `FAILED` or `UNSUPPORTED`.

## Artifact and agent recovery

Public `/bootstrap/<digest>.apk` serves only registered bootstrap content. Managed `/artifacts/<operation>/<digest>.apk` requires bearer credential, `X-Kylok-Device-ID` and `X-Kylok-Lease-ID`, matched to the assigned device/scope/active lease. Digests, lengths and immutable metadata are checked; range requests allow resumed downloads without placing credentials in URLs.

The agent persists request/report outboxes, desired revisions, download progress and session IDs before effects. Managed download paths resolve relative to the enrolled API origin, allowing QR-selected interfaces/domains. Android checks scheme, authority and exact path before adding credentials, rejects redirects/content changes, fsyncs progress and verifies the final digest. Before installation, Android checks package/version/signers, SDK/ABI/features, compatible signing and no downgrade. Callback success is verified against installed state; missing callbacks are reconciled through session/package queries.

WorkManager retries transport failures with exponential backoff starting at 30 seconds, immediate work with bounded initial jitter, and periodic recovery every 15 minutes. Boot/package replacement schedule recovery. Scheduling does not require validated internet. The host signing key, Device Owner and protected identity must survive DPC self-update; physical proof remains a manual check.

## Policy exchange

`SetPolicy` binds a normalized full-replacement document to scope/device/key and advances the shared desired revision. Required packages must have existing scoped APK assignments. Sync delivers `DesiredPolicy`; Android commits it before reconciliation. `PolicyReport` is frozen with the request and carries policy revision, complete snapshot revision and per-rule observations. Stale revisions cannot establish current compliance; APK assignments invalidate earlier snapshot observations. Unknown/duplicate rule IDs and invalid/bounded payloads are rejected without partial commit.

Canonical rule IDs are `required:<package>`, `camera`, `screen_capture`, `maximum_time_to_lock`, `restriction:<name>`, `permission:<package>:<permission>`, `lock_task`, and `cleanup`. Applied values must match expectations: lowercase booleans, decimal milliseconds, permission state names, `package@version:sorted,signers`, compact sorted `{"packages":[],"kiosk_package":""}` JSON, or `complete` for cleanup. Required-app compliance also checks current inventory. Setter/launch return values are insufficient. [Policy semantics](policies.md) defines release, prerequisites and observation behavior.

## Limits and errors

- Single/universal APKs up to 64 MiB; split execution is unsupported.
- Up to 64 desired/required apps, 64 permission rules, 201 policy results, 128 installed inventory entries, 64 operation reports and 1 MiB per sync. Policy observed values are at most 20000 bytes.
- Scope/management keys: 1–128 ASCII letters/digits/underscore/dot/hyphen, beginning with a letter/digit.
- `UNAVAILABLE`/deadline errors retry; invalid inputs, consumed/expired tokens, conflicting/stale sync and revoked credentials require diagnosis. `ErrorDetail` carries reason/retryability without exposing backend errors or secrets.

Revocation rejects sync, downloads and enrollment recovery; it does not reset Android. No in-place credential rotation is implemented. New identity requires authorized fresh provisioning. Inventory/ownership are authenticated self-reports, not hardware attestation.

Managed app configuration uses typed protobuf oneofs for Android restriction bundles. Configuration rule observations carry a normalized `sha256:` digest, never raw bundle content. Required package/version/signer checks gate enforcement. See [configuration semantics](managed-configuration.md) for value/size limits and application acknowledgement boundaries. Enrollment correlation and management cursors/change records are host-only APIs and never device authentication credentials.
