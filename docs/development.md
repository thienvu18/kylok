# Development

Kylok is experimental. Rebuild both SDKs after schema changes and use fresh development data when formats change. There is no frozen pre-release protocol baseline or migration guarantee. Do not replace an enrolled device/server deployment automatically while developing.

## Toolchain

| Tool | Version |
| --- | --- |
| Go / JDK | 1.27.1 / 17 |
| Gradle / Android Gradle Plugin | 9.3.1 wrapper / 9.1.1, built-in Kotlin 2.2.10 |
| Android SDK | `platforms;android-37.0`, `build-tools;37.0.0`; min 33, compile/target 37 |
| Buf / Go generators | 1.70.0 / Protobuf 1.36.12, Connect 1.21.1 |
| JVM generators | Protobuf 4.36.2, gRPC 1.85.1, Gradle plugin 0.10.0 |
| Android persistence/scheduling | Room 2.8.5 / WorkManager 2.12.0 |

Set `JAVA_HOME` and `ANDROID_HOME`. Add `$ANDROID_HOME/build-tools/37.0.0` and `$ANDROID_HOME/cmdline-tools/latest/bin` to `PATH`. Verification also uses Python 3, OpenSSL, curl and make. Go sums and Gradle lockfiles pin dependencies; initial builds require downloads.

## Commands

```sh
make generate       # pinned generators and generated Go
make schema         # Buf lint, format and compilation
make test-go        # Go race/storage/transport tests
make build-go       # SDK and independent example module
make test-android   # JVM checkpoint, crypto, download, installer and policy tests
make build-android  # AAR, example APK and Android lint
make interop        # real HTTP/HTTPS Go↔Kotlin enrollment/install/policy fixtures
make test-host-update # signed version 1→2 APKs and isolated custom-CA build
make distribution   # local Go/Maven/source/license archives and manifest checks
make test-consumers # independent Go module + minified Android Maven hosts
make test-release   # invalid source/tag/version gates and artifact tampering
make test-console   # authenticated example flows using a real signed APK
make docs-audit     # purpose/usage comments in Go, Kotlin and maintainer Python
make verify
python3 scripts/check-repository.py
./scripts/check-generation.sh
```

Interop verifies a real signed APK, HTTP2 gRPC, SQLite, downloads, agent checkpoints, policy assignment/readback and release. Installer/policy OS effects are simulated; HTTPS uses a test-only trusted localhost certificate. It uses fresh private fixtures and cleans up its own processes/data. Without fixture variables, only the network interop test skips during `make test-android`; both full fixtures execute it.

Go is generated into `go/gen/`; Gradle generates Java Lite/stubs for Kotlin into build output. Do not edit generated files. PR CI runs the same verification coverage in parallel Go/schema, Android/transport and package-consumer groups, without publication. A merge into protected `main` runs the separate build/sign/publish workflow without tests; hosted execution is separate evidence.

## Build outputs and host APIs

- `out/minimal-server`; `go/` and the separate example build with `GOWORK=off`.
- `android/kylok-dpc/build/outputs/aar/kylok-dpc-release.aar`.
- `examples/minimal-dpc/build/outputs/apk/{debug,release,customCA}/`.

See [local server](local-server.md) for signed builds and CLI usage. `-PhostVersionCode=2` builds a higher-version example for manual update testing; preserve the signing key. The optional `customCA` variant requires `KYLOK_CA_CERT` and `KYLOK_TRUST_HOST`. SDK versions are independent of the host APK version.

The Go public facade exposes enrollment/QR, device listing/revocation, application registration/assignment/removal, operation listing and policy replacement/status. Custom stores implement `ports` transaction/concurrency guarantees and should run `portstest.State`, `portstest.Operations` and `portstest.Policies`, plus backend durability/failure tests. Hosts own authorization and supplied resources. Artifact removal rejects live references; content bytes are retained rather than unsafely deleting shared blobs.

Android contributes provisioning/admin components, installation callbacks, boot/package replacement receivers and backup exclusions. Hosts must preserve those exclusions if overriding data-extraction rules. `Kylok.retry` schedules sync; `Kylok.status` returns sanitized status; `Kylok.confirmPendingInstall` opens an OS confirmation from a user action. The example permits HTTP; other hosts choose cleartext and TLS trust policy.

[SDK integration](sdk.md) documents typed snapshots/listeners, event hooks and provider ownership. [Release tooling](releases.md) verifies Go module and Maven consumers outside the workspace, including minification, metadata/licenses and exact artifact checksums. No normal target publishes remotely. Workflow changes are also checked with pinned `actionlint` 1.7.12; local builds remain distinct from hosted CI/registry execution.

## Documentation

```sh
python3 -m venv .tools/docs-venv
.tools/docs-venv/bin/python -m pip install -r website/requirements.txt
make docs
python3 -m http.server 8000 --bind 127.0.0.1 --directory out/site
```

Open `http://127.0.0.1:8000`. Authored guides live in `website/en/` and `website/vi/`; update both languages. The build extracts Go signatures/comments through its AST, runs pinned Dokka 2.2.0 for Android KDoc, and assembles navigation, language switching, search and API references under `out/site`. Markdown is pinned in `website/requirements.txt`. Generated output is ignored; do not edit it. `make verify` audits method documentation without needing site-renderer dependencies. `make docs` also checks translated pages and local site links.

Handwritten public methods need purpose, contract and concrete usage (platform callbacks explain their invocation). Internal/private methods need purpose comments. Explain non-obvious protocol values, limits and checkpoint/effect ordering; comments describe behavior rather than milestone labels. Generated/third-party source is excluded. The documentation workflow builds PRs and deploys `main` through GitHub Pages once the repository setting is enabled; it does not publish SDK packages.

## CI runtime

Workflows share `.github/actions/setup-build`. Actions use the latest stable releases verified at update time, pinned by commit; their JavaScript runtime is Node 24. Dependabot checks for action updates daily. Hosted jobs use `ubuntu-latest` and current stable Go; Android retains its compatible JDK 17 line with the newest patch, project Gradle wrapper and SDK/build-tools versions.

Go/schema checks, Android integration and package/release checks run concurrently. The required **SDK tests** check succeeds only after every group passes; **Documentation build** remains separately required. Go/Gradle dependency caches are reused, duplicate generation/schema steps are removed, and each HTTP/HTTPS fixture reruns only the test task while retaining compiled dependencies. PRs build the site without uploading a deployment artifact. Merge deployment builds/signs/publishes without tests.
