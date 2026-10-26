# Implementation plan

The [roadmap](../ROADMAP.md) defines milestone outcomes. M2–M6 software/release tooling is implemented; physical checks and remote publication remain separate. No backward compatibility is promised for development APIs, schemas or local databases.

| Package | Deliverable | Automated evidence |
| --- | --- | --- |
| E1: construction/storage | Validated options, durable defaults, custom-provider ownership and transactional contracts. | Constructor failures, corrupt/incomplete schema, concurrent transactions and adapter conformance. |
| E2: provisioning | Signed metadata, immutable bootstrap content, QR with actual digest/component and selected transport. | Real APK verification, metadata rejection, checksum and HTTP/HTTPS downloads. |
| E3: enrollment | Single-use scoped token, protected identity, encrypted retry receipt, registry and revocation. | Token expiry/replay, competing attempts, lost response/restart, wrong credential and scope isolation. |
| R1: state/sync | Desired/reported revisions, capabilities, inventory, compliance, leases, checkpoints and report acknowledgements. | Duplicate/conflicting sync, stale leases/reports, revision supersession and false-success rejection. |
| R2: recovery | Persist requests before sending; bounded retry, periodic/event recovery, boot/package replacement. | Fault injection before/after commit, request/report replay, process-state reload, revoked reconnect. |
| A1: artifacts | Verified registration, immutable storage, scoped authenticated range downloads and retention. | Cross-device/scope rejection, interruption/resume, content mismatch and referenced-artifact removal. |
| A2: installer | Verify staged APK; persist session before effects; handle callbacks and observe installed outcome. | Pending user action, failure/missing callbacks, duplicate delivery, checkpoint interruption and no duplicate commit. |
| A3: self-update | Preserve host signing/ownership/identity; recover after package replacement and report observed result. | Same engine/checkpoint path, version/signer checks and simulated replacement recovery. |
| P1: policy state | Scoped idempotent full replacement, shared revisions, required assignments and per-rule compliance. | Scope/conflict/concurrency, stale snapshots, report replay/rollback, revocation and adapter conformance. |
| P2: reconciliation | Durable baselines/attempts, capability checks, effective readback, release and cached offline repair. | Unsupported/denied settings, missing readback, write failure/process interruption, partial compliance and baseline restoration. |
| P3: kiosk | Allowlist/designated app after verified required apps, callback/locked-state observation, release and drift suspension. | Missing/version/signer prerequisites, asynchronous entry, duplicate launch suppression and full Go↔Kotlin policy/release exchange. OS effects are simulated. |
| S1: public integration | Documented provider/lifecycle contracts, SDK versions, bounded Go events and typed Android snapshots/listeners. | Failed/panicking/cancelled hooks, sanitized snapshots, independent host/provider conformance. |
| S2: distributions | Go proxy module, AAR/Maven metadata/sources, license/source/dependency archives and checksums. | Archive/hash/metadata checks and package-only Go/minified Android consumers. |
| S3: release automation | Protected-main PR checks, unique per-merge versions, signed Central publication, immutable SDK tags and resumable GitHub prereleases. | Invalid/dirty/wrong-tag/stable-v1 gates, artifact tampering and workflow lint. Hosted publication is not claimed. |
| S4: documentation/examples | Generated Go/Dokka API references, English/Vietnamese site, authenticated web console and public-API Android dashboard. | Method documentation audit, site links/language pairs, real-APK console flow and Android build/lint. Hosted Pages and physical UI behavior remain separate. |

## Verification and handoff

Run `make verify`, deterministic generation, repository checks and a clean source build. HTTP and HTTPS interoperability must exercise real Go/SQLite and Kotlin wire/download/checkpoint/policy code. JVM installer/policy tests use simulated platform effects; they do not prove OS enforcement.

The [physical checklist](provisioning.md) retains WAN-blocked provisioning, installation/update, recovery, DPC self-update and policy/kiosk enforcement. Record exact model/firmware and results in [compatibility](compatibility.md). The existing POCO result establishes initial LAN HTTP enrollment only.

[Publication](releases.md) runs automatically after a protected-main merge and does not rerun tests. Record hosted package/consumer evidence separately; stable v1 still requires the remaining physical gates.
