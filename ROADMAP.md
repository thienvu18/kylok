# Kylok roadmap

## Current state

The current source includes host integration, PostgreSQL persistence, replayable changes, Android host options and managed app configuration. Go and Android builds must use the same source revision. Stable qualification is open: WAN-blocked provisioning, DPC replacement, kiosk/permission enforcement and several recovery cases still need device qualification.

Current physical evidence covers one POCO Pad X1 / Android 15 / API 35 run over LAN HTTP with WAN available. Online enrollment, signed app install/update, managed configuration, background sync, server/LAN/reboot recovery and selected policy checks passed. Android allowed app removal while `no_uninstall_apps` reported applied. WAN-blocked enrollment was unavailable on the router; kiosk, permissions, DPC replacement and other models remain untested. See the [device report](docs/validation/2026-10-27-poco-pad-x1.md) and [validation status](docs/compatibility.md).

## Next milestones

**N1–N3 are implemented with automated checks; N4 is implemented and its managed-app path passed on POCO. Physical `HostOptions` coverage remains open. N5–N7 are planned.** The [implementation plan](docs/implementation-plan.md) defines contracts, dependencies and acceptance tests. Device qualification runs alongside development. Current APIs are documented in [host integration](docs/host-integration.md) and [managed configuration](docs/managed-configuration.md).

| Milestone | Outcome | Exit condition |
| --- | --- | --- |
| N1 — Host integration contracts (implemented) | Scoped device/operation reads, consistent pagination/errors, enrollment correlation and documented host lifecycle. | A framework-free external host implements enroll → associate → assign → observe → revoke through public domain APIs, with unauthorized/cross-scope requests rejected. |
| N2 — Production persistence (implemented) | Optional PostgreSQL adapter, explicit pool/schema ownership and multi-instance concurrency guarantees; SQLite remains the default. | The same adapter conformance suite passes on both backends, plus PostgreSQL crash/restart and concurrent-instance tests. |
| N3 — Reliable host synchronization (implemented) | Scoped replayable change feed with transactional persistence, checkpoints and resnapshot semantics; optional callbacks remain best-effort. | A host projection recovers after missed notifications, restart and duplicate delivery without losing committed state or leaking scopes. |
| N4 — Android host and app configuration (software implemented; managed-app path physically checked on one device) | Defined early configuration lifecycle and typed managed app configuration through Android platform APIs. | A separately packaged host configures the SDK before work starts; an independent app observes configuration, updates and release without exposing secrets. POCO app configuration passed on current source; separately packaged `HostOptions` physical coverage remains open. |
| N5 — Fleet operations | Groups, bulk assignments, deployment windows and durable reboot/lock commands. | Bounded partial results, per-device idempotency, cancellation/deadlines and honest observation semantics survive retries and restarts. |
| N6 — Managed network lifecycle | Managed Wi-Fi and private certificate lifecycle with recovery safeguards. | Target devices retain or recover management connectivity during change, failure, rotation and policy release; existing unmanaged settings are preserved. |
| N7 — Stable release qualification | Supported device/firmware matrix, documented compatibility window, scale limits, remediation of observed enforcement gaps and complete release evidence. | [Stable release readiness](docs/release-readiness.md) passes with exact source/package/device evidence and published consumers; retest uninstall prevention after remediation. |

N1 precedes N2/N3; N4 builds on N1, and N5 uses N2/N3. N6 requires the policy/configuration contracts from N4. N7 consolidates evidence from all work; no dates or device-support promises are implied.

## Scope decisions

The SDK owns Android management semantics. Hosts own users, tenants, assets, authorization, licensing, business workflows and UI. Scope IDs and external references are opaque; examples must remain independent public-API consumers. Existing interfaces should be reused before adding new hooks; custom providers never weaken authentication, isolation, integrity or atomicity.

Scheduled deployment recovery uses a compatible higher-version APK or a documented platform-supported rollback path; arbitrary APK downgrades are not promised. Diagnostics, LAN push and advanced security policies follow demonstrated needs. Work Profile/COPE, OEM extensions, OTA and remote screen control require separate feasibility decisions.
