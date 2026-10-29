# Kylok roadmap

## Current capabilities

Kylok is a hostable Go and Android SDK for enrolling Device Owner agents, delivering signed APKs, applying observable device/app policies and integrating device state into a host's own control plane.

Current source provides scoped enrollment and management, durable SQLite or optional PostgreSQL storage, replayable host changes, typed app configuration, fleet groups and bulk assignments, scheduled windows, reboot/secure-lock commands, and optional managed Wi-Fi/private CA policies. For dedicated devices, current source implements persistent HOME launcher selection (`KylokHomeActivity`), boot recovery and foreground crash recovery (`LauncherRecovery`), WorkManager background and immediate synchronization loops (`EnrollmentWorker`), an on-device policy compliance activity (`PolicyComplianceActivity`), and silent APK installation via Android's `PackageInstaller`.

All device restrictions and policies are enforced directly at the OS level using standard Android platform APIs (`DevicePolicyManager.setUninstallBlocked`, `setCameraDisabled`, `setScreenCaptureDisabled`, `setMaximumTimeToLock`, `addUserRestriction`, `setPermissionGrantState`, and `setLockTaskPackages`). Other layers (such as reconciliation re-installations or in-app enforcement) serve strictly as supplementary defense-in-depth mechanisms. Hosts own identity, authorization, user/device relationships, operator UI, task data and server lifecycle.

Kylok is self-hosted and has no required cloud control plane. Automated unit, conformance and interop suites verify software behavior, but WAN-isolated provisioning, physical kiosk lifecycle and offline recovery still need device qualification. Android API/build support is not a device compatibility guarantee. Check [current compatibility](docs/compatibility.md) and [stable release readiness](docs/release-readiness.md) for the evidence boundary.

## Next milestones

| Milestone | Outcome | Acceptance condition |
| --- | --- | --- |
| N7 — Persistent launcher and recovery | A host can select a managed app or app set for HOME, start it after boot, and recover from agent/foreground-app process loss. Implemented in current source. | Verify actual HOME, Lock Task, boot, crash, app update and offline recovery on each supported firmware; observed state must distinguish a running launcher from a successful policy setter. |
| N8 — Offline maintenance | A technician has an authenticated, policy-controlled local path to inspect agent state, restore management connectivity, perform approved maintenance and return to the managed app. Implemented in current source. | Complete the flow with no server route, deny ordinary-user escape, preserve desired policy and business-app data, and recover safely after power loss. |
| N9 — Unified provisioning contract | Host provisioning data and Kylok Device Owner enrollment can be presented through one supported setup flow, with one-time credentials and a neutral host association reference. Implemented in current source. | A single scan provisions the DPC and binds the resulting scoped device identity to host-owned records without treating correlation metadata as authorization or exposing reusable credentials. |
| N10 — Kiosk and device lockdown | Hosts can request single/multi-app Lock Task and independently selectable Wi-Fi controls: block user configuration/toggles and disable the radio. Enforced at the OS level in current source. | Test each control alone, together and on release/readback on supported Android/OEM builds. Radio disable must preserve a safe way to restore management access. |
| N11 — LAN recovery and safe updates | Sync when the management network returns, retain periodic repair, and coordinate app updates with host-reported maintenance readiness. Implemented in current source. | Verify network loss/return, offline work, server loss, update interruption and data-safe install deferral. Do not claim immediate background delivery unless the platform provides it. |
| N12 — Local messaging | Offer a transport-neutral, scoped device-to-host and agent-to-authorized-app notification contract with a LAN transport adapter. MQTT 5 is the preferred first protocol candidate; see [local messaging design](docs/local-messaging.md). Commands implemented in current source. | Define identity, ACLs, expiry, acknowledgements, offline queuing, idempotency and Android process/lifecycle behavior. Demonstrate delivery while connected, safe redelivery after reconnect and no Internet dependency. |
| N13 — Stable qualification | Set supported device/firmware/network combinations, operational scale, upgrade and recovery limits, and stable release evidence. | Complete the [stable readiness gates](docs/release-readiness.md) for exact packages and supported hardware. |

## Integration boundaries

Hosts combine Kylok's Go APIs with their own enrollment records, authorization, console and business application. A host can provide a non-secret correlation reference during bootstrap and persist the resulting scoped device ID. It remains responsible for linking that device to its own operator, site and workflow records.

The Android agent owns Device Owner operations, managed app delivery, policy reconciliation and optional local app integration. A business app remains independently installable and owns its screens, local data and business-specific offline sync. Kylok exposes reusable device-management contracts; product workflows remain with the host.

Wi-Fi profiles and radio restrictions are optional device policies; they do not define the host's network deployment. Radio disable must retain a safe recovery path. The managed Wi-Fi/private CA API remains available to hosts that choose it.

The messaging design keeps a transport interface in the SDK, broker/runtime ownership with the host, and app delivery behind an authenticated Android boundary. Push carries an event or state-change hint; durable SDK reads remain authoritative. A device with no network route cannot receive a remote message until connectivity returns.

See the [implementation plan](docs/implementation-plan.md) for sequencing and acceptance, [architecture](docs/architecture.md) for system boundaries, and [compatibility](docs/compatibility.md) for evidence.
