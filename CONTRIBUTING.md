# Contributing to Kylok

Read the [architecture](docs/architecture.md), [roadmap](ROADMAP.md), and [development guide](docs/development.md). The project is experimental; APIs and local data formats may change without backward compatibility until release.

Use `dev` for the current development integration branch and open a PR into `main` to run the required checks.

For substantial work, open an [issue](https://github.com/thienvu18/kylok/issues) describing the problem, scope, milestone and observable outcome. Keep SDKs independent of consumers and preserve authentication, scope, persistence, integrity and idempotency guarantees.

Run the relevant automated checks; `make verify` covers both SDKs, independent package consumers, schemas, HTTP/HTTPS interoperability and release failure gates. A PR should explain the resulting behavior, verification and remaining limits. Update current docs instead of appending conversational history or duplicate plans. Keep version files and [release artifacts](docs/releases.md) consistent.

Document each handwritten method's purpose and public usage; explain non-obvious limits and effect/checkpoint ordering. Comments describe behavior rather than project milestones. `make docs-audit` checks method documentation; `make docs` generates API references and the English/Vietnamese site. Update both localized guides when a user flow changes; see [documentation development](docs/development.md#documentation).

For physical behavior, record model/API/firmware, APK digest, source/build identity, transport, network conditions and results. Distinguish unit/JVM, emulator and physical evidence. Never publish QR tokens, credentials, private signing material or personal device data.

Original contributions use **LGPL-3.0-only**. Preserve applicable third-party notices; see [NOTICE](NOTICE), [LICENSE](LICENSE) and [COPYING](COPYING). PRs to `main` require up-to-date **SDK tests** and **Documentation build** checks. Merging automatically publishes a unique development prerelease without rerunning tests; review changes with that effect in mind.
