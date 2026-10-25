# Contributing to Kylok

Kylok is at the design stage. Architecture feedback, reproducible provisioning research, and clearly scoped implementation proposals are useful contributions. There is no runnable SDK or build/test command yet.

## Start with the design

Read the [README](README.md), [architecture](docs/architecture.md), [roadmap](ROADMAP.md), and [implementation plan](docs/implementation-plan.md). The original [design.md](design.md) remains the source draft. Propose consequential changes explicitly and update the affected documents together.

M0 provisioning feasibility is the first gate. Limit early implementation to the small Go/Android proof needed for that gate. SDKs must remain independent of examples, fDCMS, and other consumer applications.

## Propose a change

Open or link an [issue](https://github.com/thienvu18/kylok/issues) for substantial work. Include the problem, intended user-visible result, relevant milestone/work ID, proposed scope, Android/platform assumptions, and acceptance evidence. Check existing proposals before starting a large feature. Small documentation fixes can go directly to a pull request.

For device behavior, report the model, OEM, API level, firmware/build, SDK versions, provisioning mode, network conditions, reproduction steps, expected/actual result, and sanitized logs. State whether evidence came from static inspection, a local test, an emulator, or a physical device. Avoid posting credentials, enrollment tokens, QR secrets, signing material, or private device information.

## Implement and validate

Keep changes focused and use the public SDK boundaries. Introduce build/test commands with their working targets and document prerequisites. Preserve the default integration path; optional adapters and hooks cannot bypass authentication, scope isolation, token validation, ownership checks, artifact verification, or idempotency.

Use idiomatic Go and Kotlin, typed Protobuf contracts, and documented errors. Add meaningful tests for behavioral changes and run the checks appropriate to the change. Hardware provisioning cannot be established by a unit test or an emulator result. Record missing validation clearly.

For a pull request, explain the problem and resulting behavior, link the work item, list validation and remaining gaps, and update affected documentation. Do not mark milestone checkboxes complete until their exit gates have evidence. Publishing packages or releases is a separate maintainer action.

## Licensing and attribution

Original contributions are made under **LGPL-3.0-only**, the repository's declared license, unless a file explicitly carries applicable third-party terms. See [NOTICE](NOTICE), [LICENSE](LICENSE), and [COPYING](COPYING). Preserve copyright and license notices for third-party material. Google Test DPC is a reference implementation; source reuse requires its applicable attribution and license terms.

Use `SPDX-License-Identifier: LGPL-3.0-only` in source headers where supported, and include the appropriate license/notice material in published package archives. Do not add a different license or an "or later" grant without an explicit project decision.
