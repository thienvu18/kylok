# Repository instructions

<!-- codebase-memory-mcp:start -->
For structural codebase exploration, use the installed `codebase-memory` skill.
<!-- codebase-memory-mcp:end -->

## Current state and documentation

Kylok is a design-stage, self-hosted Android device management SDK. This checkout currently contains documentation and licensing, with no SDK source, build system, CI, or runnable examples. Do not infer implementation from a proposed directory tree or API sketch.

- `design.md`: original Vietnamese v1 design draft; preserve it as the source reference.
- `README.md`: concise public introduction and current availability.
- `ROADMAP.md`: public milestones M0–M6 and thirteen v1 release gates.
- `docs/architecture.md`: intended component boundaries and invariants.
- `docs/implementation-plan.md`: work packages, dependencies, and required validation.
- `docs/github-publishing.md`: repository identity, description, topics, keywords, and future release tag conventions.
- `CONTRIBUTING.md`: contribution and evidence expectations.
- `NOTICE`, `LICENSE`, `COPYING`: LGPL-3.0-only declaration, LGPLv3 text, and incorporated GPLv3 text.

## Architecture constraints

- Deliver two SDKs: one main Go module in `go/` and one Android AAR in `android/kylok-dpc/`, with shared typed contracts in `proto/kylok/v1/`.
- Keep `minimal-server` and `minimal-dpc` as public-API-only example consumers. SDKs must not depend on examples, fDCMS, or a business framework.
- Default integration must work with validated required configuration and built-in adapters. Custom providers are optional; do not silently create temporary persistence.
- The Go host owns server/TLS lifecycle and business authorization. The Android host owns APK application ID and signing. The AAR does not grant Device Owner privileges by itself.
- Separate desired, observed, and compliance state. Persist identity, pending work, progress, and unsent reports. Confirm Android outcomes before reporting success.
- Preserve device authentication, enrollment validation, scope isolation, APK integrity/signing, ownership checks, and operation idempotency through all extension paths.

## Implementation and validation

Start with M0 and only the minimum M1 scaffolding needed to prove LAN QR provisioning on a physical device with WAN blocked. Do not generate the entire planned module tree before that gate.

API 33–37, compile/target API 37, and Fully Managed Device Owner are design targets. Verify toolchain/platform details before coding. Publish compatibility only with API/model/firmware evidence; distinguish automated, emulator, and physical-device results.

There are no build/test commands yet. Introduce commands with real targets and document them when implementation begins. For documentation changes, check links, headings, milestone consistency, and licensing references. Update related docs together when a contract or status changes. Preserve the user's unrelated changes; do not stage, commit, tag, push, or publish unless explicitly requested.

Use English for public documentation. Keep readiness claims explicit and avoid placeholder install commands, coordinates, badges, URLs, or support claims. Original project contributions use `LGPL-3.0-only`; preserve applicable third-party notices.
