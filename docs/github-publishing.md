# GitHub publishing

Repository: [thienvu18/kylok](https://github.com/thienvu18/kylok), public `main`, SSH remote `git@github.com:thienvu18/kylok.git`. Original contributions use LGPL-3.0-only.

Use this description when publishing current development status:

> Self-hosted Android device management SDK for Go and Kotlin: LAN QR enrollment, APK deployment, policies and kiosk. Experimental.

## Discovery topics

```text
android android-enterprise mdm mobile-device-management device-management
 device-policy-controller device-owner self-hosted on-premises offline-first
 kiosk qr-code apk golang kotlin android-library protobuf grpc sdk lgpl-3-0
```

Use natural terms such as **self-hosted Android device management SDK**, Android MDM, DPC, Go, Kotlin, LAN provisioning and signed APK deployment in public descriptions. Topics describe scope, not tested compatibility. Keep readiness claims linked to [evidence](compatibility.md).

## Releases

Protected `main` requires up-to-date **SDK tests** and **Documentation build** checks, including for administrators; force pushes and deletion are blocked. PRs run tests. Each merge automatically builds, signs and publishes a unique development pair without rerunning tests.

Android uses Maven Central coordinates `io.github.thienvu18.kylok:kylok-dpc` with anonymous reads. Go uses `github.com/thienvu18/kylok/go` and `go/vVERSION` tags. Both SDK tags share the release source commit. [Release instructions](releases.md) cover secrets, public PGP identity, version generation and retry checkpoints. [GitHub Releases](https://github.com/thienvu18/kylok/releases) records actual availability; [compatibility](compatibility.md) limits device claims.

## Documentation website

The `Documentation site` workflow generates an English/Vietnamese landing page, installation/example guides, search and Go/Android API references. Preview with [the documentation build](development.md#documentation). After reviewed source is committed/pushed, enable **Settings → Pages → Source: GitHub Actions**. Pushes to `main` deploy `out/site`; pull requests only build it. The intended URL is `https://thienvu18.github.io/kylok/`; a live deployment is not yet verified. This workflow has separate Pages permissions and does not publish SDK packages.
