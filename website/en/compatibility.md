# Current device validation

The source targets Android API 33–37, with min SDK 33 and compile/target SDK 37. This is a build range, not a device support list. Go and Android SDKs should be built from the same source revision.

| Device | Build and network | Observed result |
| --- | --- | --- |
| POCO Pad X1, Xiaomi `25099RP08G`, Android 15 / API 35, HyperOS `OS2.0.205.0.VOZMIXM` | Source `d9c375993eb0e4be78f1fa2868ca497ce345bb14`; LAN HTTP at `192.168.101.5`; Internet available | Online enrollment, signed app install/update, configuration, background sync, server/LAN/reboot recovery and selected policies passed. Uninstall prevention failed despite `APPLIED` readback. |

## Remaining checks

WAN-blocked QR enrollment could not be tested because the router cannot isolate WAN while keeping LAN access. Kiosk, runtime permissions, DPC replacement, process force-stop recovery, secure lock timeout, Add user enforcement, alternate origins, revocation and other models/firmware remain open or untested. `HostOptions` has automated coverage but no physical consumer-host result.

Automated Android tests simulate operating-system effects; they do not prove OEM enforcement. See the [detailed device report](https://github.com/thienvu18/kylok/blob/main/docs/validation/2026-10-27-poco-pad-x1.md), [physical checklist](https://github.com/thienvu18/kylok/blob/main/docs/provisioning.md) and [stable readiness gates](https://github.com/thienvu18/kylok/blob/main/docs/release-readiness.md). Keep tokens, credentials and private keys out of shared evidence.
