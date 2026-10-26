# SDK distributions and releases

Go uses the public module `github.com/thienvu18/kylok/go`; Android uses Maven Central coordinates `io.github.thienvu18.kylok:kylok-dpc`. [GitHub Releases](https://github.com/thienvu18/kylok/releases) records available versions and corresponding source. Development releases may change APIs, schemas and local formats without migration. They do not certify physical device support; stable v1 requires the [release acceptance gates](../ROADMAP.md#v1-release-acceptance).

## PR checks and automatic deployment

Protected `main` requires an up-to-date PR with **SDK tests** and **Documentation build** passing, including for administrators. Force pushes and deletion are blocked. PRs run all verification suites in parallel, generation/schema/repository checks and the documentation build without publishing secrets. The required SDK check rejects any failed, cancelled or skipped suite.

Every merge into `main` starts `Deploy SDKs`. Deployment builds and signs packages without running tests. Maven Central still performs its own package validation. Each run appends its unique GitHub run ID to the version bases in `go/VERSION` and `android/VERSION`; for example, `0.1.0-dev` becomes `0.1.0-dev.123`. A base without a prerelease suffix becomes `X.Y.Z-dev.RUN_ID`. Rerunning a workflow retains its version.

Automation creates a release source commit whose parent is the merged commit, updating only version files, matching guide examples and `release/build.json`. It leaves `main` unchanged. The source tag `release/RUN_ID` and SDK tags `go/vVERSION` / `android/vVERSION` point to that same commit. Existing tags cannot move.

The workflow creates a draft, preserves the complete distribution and signed Central bundle, waits for Central to report `PUBLISHED`, publishes the SDK tags, requests Go proxy indexing, then publishes one GitHub prerelease with source, notices, checksums and package archives. A failure leaves the draft and deployment checkpoint available for retry. Stable releases are a separate maintainer decision.

## Credentials and signing

Repository Actions secrets:

| Secret | Value |
| --- | --- |
| `MAVEN_CENTRAL_USERNAME` | Central Portal user-token username |
| `MAVEN_CENTRAL_PASSWORD` | Central Portal user-token password |
| `MAVEN_SIGNING_KEY` | Armored private release-signing PGP key |
| `MAVEN_SIGNING_PASSWORD` | PGP passphrase |
| `MAVEN_SIGNING_FINGERPRINT` | Full expected signing fingerprint |

The workflow uses GitHub's automatic `GITHUB_TOKEN` for tags/releases. Central credentials are the Portal token pair, not an account password. The dedicated [public signing key](../release/signing-public-key.asc) must be retrievable from a Central-supported keyserver. Keep private backups outside Git and renew the key before expiry. PGP signs Maven artifacts; Android host APK signing remains host-owned.

## Local verification and recovery

With the [toolchain](development.md) installed:

```sh
make verify
make distribution
make test-consumers
make test-release
```

`out/release` contains a Go file proxy, Maven repository with AAR/sources/KDoc/POM/module metadata, corresponding source, licenses, dependency manifests, `kylok-release.json` and `SHA256SUMS`. `out/release-assets/kylok-distribution.zip` is the complete downloadable archive. Dirty local snapshots are identified and cannot enter publication. Packaging excludes private keys, generated trust, runtime data and build output.

`scripts/check-distribution.py out/release` checks hashes, source identity, archives, notices and metadata. Independent consumers resolve packages without source substitution; Android also builds with minification. `make test-release` covers fresh-checkout cleanliness, invalid source/version/tag gates, artifact tampering and publication retry behavior. Git stores text with normalized LF bytes; the Windows wrapper checks out as CRLF through `.gitattributes`. These tests run on PRs.

After publication, maintainers can explicitly run `KYLOK_REMOTE_SDK=all ./scripts/test-consumers.sh` from the matching release source and prepared distribution. Public Go and Maven Central reads need no registry credentials. This check is separate from deployment.

Rerun a failed workflow to reuse its prepared bytes and Portal deployment ID. `central-deployment.json` binds the upload to its source and bundle digest. If an upload response was lost before its ID was recorded, automation stops instead of uploading twice. Recover the actual deployment ID in Central Portal, restore the matching release checkout and saved artifacts, then run `python3 scripts/central.py publish --tag release/RUN_ID --deployment-id UUID` with credentials supplied privately. Rerun the original workflow to finish. Never replace released bytes or retag an existing version.

## Changing Maven namespace

After verifying a new namespace in Central, change the group ID for future releases and update consumer coordinates. Existing versions remain under their original coordinates and cannot be overwritten or moved. Kotlin packages, host application IDs and the Go module path are independent of the Maven group ID.

See [Central requirements](https://central.sonatype.org/publish/requirements/), [Publisher API](https://central.sonatype.org/publish/publish-portal-api/), [immutable components](https://central.sonatype.org/faq/can-i-change-a-component/) and [Go subdirectory tags](https://go.dev/ref/mod#vcs-version).
