# Architecture and transport

Kylok is an SDK pair, not a mandatory business framework. The Go host owns authorization, listeners and lifecycle. The Android host owns application ID, signing, network trust and the UI. Examples consume public APIs; SDK code does not depend on examples or a business application.

| Component | Responsibility |
| --- | --- |
| Go facade | Enrollment/QR, registry, app catalog/assignments, operations, policies and optional events. |
| SQLite/filesystem defaults | Durable transactions, scoped identity, immutable verified APK bytes. |
| Android AAR | Provisioning callbacks, encrypted state, sync, download/install and policy reconciliation. |
| Shared Protobuf | Typed request/response, revision, report, error and compliance contracts. |

## LAN origins

HTTP/HTTPS and IP/domain origins are normal configurations. The configured scheme selects transport; there is no automatic downgrade. HTTPS verifies certificate/hostname trust. HTTP does not encrypt enrollment or bearer credentials in transit. The API and bootstrap APK may use independent origins.

The Android client uses HTTP/2 gRPC. HTTP requires h2c support on the Go listener. Setup Wizard trust before APK installation is separate from trust configured inside the installed host. A private CA bundled in an app cannot make an earlier Setup Wizard download trusted.

## Durable identity and effects

An enrollment token is single-use and scope-bound. Android persists an attempt/key before claiming it. The server atomically consumes the token, creates the identity and stores a key-bound encrypted recovery receipt. Matching retries recover the same identity; stolen replay bindings and revoked identities fail.

Sync persists requests before sending and binds each sequence to its exact body. Desired revision, reported revision and actual observations are distinct. Installer sessions and policy attempts are journaled before external effects. Observed package/version/signers and effective policy readback establish results; callbacks and setters alone do not.

Custom providers must preserve those invariants, concurrency/context behavior and lifecycle ownership. Optional status/event observers are read-only and best-effort. Real business/audit delivery needs a transactional host outbox, not notification hooks.
