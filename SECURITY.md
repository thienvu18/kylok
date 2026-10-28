# Security Policy

## Supported versions

Kylok is an experimental Go and Android device management SDK under active development. Security updates and bug fixes are delivered through the active development branches and published development prereleases.

Because development contracts and local schemas evolve, paired Go and Android SDKs must be built from the same source revision.

| Version | Supported | Description |
|:---|:---:|:---|
| `main` / `dev` | :white_check_mark: | Active source branches |
| Latest prerelease (`v0.1.0-dev.*`) | :white_check_mark: | Evaluation packages on Go proxy and Maven Central |
| Earlier revisions | :x: | Upgrade to current source or the latest prerelease |

## Reporting a vulnerability

If you discover or suspect a security vulnerability in Kylok, please disclose it responsibly. **Do not create public GitHub issues, discussions, or pull requests for security vulnerabilities.**

### Preferred channel: GitHub Private Vulnerability Reporting

Submit an advisory through GitHub:
[Report a vulnerability](https://github.com/thienvu18/kylok/security/advisories/new)

### Alternative channel: Direct maintainer contact

Email the maintainer at [thienvu1802@gmail.com](mailto:thienvu1802@gmail.com) with the subject prefix `[SECURITY] Kylok: <brief description>`.

### Critical reporting rule

> **Never publish QR enrollment tokens, device credentials, private signing material, or personal device data in any report or public communication.** Use synthetic test identifiers and test configurations when creating proof-of-concept demonstrations.

### Information to include

To help us triage and resolve the issue quickly, please provide:

1. **Component**: Go server SDK (`go/`), Android DPC library (`android/kylok-dpc/`), Protocol schema (`proto/kylok/v1/`), or example apps (`examples/`).
2. **Environment**: Go version, Android API level, OEM firmware build, device model or emulator configuration, and transport mode (HTTP/HTTPS).
3. **Description**: Clear description of the vulnerability, attack vector, and potential impact.
4. **Reproduction steps**: Minimal steps or code using public APIs to reproduce the behavior safely.
5. **Expected vs. observed behavior**: Explain the discrepancy between security guarantees and observed outcome.

## Scope and threat model

### In scope

- **Enrollment security**: Single-use private QR tokens, atomic token consumption, scope isolation, device credential issuance, and revocation.
- **Transport security**: Enforced HTTPS certificate and hostname validation without silent downgrade; TLS verification across IP and domain endpoints.
- **Application delivery integrity**: Resumable authenticated APK downloads, SHA-256 digest validation, and APK signing certificate identity verification before package installation.
- **Policy enforcement and state durable order**: Durable checkpoints persisted prior to external side effects; state recovery across server restarts, LAN disruptions, and device reboots.
- **Host adapter idempotency**: Replay protection, deduplication of concurrent/retried operations, and strict separation between desired, observed, and compliance states.

### Out of scope

- Physical tampering on an unlocked Android device before Device Owner provisioning has completed.
- Unencrypted HTTP communication explicitly chosen on a trusted local LAN for evaluation or development (as documented in reference examples).
- Vulnerabilities within host-owned business authorization, consumer-tier application logic, external reverse proxies, or custom storage engines outside Kylok SDK boundaries.
- Android platform vulnerabilities or OEM firmware bugs that violate Android Enterprise Device Owner contracts (though documented in [compatibility evidence](docs/compatibility.md)).

## Vulnerability handling process

1. **Acknowledgment**: We aim to acknowledge receipt of reports within 48 hours.
2. **Assessment**: The maintainer will investigate, assess impact, and reproduce the behavior.
3. **Remediation**: A fix will be developed on a private branch and validated against automated suites (`make verify`) and physical hardware tests where relevant.
4. **Coordinated disclosure**: A security advisory will be published along with the patched release. Credit will be given to the reporter unless anonymity is requested.
