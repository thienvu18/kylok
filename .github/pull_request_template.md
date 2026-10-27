## Summary

<!--
Explain the problem solved, scope of the change, and observable outcome.
Reference any related issue(s) with "Fixes #..." or "Relates to #...".
-->

- **Issue**:
- **Scope**:

## Type of Change

- [ ] Go SDK (`go/`)
- [ ] Android DPC Library (`android/kylok-dpc/`)
- [ ] Protobuf Schema & Protocol (`proto/kylok/v1/`)
- [ ] Reference Applications (`examples/`)
- [ ] Documentation (`docs/`, `website/`)
- [ ] Testing & CI Tooling (`tests/`, `scripts/`, `.github/`)

## Architectural & Contract Compliance

<!-- Confirm that the change adheres to Kylok SDK boundaries and contracts -->

- [ ] **SDK Independence**: SDKs remain decoupled from examples, consumer business logic, or specific product frameworks.
- [ ] **State Separation**: Desired, observed, and compliance states are kept distinct. Checkpoints precede external side effects.
- [ ] **Guarantees**: Authentication, scope isolation, atomic token consumption, APK integrity checks, and operation idempotency are preserved across adapters.
- [ ] **Policy & Lifecycle**: Original policy settings are journaled before effects; only managed settings are restored.
- [ ] **Durable Persistence**: Checkpoint transitions are durable with explicit failure / uncertain states on unconfirmed effects.

## Documentation & Code Quality

- [ ] Every handwritten public method documents its purpose, contract, and a concrete `Usage:` example (or explains platform callback semantics).
- [ ] Internal/private methods have clear purpose comments (describing behavior and reasons, with no milestone labels like "Phase X" or "M2").
- [ ] Non-obvious protocol encodings, ordering constraints, limits, and magic constants are explained.
- [ ] Both English (`website/en/`) and Vietnamese (`website/vi/`) guides are updated if public user flows or APIs changed.

## Verification & Testing

<!--
Describe the automated and manual verification performed.
Distinguish between JVM/unit tests, Android emulator, and physical hardware evidence.
-->

- [ ] `make verify` passed locally (or targeted test suites: `make test-go`, `make test-android`, `make interop`).
- [ ] `python3 scripts/check-repository.py` passed with zero errors.
- [ ] `make docs-audit` passed with zero missing method comments or public usage examples.
- **Verification Evidence**:
  - JVM / Unit test results:
  - Emulator / Integration test results:
  - Physical device evidence (if applicable; record model, API level, firmware build, and transport):

## Security & Cleanliness Checklist

- [ ] **No Secrets**: No private signing keys, credentials, production/test QR tokens, or personal device identifiers are committed.
- [ ] **Licensing**: Original code is licensed under **LGPL-3.0-only**; applicable third-party notices and license headers are preserved.
