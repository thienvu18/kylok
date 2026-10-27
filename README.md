# Kylok — Android device management for your product

[![Status: experimental](https://img.shields.io/badge/status-experimental-orange)](ROADMAP.md)
[![License: LGPL-3.0-only](https://img.shields.io/badge/license-LGPL--3.0--only-blue)](LICENSE)

[English](README.md) · [Tiếng Việt](README.vi.md) · [Documentation](https://thienvu18.github.io/kylok/) · [Releases](https://github.com/thienvu18/kylok/releases)

Add Android device management to your own product with Kylok, without building enrollment, app delivery and policy workflows from scratch. Kylok pairs a **Go server SDK** with a **Kotlin Android Device Policy Controller (DPC) library**, running on infrastructure you control. Your team keeps ownership of authorization, server lifecycle, application identity and signing.

> [!IMPORTANT]
> Kylok is actively developed. Go and Android packages are available through the [Go module proxy](https://proxy.golang.org/github.com/thienvu18/kylok/go) and [Maven Central](https://central.sonatype.com/artifact/io.github.thienvu18.kylok/kylok-dpc). Build paired SDKs from the same source revision. Stable release qualification remains open; see [current validation](docs/compatibility.md).

## What you can build with Kylok

- Enroll Android devices using private, single-use QR codes and configured IP or domain origins over HTTP or HTTPS.
- Manage device identities with scope-bound enrollment, encrypted credential recovery and revocation.
- Assign signed APKs and track durable desired versions, device inventory, installation progress and results.
- Resume authenticated APK downloads, verify APK digests and signing identity, and recover interrupted installs.
- Apply basic policies for required apps, Android Lock Task, runtime permissions and selected restrictions, with observed results for each rule.
- Embed the SDKs in your own Go and Android applications. SQLite and filesystem storage are ready to use; custom providers remain under host control.
- Start from the example web console and Android dashboard for enrollment, apps, policies and device status.

Kylok is an SDK, not a hosted management service: your application controls authorization, server lifecycle, application ID and signing. The example apps show one way to connect the pieces. Source targets Android API 33–37; compile/target SDK is 37. See [validation](docs/compatibility.md) for the evidence collected so far.

## Verified on a real Android device

On a POCO Pad X1 running Android 15/API 35 and HyperOS, the current source completed online QR enrollment, signed app installation and update, managed app configuration and release, background sync, server restart, LAN recovery and reboot recovery. The server also rejected a duplicate request with changed content and a requested app downgrade. Camera, screenshot and account restrictions were exercised. This evidence applies to the recorded device, firmware and build. Android allowed app removal despite the reported uninstall restriction, so that control is not confirmed on this firmware. See the [device report](docs/validation/2026-10-27-poco-pad-x1.md) and [current validation](docs/compatibility.md).

## Install the SDKs

```sh
go get github.com/thienvu18/kylok/go@v0.1.0-dev.40316176224
```

```kotlin
repositories { mavenCentral() }
dependencies { implementation("io.github.thienvu18.kylok:kylok-dpc:0.1.0-dev.40316176224") }
```

Public package downloads need no registry account. Follow the [integration guide](docs/sdk.md) for bootstrap APK verification, HTTP/2 serving, Android host identity and lifecycle. SQLite/filesystem are the default providers. Current source also includes [PostgreSQL and replayable changes](docs/host-integration.md), plus [Android host/app configuration](docs/managed-configuration.md); build from this source tree to use those APIs.

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

[Implementation plan](docs/implementation-plan.md) · [Testing](docs/testing.md) · [Physical checks](docs/provisioning.md) · [Stable readiness](docs/release-readiness.md)

Original contributions use **LGPL-3.0-only**; see [NOTICE](NOTICE), [LICENSE](LICENSE) and [COPYING](COPYING).
