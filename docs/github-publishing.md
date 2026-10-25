# GitHub publishing and discoverability

Kylok is hosted at [thienvu18/kylok](https://github.com/thienvu18/kylok) as a public repository. This guide records its repository metadata, discoverability topics, and future publishing conventions. SDK packages, a documentation website, and releases are not available yet.

## Repository identity

| Field | Repository configuration |
| --- | --- |
| Repository name | `kylok` |
| Owner | `thienvu18` |
| SSH remote | `git@github.com:thienvu18/kylok.git` |
| Visibility / default branch | Public / `main` |
| Display title | Kylok — Self-hosted Android Device Management SDK |
| About description | Open-source, self-hosted Android device management (MDM) SDK for Go and Kotlin. Designed for LAN QR provisioning, signed APK deployment, and kiosk policies. Design stage. |
| Website | Leave empty until a maintained documentation site exists. |
| License | GNU Lesser General Public License v3.0 only (`LGPL-3.0-only`). |

Use the confirmed owner consistently in clone URLs, Go module paths, documentation links, and release automation. Select and verify Maven publishing coordinates separately. Do not publish placeholder coordinates from the design as installable packages.

## GitHub topics

GitHub topics help people discover related repositories. GitHub permits at most 20 topics, each using lowercase letters, numbers, and hyphens and no more than 50 characters. Manage them with `gh repo edit thienvu18/kylok --add-topic` or the repository's **About → Edit → Topics** field. Keep the remote configuration aligned with this list. See [GitHub's topic guidance](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/classifying-your-repository-with-topics).

Repository topics, ordered by relevance:

```text
android
android-enterprise
mdm
mobile-device-management
device-management
device-policy-controller
device-owner
self-hosted
on-premises
offline-first
kiosk
qr-code
apk
golang
kotlin
android-library
protobuf
grpc
sdk
lgpl-3-0
```

These classify the project's intended scope; they do not certify Android compatibility or feature readiness. Revisit them when scope changes.

## Search keywords and README content

Primary phrase: **self-hosted Android device management SDK**.

Use related terms naturally in the description, opening paragraph, relevant feature explanations, and documentation titles:

- Android mobile device management, MDM SDK, Android Enterprise, Device Policy Controller, DPC, Fully Managed Device Owner.
- Go device management SDK, Kotlin Android AAR, embeddable MDM, host-neutral device management.
- LAN QR provisioning, offline-first Android management, on-premises kiosk management, signed APK deployment, DPC self-update.

Prefer useful descriptions and stable headings to repeated keyword lists or hidden HTML metadata. A GitHub README is not a website template with a supported SEO configuration file. Keep readiness qualifiers close to feature descriptions; replace design-stage text only after implementation and evidence exist. Do not advertise an official Google affiliation, universal OEM support, or production readiness.

## Roadmap and issue organization

Keep [ROADMAP.md](../ROADMAP.md) as the discoverable public milestone overview. When implementation tracking starts, create GitHub milestones M0–M6 using the same names and exit gates. Convert work IDs from the [implementation plan](implementation-plan.md) into linked issues rather than maintaining a second copy of detailed progress.

Suggested issue labels are `type:bug`, `type:feature`, `type:docs`, `area:go`, `area:android`, `area:protocol`, `area:provisioning`, `area:tooling`, `priority:p0`, `priority:p1`, `priority:p2`, and `needs-device-test`. Add `good first issue` or `help wanted` only to scoped issues with enough context to contribute independently. Labels and milestones are suggestions, not configured GitHub state.

## Release tags and package visibility

Repository topics and Git release tags serve different purposes. Topics classify the project; tags identify immutable release commits. There are no release tags yet.

| Artifact | Planned tag examples | Requirement |
| --- | --- | --- |
| Go module in `go/` | `go/v0.1.0`, later `go/v1.0.0` | Use the module subdirectory prefix required by [Go's module version mapping](https://go.dev/ref/mod#vcs-version). |
| Android AAR | `android/v0.1.0`, later `android/v1.0.0` | Match the selected Maven artifact version and publication workflow. |
| Wire protocol | Protobuf namespace `kylok.v1` | Version compatibility independently of SDK tags. |

Do not create a v1.0.0 tag for the documentation baseline. At release time, publish notes with behavior changes, migration steps, compatible server/agent versions, tested device/firmware rows, known limits, package coordinates, and source/license links. Add registry/version/CI badges only when their real targets exist. Publish package descriptions and keywords in the registries' supported metadata once those packages are configured.
