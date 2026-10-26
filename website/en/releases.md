# Release packages

Kylok is released as a Go server SDK and an Android library. Choose matching versions from [GitHub Releases](https://github.com/thienvu18/kylok/releases). Go packages are available from the public Go module proxy; Android packages are on Maven Central. Development releases may change without backward compatibility.

## Install a published version

Use the version shown in GitHub Releases for both SDKs. Add the Go module to your server with `go get github.com/thienvu18/kylok/go@vVERSION`. Add `mavenCentral()` to your Android repositories, then declare `io.github.thienvu18.kylok:kylok-dpc:VERSION` as a dependency. You do not need registry credentials to download either package.

## Build packages from source

If you are modifying Kylok or need to build packages locally, run:

```sh
make distribution
make test-consumers
```

The packages are created under `out/release` for local integration. This does not publish them. See the [development guide](development.html) for build prerequisites and checks.
