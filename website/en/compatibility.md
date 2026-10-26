# Compatibility and evidence

Source targets are Android API 33–37, with min SDK 33 and compile/target 37. That range is not a certified device/firmware matrix. Go/Android development packages are paired from one source revision; [GitHub Releases](https://github.com/thienvu18/kylok/releases) records public versions.

[Hosted SDK checks](https://github.com/thienvu18/kylok/actions/runs/40282303400) and the [documentation build](https://github.com/thienvu18/kylok/actions/runs/40282303423) passed on 2026-10-26 for source `4e0730f`. They establish automated integration, not physical Android support or registry availability.

| Evidence | What it proves |
| --- | --- |
| Go race/transaction tests | Enrollment binding, scoped persistence, replay, leases, policy/operation contracts. |
| JVM HTTP/HTTPS fixtures | Actual Go↔Kotlin wire, encrypted enrollment, authenticated downloads and reports; platform effects are simulated. |
| Android builds/minification/lint | Source and package integration, including merged DPC resources and retained reflective components. |
| Independent package consumers | A new Go host and Android host integrate from packages without copied SDK sources or project substitutions. |
| POCO Pad X1, Android 15 / API 35, HyperOS OS2.0.205.0.VOZMIXM | Historical original development APK: QR setup and authenticated LAN HTTP enrollment, **with WAN available**. |

## Maintainer-owned physical checks

- Factory-reset QR provisioning with WAN blocked through setup/download/enrollment, while LAN remains reachable.
- Real silent app installation/update, rejected/corrupt/incompatible APKs and required user action.
- Lost callbacks, process termination, LAN loss, server restart and device reboot, preserving identity/checkpoints.
- Higher-version same-signing DPC replacement retaining Device Owner, identity and connectivity.
- Actual kiosk entry/exit, required-app drift handling, permissions, restrictions and baseline restoration.

Record exact source/SDK/APK identity, digest/signer, model/API/firmware, transport and network conditions. Do not publish enrollment QR, tokens, credentials, private keys or personal identifiers. New dashboards display evidence; they cannot create physical compatibility proof.
