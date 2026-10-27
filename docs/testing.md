# Testing strategy

This page describes verification for the current source. Pair Go and Android builds from the same source revision, and scope physical results to the exact build, device, firmware and network conditions in the [validation report](compatibility.md).

## Automated suites

| Command | Coverage |
| --- | --- |
| `make check` | Local Markdown links, baseline/readiness structure, unchanged design draft, versions/licenses and SDK dependency direction. |
| `make schema docs-audit` | Current typed contracts and handwritten method documentation. |
| `make test-postgres` | Disposable PostgreSQL 18.6 fixture: shared conformance, competing instances, rollback, connection termination, crash/restart and saved projection recovery. |
| `make test-go` | Go race, enrollment, storage, sync, policy and example behavior. |
| `make test-android` | JVM agent/checkpoint/download/installer/policy tests; standalone network fixture is skipped. |
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

## Integration checks and future suites

- **N1:** consumer-owned ports, domain/error mapping, scope/cursor rules, correlation retry and lifecycle/handler integration.
- **N2:** existing provider conformance plus real PostgreSQL multi-instance races, transaction rollback and restart/crash recovery. Simulated SQL tests are insufficient.
- **N3:** transaction-bound changes, cursor ordering/expiry, resnapshot consistency and replay after missed callbacks.
- **N4:** Android initialization races and typed configuration round trips. The independent app's physical receipt, visible application, restart persistence and release passed on one POCO Pad X1 using current development source; separate physical `HostOptions` coverage remains open.
- **N5:** partial bulk results, scheduling/cancellation/time changes and command uncertainty around effects/reboot.
- **N6:** physical network/trust change, rotation, fallback and preservation of unmanaged settings.
- **N7:** declared compatibility pairs, scale benchmarks and the complete stable candidate matrix.

N1–N4 software checks are implemented; selected N4 app behavior and policies have one-device evidence, while broader physical qualification remains manual. N5–N7 suites remain planned; do not add passing placeholders. Every adapter must preserve the same authentication, scope, atomicity, integrity and idempotency checks.

## Physical evidence

Maintainers execute [the device checklist](provisioning.md), beginning with WAN-blocked provisioning and install/update/recovery/policy checks. Record source/package/APK identities, model/API/firmware, transport, network conditions, expected/observed behavior and sanitized logs. Current POCO evidence covers online enrollment, signed app install/update, managed configuration, background sync, LAN/server/reboot recovery and selected policies; it also exposes failed uninstall prevention. WAN-isolated setup and several device controls remain blocked or untested. Unit tests, installer metadata or a successful policy setter cannot close a physical gate. See the [device report](validation/2026-10-27-poco-pad-x1.md).
