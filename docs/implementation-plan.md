# Implementation plan

This plan follows the current host-neutral Go/Android SDK and the next dedicated-device capabilities. Hosts retain their users, assets, permissions, business rules, operator interface and server lifecycle. The [roadmap](../ROADMAP.md) lists outcomes; [compatibility](compatibility.md) separates automated coverage from device support.

## Current source

The SDK provides scoped enrollment/management, SQLite and PostgreSQL adapters, transactional change/projection reads, signed APK assignment and silent `PackageInstaller` execution, replacement policies enforced at the OS level, typed app configuration, fleet groups/bulk deployment, timed assignments, durable reboot/secure-lock commands and managed Wi-Fi/private CA policies. Dedicated-device capabilities implemented in current source include persistent HOME launcher selection (`KylokHomeActivity`), boot recovery and foreground crash recovery (`LauncherRecovery`), WorkManager periodic and immediate synchronization loops (`EnrollmentWorker`), and an on-device policy compliance activity (`PolicyComplianceActivity`). All device policies are enforced directly at the OS level using standard Android platform APIs (`DevicePolicyManager.setUninstallBlocked`, `setCameraDisabled`, `setScreenCaptureDisabled`, `setMaximumTimeToLock`, `addUserRestriction`, `setPermissionGrantState`, and `setLockTaskPackages`). Current contracts are described in [SDK integration](sdk.md), [fleet operations](fleet-operations.md), [managed networks](managed-network.md) and [host integration](host-integration.md).

## Next implementation sequence

| Order | Work | Interface direction | Acceptance evidence |
| --- | --- | --- | --- |
| N7 | Persistent HOME and agent recovery | Host supplies an allowed package set and startup package. Agent owns the persistent HOME activity, Lock Task transition and boot/process recovery. Implemented in current source. | Physical boot, foreground-app crash, agent process death, offline reboot, update and release tests with observed HOME/Lock Task state. |
| N8 | Offline maintenance | Device-local entry checks an explicit admin/technician authorization and offers a narrow set of recovery operations. Recovery state stays separate from business app storage. Implemented in current source. | No server connection required; unauthorized users cannot leave kiosk; interruption returns to a safe managed state. |
| N9 | Joined provisioning and enrollment | Compose one-time Android provisioning fields with Kylok token/origin plus optional opaque host correlation. Consume and bind each credential once; expose the scoped Kylok device ID to the host integration. Implemented in current source. | One setup flow and a secure host association across factory reset, lost replies and retries. QR/bootstrap data must not grant host business authorization. |
| N10 | Kiosk and Wi-Fi controls | Add independently observable Lock Task/Home policies and separate Wi-Fi configuration/toggle restrictions from radio disable. Enforced at the OS level in current source. | Verify each mode alone and together, then confirm separate readback, release and safe recovery across the declared API/OEM matrix. |
| N11 | LAN return and update readiness | Detect management-network return and resume sync/download work with periodic repair. Add an optional host-app readiness/defer contract before replacing a package. Implemented in current source. | Network loss/return and server loss; an update during unsent local data is deferred when the host reports busy; restart resumes safely. |
| N12 | Local messages and app bridge | Define a transport-neutral publisher/subscriber contract, device/app identity, message ID, TTL, replay/ack rules and a constrained Android delivery API. MQTT 5 is the first protocol candidate; implementations remain adapters. Commands implemented in current source. | Security review, session/expiry behavior, duplicate handling, reboot/process-death behavior, scoped ACL tests and physical idle-device evaluation under the declared network/power matrix. |
| N13 | Stable support qualification | Document exact Android/device/adapter/network combinations, measured fleet capacity, upgrade/recovery behavior and stable release evidence. | All [stable readiness](release-readiness.md) gates pass for the announced package/hardware matrix. |

## Enrollment and ownership contract

The host creates an authorized, scoped enrollment record and asks Kylok to issue its short-lived device credential. If a single QR flow includes both DPC bootstrap and host association, it contains only one-time Kylok credentials and a non-secret opaque correlation reference. It does not include a host session, user password, or business authorization grant. After authenticated enrollment, the host maps the resulting scoped Kylok device ID to its own record and performs its own device/business association.

DPC provisioning is an Android Setup Wizard/device-owner operation. A host's existing mobile-app enrollment endpoint is a separate business contract; one operator flow may compose both without merging their credentials or authorization domains.

## Update and local data safety

The host app owns its offline records, upload queue and a readiness signal such as `READY`, `BUSY` or `RECOVERY_REQUIRED`. Kylok may defer a package replacement when the host reports that stopping it would endanger unsent data. The host cannot weaken APK signature/digest verification or Device Owner policy. Reinstallation/update completion remains based on observed package state; business-data upload completion remains the app's own responsibility.

## Local messaging architecture

Use a transport-neutral SDK contract and keep broker selection out of device policy/domain APIs. The first candidate is MQTT 5 behind an optional host adapter, with a host-owned broker reachable on the local management network. Maintain a durable outbox and authoritative state/read APIs in the host; messages only signal a new event/state revision.

Prefer QoS 1 with stable message IDs and consumer deduplication over assuming exactly-once business effects. Give each device a scoped broker identity and topic ACL; authorize app consumers separately. Bound queued message count/bytes, session retention and message expiry. Do not retain personal/task content as broker state or publish secrets. A disconnected device receives queued messages only after it regains a route.

Android process/background limits mean a persistent socket cannot be promised as always-on or instantaneous. Compare a foreground MQTT client against WorkManager/network-return synchronization on target firmware and power modes. A brokerless WebSocket/gRPC stream is an alternative if operations prefer fewer services; it still needs durable per-device queues, reconnect, auth and fan-out. Define the transport/app contract first, then choose a maintained, license-compatible Go/Kotlin implementation after security, battery, reconnection and device tests.

## Verification and cleanup

Provider conformance, fault-injection tests, transport fixtures, independent consumers and release scripts remain useful maintenance assets. Keep them when they check a current contract or a plausible future regression. Remove only a test or script after confirming it has no current workflow, public guide, release or recovery caller and its behavior is covered by a maintained replacement.

Keep English/Vietnamese product guides aligned. Keep detailed device evidence in [compatibility](compatibility.md); product introduction pages should describe current capabilities and planned work without serving as run logs.
