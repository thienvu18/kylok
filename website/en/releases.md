# Release packages

Go publishes through `github.com/thienvu18/kylok/go` tags; Android publishes to Maven Central as `io.github.thienvu18.kylok:kylok-dpc`. Choose an available version from [GitHub Releases](https://github.com/thienvu18/kylok/releases). Development pairs share one source revision and may change contracts without migration.

## Publication flow

1. Open a PR to protected `main`. The up-to-date branch must pass **SDK tests** and **Documentation build**.
2. A merge automatically builds and signs both SDKs, without running tests during deployment. Each version appends the GitHub run ID, for example `0.1.0-dev.123`.
3. Automation preserves packages in a draft, publishes the signed Maven bundle through Central Portal, creates immutable `go/vVERSION` and `android/vVERSION` tags, requests Go indexing and publishes one GitHub prerelease.
4. The two SDK tags and `release/RUN_ID` identify the same generated release source commit, whose parent is the merged commit. Only version files, matching guide examples and build metadata differ.
5. A failed workflow resumes its saved artifacts and deployment ID. An ambiguous upload requires recovering the actual ID from Central Portal; never upload the same version blindly.

Public consumers need no Maven registry credentials. Configure `mavenCentral()` and use the released version. A future verified Maven namespace can be used for new releases; existing coordinates stay immutable.

## Local packages

```sh
make distribution
make test-consumers
make test-release
```

The complete `out/release-assets/kylok-distribution.zip` includes the Go proxy, Maven AAR/sources/KDoc/POM/module metadata, source, licenses, dependency manifests and checksums. `kylok-release.json` records versions and source identity. Dirty local packages cannot be published.

After publication, `KYLOK_REMOTE_SDK=all ./scripts/test-consumers.sh` explicitly checks public packages from the matching release source and distribution. This is a separate maintainer check. Stable v1 still requires all physical and software release gates. See the repository's [release operations](https://github.com/thienvu18/kylok/blob/main/docs/releases.md) for secrets, signing and recovery.
