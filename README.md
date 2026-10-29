# Kylok — Android device management for your product

[![Status: experimental](https://img.shields.io/badge/status-experimental-orange)](ROADMAP.md)
[![License: LGPL-3.0-only](https://img.shields.io/badge/license-LGPL--3.0--only-blue)](LICENSE)

[English](README.md) · [Tiếng Việt](README.vi.md) · [Documentation](https://thienvu18.github.io/kylok/) · [Releases](https://github.com/thienvu18/kylok/releases)

Add Android device management to your own product with Kylok, without building enrollment, app delivery and policy workflows from scratch. Kylok pairs a **Go server SDK** with a **Kotlin Android Device Policy Controller (DPC) library**, running on infrastructure you control. Your team keeps ownership of authorization, server lifecycle, application identity and signing.

> [!IMPORTANT]
> Kylok is actively developed. Go and Android packages are available through the [Go module proxy](https://proxy.golang.org/github.com/thienvu18/kylok/go) and [Maven Central](https://central.sonatype.com/artifact/io.github.thienvu18.kylok/kylok-dpc). Build paired SDKs from the same source revision. Stable release qualification remains open; see [current validation](docs/compatibility.md).

## Current capabilities

- Enroll Device Owner agents with one-time QR codes for an IP or domain origin over HTTP or HTTPS.
- Deliver signed APKs with resumable downloads, integrity checks, durable install tracking and silent `PackageInstaller` execution.
- Enforce device policies directly at the OS level (`DevicePolicyManager.setUninstallBlocked`, `setCameraDisabled`, `setScreenCaptureDisabled`, `setMaximumTimeToLock`, `addUserRestriction`, `setPermissionGrantState`, and `setLockTaskPackages`).
- Provide persistent HOME launcher selection (`KylokHomeActivity`), boot recovery and foreground crash recovery (`LauncherRecovery`).
- Run periodic and immediate synchronization loops via WorkManager (`EnrollmentWorker`) with on-device compliance reporting (`PolicyComplianceActivity`).
- Manage scoped device groups, resumable bulk assignments, deployment windows, reboot and secure-lock commands.
- Integrate SQLite or PostgreSQL state, scoped reads and replayable changes into your Go host.
- Optionally manage Wi-Fi credentials/private CA trust with scoped secret references and a verified recovery route.

Kylok runs on infrastructure owned by the host. The host retains application identity/signing, authorization, device/user relationships and its management UI. A business app owns its own screens, local data and business-specific workflows. While automated unit, conformance and interop tests verify software behavior, physical device qualification across OEM firmware remains tracked in the roadmap and compatibility evidence. See the [roadmap](ROADMAP.md) and [compatibility limits](docs/compatibility.md).

## Install the SDKs

```sh
go get github.com/thienvu18/kylok/go@v0.1.0-dev.40449063797
```

```kotlin
repositories { mavenCentral() }
dependencies { implementation("io.github.thienvu18.kylok:kylok-dpc:0.1.0-dev.40449063797") }
```

Public package downloads need no registry account. Follow the [integration guide](docs/sdk.md) for bootstrap APK verification, HTTP/2 serving, Android host identity and lifecycle. SQLite/filesystem are the default providers. The current source also includes [PostgreSQL and replayable changes](docs/host-integration.md), [Android host/app configuration](docs/managed-configuration.md), [fleet operations](docs/fleet-operations.md) and [managed networks](docs/managed-network.md); build from this source tree to use those APIs.

## Try the reference app

With the [toolchain](website/en/development.md) installed, start the example server and Android host:

```sh
./scripts/prepare-local-server.sh 192.168.101.5 http
./scripts/run-local-server.sh 192.168.101.5 http
```

For a local evaluation on a trusted LAN, the example serves the API and APK over HTTP without a domain or certificate. HTTP traffic is not encrypted. Open the example console at `http://127.0.0.1:8081` and enter the private token whose location appears in the startup log. The console is a reference app, not a hosted Kylok service. See the [example guide](website/en/examples.md) for enrollment and device management; start with the [SDK integration guide](website/en/install.md) to embed Kylok in your product.

The [English](website/en/index.md) and [Vietnamese](website/vi/index.md) guides explain setup, app delivery and policies. Browse the [API reference](website/en/api.md) and [compatibility evidence](website/en/compatibility.md) when planning an integration.

## Contributors

[![Contributors](https://contrib.rocks/image?repo=thienvu18/kylok)](https://github.com/thienvu18/kylok/graphs/contributors)

[SDK integration](docs/sdk.md) · [Architecture](docs/architecture.md) · [Protocol](docs/enrollment-protocol.md) · [Roadmap](ROADMAP.md) · [Contributing](CONTRIBUTING.md) · [Code of Conduct](CODE_OF_CONDUCT.md) · [Security](SECURITY.md)

[Implementation plan](docs/implementation-plan.md) · [Local messaging design](docs/local-messaging.md) · [Testing](docs/testing.md) · [Physical checks](docs/provisioning.md) · [Stable readiness](docs/release-readiness.md)

Original contributions use **LGPL-3.0-only**; see [NOTICE](NOTICE), [LICENSE](LICENSE) and [COPYING](COPYING).
