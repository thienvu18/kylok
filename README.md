# Kylok — Self-hosted Android Device Management SDK

[![Status: experimental](https://img.shields.io/badge/status-experimental-orange)](ROADMAP.md)
[![License: LGPL-3.0-only](https://img.shields.io/badge/license-LGPL--3.0--only-blue)](LICENSE)

Embed Android mobile device management in your own product: a **Go server SDK** and **Kotlin Device Policy Controller AAR**, connected over your LAN. Hosts own authorization, server lifecycle, application identity, and signing.

> [!IMPORTANT]
> Development APIs, schemas and databases may change without backward compatibility. See [GitHub Releases](https://github.com/thienvu18/kylok/releases) for available development packages. Physical enrollment passed on one POCO / Android 15 configuration with internet available; offline provisioning and physical installation/recovery tests remain separate requirements.

## Current scope

- QR provisioning from signed APK metadata; HTTP or HTTPS, IP addresses or domain names.
- Scope-bound, single-use enrollment with encrypted credential recovery and revocation.
- Durable desired app revisions, inventory, operation leases, progress and report acknowledgements.
- Authenticated APK downloads with resume, digest/signing checks and installer session recovery.
- Device Owner checks and observed package/version/signer verification, including the DPC update path.
- [Basic policies and kiosk](docs/policies.md): required apps, Lock Task, permissions, selected restrictions and per-rule observed compliance.
- Versioned Go/Maven distributions, typed Android status and optional Go event hooks; [independent consumer tests and release tooling](docs/releases.md).
- Example web console for QR enrollment, interface/domain selection, APKs, policies and operations; Android dashboard for local observations and actions.

SQLite and filesystem adapters are included. The example Android host uses encrypted Room state, Android Keystore, and WorkManager. Target APIs are 33–37; compile/target SDK is 37. See [validation](docs/compatibility.md) for what has actually been tested.

## Build and run

With the [toolchain](docs/development.md) installed:

```sh
make verify
./scripts/prepare-local-server.sh 192.168.101.5 http
./scripts/run-local-server.sh 192.168.101.5 http
```

The server hosts APIs and APKs together; HTTP needs no domain or certificate. Open the example console at `http://127.0.0.1:8081` and enter the private token from the file named in its startup log. Follow the [local server guide](docs/local-server.md) for QR/interface selection and management. To embed Kylok in a new product, use the [SDK guide](docs/sdk.md); `make distribution` creates local packages under `out/release`. PRs run tests; merging into protected `main` automatically publishes a signed development pair through [the release workflow](docs/releases.md).

The [English](website/en/index.md) and [Vietnamese](website/vi/index.md) documentation sources include installation and example walkthroughs. `make docs` generates the site and Go/Android API references; see [development](docs/development.md#documentation). The Pages workflow deploys reviewed source after it reaches `main` and GitHub Pages is enabled.

[Architecture](docs/architecture.md) · [Protocol](docs/enrollment-protocol.md) · [Roadmap](ROADMAP.md) · [Implementation plan](docs/implementation-plan.md) · [Contributing](CONTRIBUTING.md)

[Physical checks](docs/provisioning.md) · [Compatibility](docs/compatibility.md) · [Publishing](docs/github-publishing.md) · [Original design draft](design.md)

Original contributions use **LGPL-3.0-only**; see [NOTICE](NOTICE), [LICENSE](LICENSE) and [COPYING](COPYING).
