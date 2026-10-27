# SDK integration

Both SDKs are in active development; [GitHub Releases](https://github.com/thienvu18/kylok/releases) lists available public versions. `make distribution test-consumers` produces and tests local packages; [release instructions](releases.md) describe publication. Go and Android have independent `VERSION` files. Build paired SDKs from the same source revision.

## Go host

Module: `github.com/thienvu18/kylok/go`; current package version is in `go/VERSION`. For published packages, use the release's `go/vX.Y.Z` repository tag through normal Go module resolution. For local artifact verification use the generated `out/release/go-proxy` as a file proxy, without a workspace or `replace` directive.

Install the public Go package from a new Go module:

```sh
go get github.com/thienvu18/kylok/go@v0.1.0-dev.40316176224
```

For a local source distribution, substitute the absolute distribution path:

```sh
GOWORK=off GOPROXY=file:///absolute/path/to/release/go-proxy,https://proxy.golang.org \
  GONOSUMDB=github.com/thienvu18/kylok/go \
  go get github.com/thienvu18/kylok/go@v0.2.0-dev.40449063797
```

The checksum-database exception is for unpublished local snapshots; use normal module verification for published versions.

```go
apk, err := provisioning.Inspect(ctx, signedHostAPK, provisioning.Tools{})
// Handle err before constructing the SDK.
sdk, err := kylok.New(ctx, kylok.Options{
    PublicURL: "http://192.168.101.5:8080", Bootstrap: apk, DataDir: "/var/lib/kylok",
})
// Handle err. Mount sdk.Handler(), own the listener and drain it before sdk.Close().
```

Android uses gRPC over HTTP/2. For an HTTP listener, set `http.Server.Protocols` to an `http.Protocols` with `SetHTTP1(true)` and `SetUnencryptedHTTP2(true)`; plain `http.ListenAndServe` defaults are insufficient. For HTTPS, enable HTTP/2 with valid TLS and matching host trust. The [minimal server](../examples/minimal-server/main.go) demonstrates both configurations; the external Go consumer exercises actual h2c gRPC.

`Inspect` requires Android `apksigner`/`apkanalyzer`; `InspectApplication` verifies ordinary APKs. Hosts supply business authorization before enrollment/QR, device, application, operation and policy calls. `ListApplications` pages the scoped verified catalog. `QRWithOrigins(ctx, token, apiOrigin, bootstrapOrigin)` redraws an existing token for a different IP/domain/transport without changing its identity or integrity binding; it does not retarget enrolled devices. Never expose management methods as anonymous remote actions. `Handler` mounts agent/API/APK routes; `BootstrapHandler` allows a separate bootstrap origin. [Transport/protocol](enrollment-protocol.md) and [policies](policies.md) define their invariants. `Version()` identifies the Go SDK.

Defaults are durable SQLite/filesystem. Override `Options.Store` / `Artifacts` with `ports.StateStore` / `ArtifactStore`; hosts close supplied providers. Methods must be concurrent/context-safe and preserve atomicity, authentication, scope isolation, retry binding, idempotency and immutable content. Run `portstest.State`, `Operations` and `Policies`, plus backend crash/durability tests. `Clock` is injectable. Apply additional business validation before SDK calls, never by weakening adapter checks.

`Options.Events` optionally receives sanitized committed device/enrollment/sync/app-assignment/policy/revocation identifiers. Delivery uses one worker and a bounded queue; the callback must honor its context (`EventTimeout`: default 1s, range 1ms–30s). Errors/panics do not change results. Events can drop, duplicate on replay or be lost on shutdown; deduplicate by event ID and use `ListChanges`/`ReadProjection` for durable projection recovery and export permanent audit data into host-owned storage before retention expires. Hosts own the sink; the SDK cancels its worker on `Close` and bounds the wait even for a faulty sink.

## Android host

Public Android packages use Maven Central: configure `mavenCentral()`; the currently available public coordinate is `io.github.thienvu18.kylok:kylok-dpc:0.1.0-dev.40316176224`. Local development coordinates are `io.github.thienvu18.kylok:kylok-dpc:0.2.0-dev.40449063797`. Point a Maven repository at the extracted `kylok-maven.zip` or `out/release/maven`, then declare:

```kotlin
// In the host's dependency repository configuration:
repositories {
    google()
    mavenCentral()
    maven { url = uri("/absolute/path/to/release/maven") }
}
// In the host Android module:
implementation("io.github.thienvu18.kylok:kylok-dpc:0.2.0-dev.40449063797")
```

Use min SDK 33, compile/target 37, JDK 17 and a compatible Kotlin 2.2 compiler. Maven metadata supplies transitive dependencies; copying only an AAR omits that information. The AAR contributes provisioning/admin/install/lifecycle components and consumer rules for Room/WorkManager. The host owns application ID, signing, HTTP permission/TLS trust and backup policy; preserve the SDK's data-extraction exclusions. It becomes Device Owner only through Android provisioning.

`Kylok.retry(context)` schedules reconciliation; `confirmPendingInstall(context)` opens a pending OS confirmation from a host user action. `snapshot(context) { AgentStatus -> … }` returns sanitized typed local state on the main thread. `observe(StatusListener { … })` registers optional notifications after worker commits; close the returned registration with the host lifecycle. Notifications are read-only, best-effort, not durable history. Observer exceptions cannot fail agent work. `status(context)` supplies formatted diagnostics; `sdkVersion` / `protocolVersion` expose SDK identity. Call `snapshot` for current state after subscribing; earlier notifications are not replayed.

The independent hosts in `tests/consumers/` demonstrate package-only integration, custom provider conformance and a minified Android build. The fresh Go host uses the new Android host's signed APK with default APK checks. They do not execute Android services on a device. [Physical checks](provisioning.md) and [compatibility evidence](compatibility.md) remain required before claiming runtime support.

Snapshots include the enrolled endpoint, last authenticated sync, desired/observed app versions, per-rule outcomes and durable install progress. Old policy observations become pending after a desired revision change. Confirmation intents, credentials, tokens and keys are excluded. The example dashboard consumes these public values. Generate signatures/KDoc and usage reference pages with [the documentation build](development.md#documentation).

The supported host surface is the Go facade/`ports`/`provisioning` contracts and Android `Kylok`, status/listener models and merged provisioning components. Generated protocol and implementation/storage classes are not general extension points. Custom business authorization/policy validation belongs before Go facade calls; arbitrary privileged Android policy plugins are not provided.

Current source provides [host integration](host-integration.md): scoped point/domain-page reads, opaque enrollment correlation, optional PostgreSQL, readiness and durable change/projection APIs. [Android configuration](managed-configuration.md) documents `HostOptions` and typed managed app restrictions. Custom providers implement management and change/projection contracts; run `portstest.Management` and `portstest.Changes` with the other conformance checks.
