# Current validation

Kylok uses protocol version 1. Go and Android SDKs must be built from the same source revision. The source targets Android API 33–37 (min SDK 33, compile/target SDK 37); this is a build range, not a device support list. See the [testing strategy](testing.md) for automated coverage and [stable readiness](release-readiness.md) for release gates.

## Software verification coverage

The repository verification suite covers:

| Area | Coverage |
| --- | --- |
| Go storage and transport | Enrollment, recovery, revocation, scoped state, concurrent assignments, replay, retention and integrity. |
| Android agent | Durable requests/reports/checkpoints, downloads, installer outcomes and policy recovery. |
| HTTP/HTTPS interoperability | Kotlin/Java Lite and OkHttp with Go/SQLite for enrollment, assignment, authenticated downloads, reports and release. |
| Android builds | AAR and examples, API 37 compilation, lint, signed update fixtures and private-CA configuration. |
| Providers and host integration | SQLite/PostgreSQL conformance, concurrent instances, transaction recovery, independent Go/Maven consumers and host APIs. |
| Distribution and documentation | Archives, licenses, checksums, release gates, authenticated examples, API documentation and bilingual site links. |

Automated Android tests simulate platform effects. They do not establish OEM behavior for install prompts, policy enforcement, recovery or kiosk mode.

## Physical qualification

| Device / firmware | Build and network | Result |
| --- | --- | --- |
| POCO Pad X1; Xiaomi `25099RP08G`, Android 15 / API 35, HyperOS `OS2.0.205.0.VOZMIXM` | Source commit `d9c375993eb0e4be78f1fa2868ca497ce345bb14`; LAN HTTP at `192.168.101.5`; Internet reachable | Online QR enrollment, signed app install/update, idempotency/conflict and downgrade checks, managed configuration, background sync, server/LAN/reboot recovery, and selected policy checks passed. Uninstall prevention failed despite `APPLIED` readback. See the [device report](validation/2026-10-27-poco-pad-x1.md). |

WAN-isolated enrollment was unavailable because the router could not block WAN while keeping LAN access. `HostOptions` hardware coverage, kiosk, runtime permissions, DPC replacement, process force-stop recovery, secure lock timeout, Add user enforcement, alternate origins, revocation and other device models remain open or untested. The sample app's configuration survived an offline reboot; this does not establish behavior for every policy. See the [physical checklist](provisioning.md) for remaining work. Keep tokens, credentials, private keys and personal device data outside Git.
