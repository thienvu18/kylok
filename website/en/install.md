# Install and integrate

Kylok contains two independently versioned SDKs. Current development versions are Go `0.1.0-dev` and Android `0.1.0-dev`, using protocol 1. Pair them from the same source revision; protocol numbers alone do not promise development compatibility.

## Obtain packages

Choose a published version from [GitHub Releases](https://github.com/thienvu18/kylok/releases). Public Go modules and Maven Central need no read credentials. With `mavenCentral()` configured, declare `io.github.thienvu18.kylok:kylok-dpc:VERSION`; use `go get github.com/thienvu18/kylok/go@vVERSION` for Go. Replace VERSION with the released SDK version. For local packages, use a checkout with the documented toolchain:

```sh
make distribution
make test-consumers
```

`out/release` contains a Go module proxy, Maven repository, verified metadata, source/license archives and checksums. The complete downloadable bundle is prepared under `out/release-assets` by `make test-release`. Do not copy only an AAR: Maven metadata supplies its dependencies.

## Go host

Module: `github.com/thienvu18/kylok/go`. For local packages, add the generated `go-proxy` as a file proxy. Published Go versions will use `go/vX.Y.Z` repository tags.

From a new module, substitute the absolute distribution path:

```sh
GOWORK=off GOPROXY=file:///absolute/path/to/release/go-proxy,https://proxy.golang.org \
  GONOSUMDB=github.com/thienvu18/kylok/go \
  go get github.com/thienvu18/kylok/go@v0.1.0-dev
```

The checksum-database exception is only for unpublished local snapshots. Published versions use normal module verification.

```go
apk, err := provisioning.Inspect(ctx, signedHostAPK, provisioning.Tools{})
// Handle err before constructing the SDK.
sdk, err := kylok.New(ctx, kylok.Options{
    PublicURL: "http://192.168.101.5:8080",
    Bootstrap: apk,
    DataDir: "/var/lib/kylok",
})
// Handle err. Mount sdk.Handler() on the host-owned listener.
```

The host owns business authorization, listener lifecycle and shutdown. Enable HTTP/2: HTTP needs `http.Protocols.SetUnencryptedHTTP2(true)` alongside HTTP/1; HTTPS needs HTTP/2 and valid certificate trust. A plain HTTP/1 listener cannot serve the Android gRPC client.

`CreateEnrollment`/`QR` provision; `ListDevices`/`RevokeDevice` manage identities; application APIs register/assign/list/remove verified artifacts; policy APIs replace desired state and read observed compliance. Defaults persist in SQLite/filesystem. Custom `ports` providers remain host-owned and must pass the reusable conformance suites.

## Android host

Configure a Maven repository pointing to the extracted local repository and declare:

```kotlin
// Host dependency repository configuration:
repositories {
    google()
    mavenCentral()
    maven { url = uri("/absolute/path/to/release/maven") }
}
// Host Android module:
implementation("io.github.thienvu18.kylok:kylok-dpc:0.1.0-dev")
```

Use JDK 17, min SDK 33, compile/target SDK 37 and a compatible Kotlin 2.2 compiler. The host owns application ID, signing, cleartext permission/TLS trust and backup policy. Preserve the AAR's merged provisioning components and data-extraction exclusions. Only Android provisioning can grant Device Owner.

```kotlin
val registration = Kylok.observe(StatusListener { status -> render(status) })
Kylok.snapshot(context) { status -> render(status) }
// Close registration with the host lifecycle.
Kylok.retry(context) // schedules sync; completion is observed separately
```

Open `Kylok.confirmPendingInstall` from a deliberate user action when Android requires confirmation. Status/event hooks are read-only and best-effort; they cannot approve policy or bypass authentication. See the [API reference](api.html) and [example flows](examples.html).
