# Compatibility and evidence

Kylok targets Android API 33–37, with min SDK 33 and compile/target 37. This is the supported build range, not a certification for every device or firmware in that range. Use the Go and Android packages from the same release; [GitHub Releases](https://github.com/thienvu18/kylok/releases) lists available versions.

[SDK integration checks](https://github.com/thienvu18/kylok/actions/runs/40282303400) passed on 2026-10-26 for source `4e0730f`. They confirm the Go and Android packages work together in automated environments; they do not certify a physical Android device.

| Evidence | What it proves |
| --- | --- |
| Go server behavior | Enrollment, device scoping, sync, app assignments and policy operations are covered by automated checks. |
| Go and Android communication | Authenticated enrollment, APK downloads and reports work over HTTP/HTTPS; Android installation and policy effects are simulated. |
| Android package integration | The AAR builds and works in independent Android apps, including a minified app build. |
| Package-based integration | Independent Go and Android apps consume the published SDK packages without copying SDK source into their projects. |
| POCO Pad X1, Android 15 / API 35, HyperOS OS2.0.205.0.VOZMIXM | One development APK completed QR setup and authenticated LAN enrollment over HTTP, **with internet available**. |

## What still needs testing on devices

- Enroll a reset device with internet blocked throughout setup and APK download while keeping the management server reachable on the LAN.
- Install and update apps on a real device, including failed, incompatible and user-confirmed installations.
- Recover device identity and unfinished work after network loss, process termination, server restart and device reboot.
- Update the DPC while keeping Device Owner status, device identity and connectivity.
- Apply and release kiosk, app, permission and restriction policies on the target firmware.

If your deployment depends on one of these scenarios, verify it on the exact device, firmware and network setup you intend to use. Treat enrollment QR codes, tokens, credentials and private keys as secrets.
