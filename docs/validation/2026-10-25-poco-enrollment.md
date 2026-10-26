# POCO QR enrollment, 2026-10-25

**Passed:** maintainer-reported factory-reset QR setup and server-verified initial enrollment. **Not tested:** WAN-blocked provisioning; the maintainer confirmed internet remained available.

| Evidence | Value |
| --- | --- |
| Device | POCO Pad X1; authenticated agent model `Xiaomi 25099RP08G`, Android 15 / API 35. |
| Firmware | HyperOS `OS2.0.205.0.VOZMIXM`; fingerprint `POCO/uke_p_global/uke:15/AQ3A.240801.002/OS2.0.205.0.VOZMIXM:user/release-keys`. |
| Transport | API and APK at `http://192.168.101.5:8080`, LAN reachable, WAN available. |
| Host | `io.github.thienvu18.kylok.example`, version code 1, non-debuggable, min 33/target 37; original agent `0.0.1-m2`. |
| Source | `5ed36f9` plus uncommitted M2 implementation; not a published release or evidence for subsequent builds. |
| Registry | Exactly one `MANAGED` device in `poco-lab`, ownership reported true, one consumed enrollment with encrypted recovery receipt. |
| Times | Created 23:06:43.213; authenticated inventory 23:06:54.553; checked after 23:09, Asia/Saigon (UTC+07). |
| Integrity | SQLite quick check `ok`; actual LAN APK returned 200, 11,107,238 bytes; signature/manifest verification and QR checksum matched. |

APK SHA-256: `71430d7e00ab5104b19743a2e81137d8c57566adf142bdddda3aedd5a316e0bf`.

Signing certificate SHA-256: `2379abc7825ee25208981382800083974dfeff2c1e83f3f0de9908bf0e1ac73d`.

Admin receiver: `io.github.thienvu18.kylok.dpc.KylokDeviceAdminReceiver`. Evidence used the exact immutable HTTP APK, not the separately built custom-CA APK. Device Owner was checked by the shipped worker and reported in authenticated inventory; there was no direct assistant ADB inspection or ownership shortcut.

This establishes the initial enrollment path only. Reboot/process recovery, actual app/DPC updates, user-action handling, HTTPS trust and offline setup require the [manual checklist](../provisioning.md). No private tokens, credentials or device IDs are included.
