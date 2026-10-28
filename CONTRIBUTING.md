# Contributing to Kylok

Read the [architecture](docs/architecture.md), [roadmap](ROADMAP.md), and [development guide](docs/development.md). Please review our [Code of Conduct](CODE_OF_CONDUCT.md) before participating, and report vulnerabilities privately through our [Security Policy](SECURITY.md). Kylok is in active development; use the current source contracts and fresh development data when schemas change.

Use `dev` for the current development integration branch and open a PR into `main` to run the required checks.

For substantial work, open an [issue](https://github.com/thienvu18/kylok/issues) describing the problem, scope and observable outcome. Keep SDKs independent of consumers and preserve authentication, scope, persistence, integrity and idempotency guarantees.

Run the relevant automated checks; `make verify` includes `make check` and covers both SDKs, independent package consumers, schemas, HTTP/HTTPS interoperability and release failure gates. A PR should explain the behavior, verification and remaining limits. Keep docs concise and current, and avoid duplicate plans. Keep version files and [release artifacts](docs/releases.md) consistent.

Document each handwritten method's purpose and public usage; explain non-obvious limits and effect/checkpoint ordering. Comments describe behavior rather than project milestones. `make docs-audit` checks method documentation; `make docs` generates API references and the English/Vietnamese site. Update both localized guides when a user flow changes; see [documentation development](docs/development.md#documentation).

Follow the [testing strategy](docs/testing.md); stable publication uses [release readiness](docs/release-readiness.md), independently of roadmap completion.

For physical behavior, record model/API/firmware, APK digest, source/build identity, transport, network conditions and results. Distinguish unit/JVM, emulator and physical evidence. Never publish QR tokens, credentials, private signing material or personal device data.

Original contributions use **LGPL-3.0-only**. Preserve applicable third-party notices; see [NOTICE](NOTICE), [LICENSE](LICENSE) and [COPYING](COPYING). PRs to `main` require up-to-date **SDK tests** and **Documentation build** checks. Merging automatically publishes a unique development prerelease without rerunning tests; review changes with that effect in mind.
