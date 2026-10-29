# Roadmap

## Current SDK

Kylok provides Go and Android libraries for scoped enrollment, signed APK delivery, observed policies, managed app configuration, durable fleet assignments and commands, optional PostgreSQL, change feeds and managed Wi-Fi/private certificate policies. Dedicated-device capabilities implemented in current source include persistent HOME launcher selection (`KylokHomeActivity`), boot recovery and crash recovery (`LauncherRecovery`), WorkManager background synchronization (`EnrollmentWorker`), on-device compliance reporting (`PolicyComplianceActivity`), and silent APK installation. Policies are enforced directly at the OS level using standard Android platform APIs. Hosts control identity, authorization, UI, lifecycle and business workflows.

## Planned work

Software interfaces and automated test suites for milestones N7 through N12 are implemented in the current source revision. Acceptance requires physical device qualification and release gating across supported firmware.

| Order | Capability | Status |
| --- | --- | --- |
| N7 | Persistent HOME launcher, boot recovery and foreground app recovery. | Implemented in source; physical boot/recovery qualification pending. |
| N8 | Authenticated on-device maintenance and compliance inspection. | Implemented in source; physical offline qualification pending. |
| N9 | Joined provisioning contract bootstrapping Device Owner enrollment and host association. | Implemented in source; physical single-scan setup qualification pending. |
| N10 | Kiosk/Lock Task and Wi-Fi restrictions enforced at the OS level. | Implemented in source; physical OEM enforcement qualification pending. |
| N11 | Sync recovery on network return and safe update deferral. | Implemented in source; physical network-loss qualification pending. |
| N12 | Transport-neutral local messaging and one-shot device commands. | Commands and contracts in source; physical idle-device evaluation pending. |
| N13 | Stable device/firmware/network support and release qualification. | Gated by stable readiness checklist. |

A remote message cannot arrive while a device has no route. After reconnection, bounded queued events and durable state reconciliation must recover safely. Background delivery timing depends on Android and OEM behavior.

See the [implementation sequence](https://github.com/thienvu18/kylok/blob/main/docs/implementation-plan.md), [local messaging design](https://github.com/thienvu18/kylok/blob/main/docs/local-messaging.md), and [current compatibility](compatibility.html).
