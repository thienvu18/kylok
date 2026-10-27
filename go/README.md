# Kylok Go SDK

Experimental self-hosted Android device management engine. Module: `github.com/thienvu18/kylok/go`; SDK identity: `kylok.Version()`. Go and Android SDKs should use the same source revision.

The currently available public version is `v0.1.0-dev.40316176224`. Install with `go get github.com/thienvu18/kylok/go@v0.1.0-dev.40316176224`; use the matching Android SDK version.

See the repository's [SDK integration](https://github.com/thienvu18/kylok/blob/main/docs/sdk.md), [architecture](https://github.com/thienvu18/kylok/blob/main/docs/architecture.md), [release guide](https://github.com/thienvu18/kylok/blob/main/docs/releases.md) and [compatibility evidence](https://github.com/thienvu18/kylok/blob/main/docs/compatibility.md). Hosts own authorization, listeners and supplied adapters. Defaults use durable SQLite/filesystem; HTTP/HTTPS and IP/domain origins are supported.

Original code is LGPL-3.0-only; retain `LICENSE`, `COPYING` and `NOTICE`. Dependencies retain their published licenses. The corresponding repository source/schema and build scripts accompany release archives.
