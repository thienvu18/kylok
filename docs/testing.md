# Testing strategy

This page describes verification for the current source. Pair Go and Android builds from the same source revision, and scope physical results to the exact build, device, firmware and network conditions in the [validation report](compatibility.md).

## Automated suites

| Command | Coverage |
| --- | --- |
| `make check` | Local Markdown links, baseline/readiness structure, unchanged design draft, versions/licenses and SDK dependency direction. |
| `make schema docs-audit` | Current typed contracts and handwritten method documentation. |
| `make test-postgres` | Disposable PostgreSQL 18.6 fixture: shared conformance, competing instances, rollback, connection termination, crash/restart and saved projection recovery. |
| `make test-go` | Go race, enrollment, storage, sync, policy, fleet fan-out/windows/cancellation, command report binding and example behavior. |
| `make test-android` | JVM agent/checkpoint/download/installer/policy, scheduled commit windows and one-shot command fault tests; standalone network fixture is skipped. |
| `make build-android interop` | Android build/lint and complete real Go↔Kotlin HTTP/HTTPS fixtures; OS effects are simulated. |
| `make test-host-update test-console` | Same-signer update APK metadata and authenticated console flows with real signed APKs. |
| `make test-release-gates` | Fast publication failure checks without building a distribution: fresh checkout, version/tag identity, stable readiness, immutable refs and upload retry behavior. |
| `make test-consumers test-release` | Build distributions, independent Go/minified Android hosts, exact artifact checks and tampering checks. |
| `make verify` | All source/package suites above, including repository checks. |
| `make docs` | Generated Go/Dokka reference, bilingual site and site links. |

`./scripts/check-generation.sh` independently checks reproducibility; workflow edits also need the pinned `actionlint`. PR CI splits suites into Go/schema, Android/transport and package/release jobs, with a required aggregate gate. Documentation builds are separately required. Deployment after merge builds/signs/publishes without rerunning tests.

## Registry package checks

To validate registry packages, use the matching source tag in a clean checkout, install the documented toolchain and prepare `out/release` from its `kylok-distribution.zip`. Then run:

```sh
KYLOK_REMOTE_SDK=all ./scripts/test-consumers.sh
```

This builds independent consumers from remotely resolved Go/Maven artifacts and checks their bytes against the distribution. It needs no registry credentials. Registry metadata availability, an independent consumer build and physical execution are distinct checks; record each separately in [current validation](compatibility.md).

## Feature contracts and future qualification

Regression tests cover current enrollment/scope, SQLite/PostgreSQL adapters, durable changes/projections, APK integrity/install checkpoints, policy/configuration, fleet fan-out, deadlines/cancellation, commands, managed network secrets, CA/TLS behavior and HTTP/HTTPS interoperability. These tests protect reusable SDK contracts and remain part of CI.

Future qualification adds:

- **N7:** HOME ownership, boot receiver behavior, launcher recovery after foreground-app and agent process death, offline restart and app replacement.
- **N8:** Technician authorization, serverless recovery actions, secure exit/return to kiosk and interrupted maintenance.
- **N9:** One-scan DPC bootstrap, one-time credential consumption, host correlation binding, replay and lost-response recovery.
- **N10:** Lock Task and launcher observation, Wi-Fi configuration/toggle restrictions, separate radio disable, restoration and safe management recovery.
- **N11:** Management-network loss/return, sync recovery, offline data retention, busy/defer decisions, update interruptions and safe resume.
- **N12:** Scoped broker identities/ACLs, message expiry and queue bounds, duplicate delivery, durable acknowledgement, app IPC authorization, Doze/process death and reconnect on target hardware.
- **N13:** Declared device/firmware/network matrix, measured fleet scale and stable release evidence.

A passing unit/transport test establishes its software contract only. It does not establish Android OEM enforcement, background delivery timing or supported hardware. Keep those results tied to exact device and network evidence.

## Physical qualification

For every supported candidate, follow the [device checklist](provisioning.md). Record source/package/APK identities, model/API/firmware, adapter and transport, network conditions, expected and observed behavior, and sanitized logs. Keep dated device records in [compatibility](compatibility.md); do not turn one device result into a general support claim. WAN-isolated provisioning, kiosk/launcher behavior, scheduled installation, device commands and managed-network effects require separate physical qualification.
