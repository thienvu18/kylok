# Validation and compatibility

## Current automated checks

**Passed locally (2026-10-26):** source-only `make verify`, deterministic generation, repository checks and generated documentation. Both HTTP/HTTPS fixtures executed all 51 JVM tests without skips/failures; Go race checks passed. Fresh Go/minified Android package consumers, exact package contents, twelve release failure/tampering/publication tests and workflow lint passed. Chrome checks covered real console QR/redraw/enrollment, signed APK registration/assignment, policy submission, scope switching, bilingual navigation/search/API links and mobile layouts. Installed toolchains/global dependency caches were reused. No hosted CI, remote publication or new physical support is claimed.

| Check | What it establishes |
| --- | --- |
| Go race/storage/transport tests | Enrollment/recovery, transactional assignment/sync, concurrency, leases, replay/supersession/deadlines, scope authorization, retention and integrity. |
| Signed APK verification | Real built APK signature/manifest validation and digest agreement; engine fixtures otherwise mock external Android tools. |
| JVM agent tests | Durable request/report/checkpoint behavior, lost responses, downloads, simulated installer/replacement failures and policy baseline/attempt/release fault tests. |
| Policies | Scoped/idempotent replacement, concurrent requests, stale observations, false-success rejection, rollback/revocation and reusable adapter conformance. |
| HTTP/HTTPS interoperability | Real Kotlin/Java Lite/OkHttp → Connect-Go/SQLite, CLI app/policy assignment, authenticated APK download, observed completion and policy release. Installer/policy effects are simulated. |
| Android builds/lint | API 37 SDK/example compilation and static platform checks, not physical install behavior. |
| Signed host-update fixture | Builds versions 1 and 2 with the same package/receiver/signer and checks custom-CA variant isolation; no OS install is performed. |
| Generation/schema | Pinned, repeatable code generation and current schema lint/compilation. |
| Public hooks | Committed Go event identifiers, error/panic/cancellation isolation, sanitized Android status snapshots and listener lifecycle. No durable event delivery claim. |
| Example interfaces | Authenticated console API tests with a real APK plus headless Chrome flows; Android dashboard build/lint and JVM status mapping, without device rendering/enforcement evidence. |
| Documentation | Purpose/usage audit, generated Go/Dokka references, translated page pairs, local links and browser navigation/search/layout checks. Pages deployment remains pending. |
| Independent package consumers | New Android host resolves Maven metadata, verifies exact AAR bytes and builds debug/minified release APKs with a distinct application ID. New Go host resolves the module without `replace`/workspace, uses that new host's signed APK and executes public APIs/provider conformance. No Android device execution. |
| Release distributions | Module/Maven/source/license/KDoc archives, dependency metadata and checksums; dirty/wrong-tag/version/v1 gates, artifact tampering, immutable refs and lost-upload-response recovery. A real PGP bundle was signed/verified locally and its public key retrieved from the keyserver. Remote publication remains pending. |

JVM crypto providers do not establish Android Keystore/Room/WorkManager reboot behavior. Simulator success is not silent-install, DPC self-update or policy/kiosk enforcement evidence. Physical tests below remain maintainer-owned; these checks do not deploy to a device.

| Pair / protocol | Automated integration | Physical compatibility |
| --- | --- | --- |
| Go and Android `0.1.0-dev`, protocol 1, same source revision | Local module/Maven consumers and HTTP/HTTPS fixtures; min SDK 33, compile/target 37. | Current pair untested on hardware. API 33–37 is a source target range, not a supported-device matrix. |
| Original M2 development pair | Historical enrollment only. | One POCO/API 35 online LAN HTTP run, below; no compatibility with newer development builds promised. |

## Physical results

| Device / firmware | Environment | Result |
| --- | --- | --- |
| POCO Pad X1; model `Xiaomi 25099RP08G`, API 35, HyperOS `OS2.0.205.0.VOZMIXM` | LAN HTTP, **WAN available**, original M2 development APK | QR setup and one authenticated `MANAGED` registry entry, Device Owner reported. [Recorded evidence](validation/2026-10-25-poco-enrollment.md). |
| Other models, firmware and API 33–37 combinations | Untested | No compatibility claim. |

That historical M2 result does not establish support for later development builds. Complete the [manual checklist](provisioning.md) for WAN-blocked provisioning, real installs/updates, recovery, DPC replacement and policy/kiosk enforcement. Record source/APK/signing identity, actual model/API/build, transport/network conditions and sanitized results. Keep tokens, private keys and raw personal device data outside Git.
