# Install and integrate

Kylok is a pair of libraries: a Go SDK for your server and an Android Device Policy Controller (DPC) library for your device app. Choose matching Go and Android versions from the same release. Your applications continue to own business authorization, server lifecycle, Android application ID and signing.

## Obtain packages

Choose a version from [GitHub Releases](https://github.com/thienvu18/kylok/releases). The Go module is public, and the Android library is available from Maven Central; neither requires read credentials. Development packages may change without backward compatibility, so use them only with the matching release pair.

## Go host

Add the Go SDK to your server module:

```sh
go get github.com/thienvu18/kylok/go@vVERSION
```

Inspect the signed Android host APK, create the SDK and mount its handler on your server:

```go
apk, err := provisioning.Inspect(ctx, signedHostAPK, provisioning.Tools{})
// Handle err before constructing the SDK.
sdk, err := kylok.New(ctx, kylok.Options{
    PublicURL: "https://mdm.example.com",
    Bootstrap: apk,
    DataDir: "/var/lib/kylok",
})
// Handle err. Mount sdk.Handler() on your HTTP server.
```

Your server controls authorization, listening addresses and shutdown. The Android client uses HTTP/2 gRPC: enable HTTP/2 on the listener; if you use HTTP, enable h2c as well. A listener that only supports HTTP/1 cannot serve the Android SDK.

Use the Go APIs to create QR enrollments, list or revoke devices, register and assign APKs, review installation operations, and set or inspect policies. SQLite and filesystem storage are included. Use the `ports` interfaces if your product needs custom storage.

## Android host

Add Maven Central and the Kylok dependency to your Android app:

```kotlin
repositories {
    google()
    mavenCentral()
}
dependencies {
    implementation("io.github.thienvu18.kylok:kylok-dpc:VERSION")
}
```

Use JDK 17, min SDK 33, compile/target SDK 37 and a compatible Kotlin 2.2 compiler. Your app owns its application ID, signing key, network trust and backup policy. Keep the provisioning components and backup exclusions supplied by the library. Only Android's provisioning flow can grant Device Owner.

Read device status or schedule a sync from your app:

```kotlin
val registration = Kylok.observe(StatusListener { status -> render(status) })
Kylok.snapshot(context) { status -> render(status) }
// Close registration with your app lifecycle.
Kylok.retry(context) // schedules sync; read the result separately
```

Open `Kylok.confirmPendingInstall` only after a user action when Android requests install confirmation. Status callbacks are read-only notifications; they cannot approve policies or bypass authentication. See the [API reference](api.html) and [example flows](examples.html).
