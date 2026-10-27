# Build from source

Kylok is experimental. If you build from source, keep the Go and Android SDKs on the same source revision and use fresh development data when local formats change.

## Build and verify

Use Go 1.27.3 (module minimum 1.27.1), JDK 17, Gradle 9.3.1, Android Gradle Plugin 9.1.1 and Android SDK/build-tools 37. Set `JAVA_HOME` and `ANDROID_HOME`, then run:

```sh
make verify
```

These checks cover the Go service, Android library, network exchange and package-based integration. Android installation and policy effects are simulated; successful builds do not confirm behavior on a physical device. Check the [compatibility page](compatibility.html) before choosing devices for a deployment.

## Before you ship

Your application owns business authorization, server lifecycle, Android application ID and signing. Only Android's provisioning flow can grant Device Owner. Validate network trust and the management policies on the devices you plan to support; an application-bundled certificate cannot change the trust check that Android Setup Wizard performs before installing the app.

For contribution guidance, see the project's [contribution guide](https://github.com/thienvu18/kylok/blob/main/CONTRIBUTING.md).

`make verify` includes repository checks. Use `make check test-release-gates` for focused documentation/publication-gate changes, and `make docs` to rebuild this site. See the [testing strategy](https://github.com/thienvu18/kylok/blob/main/docs/testing.md) for suite boundaries and planned checks.
