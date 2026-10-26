# Development and generated documentation

The project uses Go 1.27.1, JDK 17, Gradle 9.3.1, Android Gradle Plugin 9.1.1 and Android SDK/build-tools 37. Set `JAVA_HOME`, `ANDROID_HOME` and Android tool paths first. Dependencies are pinned by Go sums and Gradle lockfiles.

```sh
make verify
python3 scripts/check-repository.py
```

Verification covers Go race/adapter tests, JVM agent behavior, HTTP/HTTPS interoperability, signed host updates, a new Go host and a minified Android host consuming packages. Simulator/build success does not prove Android OS enforcement.

## Generate this site

```sh
python3 -m venv .tools/docs-venv
.tools/docs-venv/bin/python -m pip install -r website/requirements.txt
make docs
```

Go reference pages are extracted through Go's AST from current signatures and comments. Dokka generates the Android API from KDoc. Authored English/Vietnamese guides and generated references are assembled into `out/site`; a GitHub Pages workflow builds and deploys that artifact. Never hand-edit generated HTML.

Every handwritten public method documents purpose and concrete usage; private/internal methods explain their purpose. Explain non-obvious protocol values, limits and effect ordering. Comments describe behavior, never development phase labels. Generated/third-party source is excluded from manual comment requirements.

## Contribute

Keep SDKs independent of examples and business frameworks. Preserve authentication, scope isolation, atomic token consumption, immutable APK integrity and operation idempotency. Keep desired/observed/compliance state separate. Development APIs and databases may change; do not add compatibility adapters just for earlier development builds. Preserve the original `design.md` and third-party notices. Original code is LGPL-3.0-only.
