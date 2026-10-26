# Repository instructions

<!-- codebase-memory-mcp:start -->
For structural codebase exploration, use the installed `codebase-memory` skill.
<!-- codebase-memory-mcp:end -->

## Scope and current state

Kylok is an experimental Go/Android device management SDK. Work through M6 covers enrollment, durable sync, APK lifecycle, policies/kiosk, package consumers and release tooling. Remote publication and physical release gates remain separate; current evidence is in `docs/compatibility.md`. Keep claims limited to recorded evidence. Do not expand feature scope without a task.

Development APIs, schemas, and local data formats may change without backward compatibility. Do not add legacy adapters or migrations solely for earlier development builds. Preserve `design.md` as the original draft; current decisions live in `docs/architecture.md` and `docs/enrollment-protocol.md`.

## Boundaries

- One Go module in `go/`, one Android AAR in `android/kylok-dpc/`, shared schema in `proto/kylok/v1/`.
- Examples consume public APIs only. SDKs must not depend on examples, fDCMS, or a business framework.
- HTTP/HTTPS and IP/domain origins are normal configurations. The URL selects transport; HTTPS verifies certificates/hostnames without automatic downgrade.
- Hosts own business authorization, server lifecycle, application ID and signing. The AAR cannot grant itself Device Owner.
- Preserve authentication, scope isolation, atomic token consumption, retry binding, APK integrity, ownership checks, and operation idempotency across adapters.
- Keep desired, observed and compliance state distinct. Persist checkpoints before external effects; report success only after observation.
- Journal original policy settings before effects; restore only managed settings. Kiosk requires verified app prerequisites and observed managed locking.
- Default persistence is durable; no temporary fallback. Custom providers remain host-owned.
- Optional event/status observers are read-only and best-effort; their failures cannot change authentication or committed operations.

## Work and verification

Kylok is an open-source library: every handwritten public method must document its purpose, contract and a concrete usage example (or explain how Android invokes a platform callback). Every private/internal method must have a clear purpose comment. Explain non-obvious statements, protocol encodings, ordering, limits and other magic values; prefer named constants when they clarify the rule. Comments describe behavior and reasons, never project phases or milestone labels such as “Phase 1” or “M2”. Do not edit generated/third-party code to satisfy these rules. Keep generated API documentation and the English/Vietnamese documentation site current when APIs change.

Use `make verify` and `python3 scripts/check-repository.py`; prerequisites are in `docs/development.md`. This includes independent Go/Maven consumers and release checks. Keep `go/VERSION`, `android/VERSION` and package docs aligned; development pairs use one source revision. Keep docs concise, in English, and aligned with code. Original code uses LGPL-3.0-only; preserve third-party notices and mirrored Go license texts.

Preserve unrelated changes and existing staging. Do not stage, commit, tag, push or publish unless explicitly requested.
