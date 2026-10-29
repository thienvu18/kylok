# Policies and kiosk

Per-device policy replacement uses `SDK.SetPolicy(ctx, scope, device, requestKey, kylok.Policy{...})` and `SDK.GetPolicy(ctx, scope, device)`. The host authorizes these calls. The example exposes CLI commands and an optional separately authenticated web console; the SDK's agent listener has no administrator endpoint.

Packaged hosts use the same [public SDK contract](sdk.md). Typed Android snapshots/listeners expose per-rule outcomes; optional Go events notify committed assignments/sync. These best-effort hooks do not approve policy operations, replace observed compliance or provide durable audit delivery.

Register and assign signed APKs first using the [local server guide](local-server.md). `required_packages` refers to those assignments, including their exact version and signer set. Lock Task and permission targets must be required packages. An app assignment continues independently when it is removed from a policy; policy replacement does not uninstall apps.

## Policy document

```json
{
  "required_packages": ["io.example.kiosk"],
  "lock_task": {
    "packages": ["io.example.kiosk"],
    "kiosk_package": "io.example.kiosk"
  },
  "screen_capture_disabled": true,
  "restrictions": [{"name": "no_modify_accounts", "enabled": true}]
}
```

| Field | Behavior |
| --- | --- |
| `required_packages` | Up to 64 already assigned packages; missing/wrong versions or signers keep prerequisites pending. Required packages are automatically protected against uninstallation at the OS level (`DevicePolicyManager.setUninstallBlocked`). |
| `lock_task` | Sorted allowlist and optional designated launcher package. Kiosk waits for all required apps, then uses managed Lock Task, not screen pinning. An empty kiosk package requests exit while retaining the specified allowlist; an empty object clears it. |
| `camera_disabled`, `screen_capture_disabled` | Optional booleans; false is an explicit desired setting. |
| `maximum_time_to_lock_millis` | Optional 0–86400000; zero removes this admin's timeout restriction. |
| `restrictions` | Explicit booleans for `no_add_user`, `no_debugging_features`, `no_modify_accounts`, `no_uninstall_apps`. No factory-reset restriction is added. `no_uninstall_apps` enforces uninstallation prevention at the OS level (`setUninstallBlocked`) across all installed applications, in addition to the `DISALLOW_UNINSTALL_APPS` user restriction. |
| `permissions` | Up to 64 `{ "package_name": "…", "permission": "android.permission.…", "state": "GRANTED" }` rules. States: `DEFAULT`, `GRANTED`, `DENIED`. Only requested runtime permissions are supported; Android/OEM restrictions can reject grants. |

This is a **full replacement**. Omitting a previously managed setting restores its captured baseline. `{}` releases all Kylok policy controls. Reuse a request key only for the same document; reordered sets are equivalent. A fresh key advances desired revision. No policy runs before its authenticated snapshot is persisted.

Android journals each original admin setting and attempt before effects, reads effective state afterwards, and rechecks cached policies even when the server is unreachable. Changed/released settings survive interruption. Hosts must not independently modify the same admin settings while Kylok manages them. Kiosk release restores the original allowlist and exits managed locking; it does not replay an earlier external app launch. Missing app prerequisites suspend an existing kiosk. Persistent HOME and boot recovery components (`KylokHomeActivity`, `LifecycleReceiver`) are declared in the AAR manifest and merge automatically into the host application; host apps must not duplicate these declarations. However, the host app must be provisioned as Device Owner (`dpm set-device-owner <hostPackage>/io.github.thienvu18.kylok.dpc.KylokDeviceAdminReceiver`), and the designated kiosk application must declare a launchable activity (`CATEGORY_LAUNCHER` or `CATEGORY_HOME`).

Results are per rule: `APPLIED`, `PENDING`, `FAILED`, `UNSUPPORTED`, with reason and observed value. Asynchronous kiosk entry requires both actual locked mode and a matching system callback from the current boot. A launch/setter return alone stays pending. Duplicate pending effects are throttled for 30 seconds. `cleanup` remains unconfirmed until released settings are restored. Missing/stale observations never imply success; APK changes invalidate earlier snapshot observations.

`DEFAULT` releases permission policy; it **does not revoke an existing grant**. This and sensor-grant limits follow [DevicePolicyManager](https://developer.android.com/reference/android/app/admin/DevicePolicyManager#setPermissionGrantState(android.content.ComponentName,java.lang.String,java.lang.String,int)). Kiosk entry/exit follows [Android Lock Task](https://developer.android.com/work/dpc/dedicated-devices/lock-task-mode). Actual OEM enforcement, launch restrictions and reboot behavior require the [manual policy checks](provisioning.md#policies-and-kiosk).

Kylok enforces all policy controls and restrictions at the OS level using standard Android platform APIs (`DevicePolicyManager.setUninstallBlocked`, `setCameraDisabled`, `setScreenCaptureDisabled`, `setMaximumTimeToLock`, `addUserRestriction`, `setPermissionGrantState`, `setLockTaskPackages`). For application removal, user restrictions (`DISALLOW_UNINSTALL_APPS`) alone can be bypassed on OEM launchers with package-deletion privileges; Kylok therefore enforces OS-level `PackageManager` blocks (`setUninstallBlocked`) for all required applications and when `no_uninstall_apps` is active. Reconciliation reinstallations serve solely as supplementary defense-in-depth. If the platform provides no OS API for a given restriction, that limitation is explicitly documented.

Android and OEM firmware can reject or inconsistently enforce device policies. In particular, do not treat `no_uninstall_apps` as effective without observing the uninstall flow on each supported build. Qualify the actual OS behavior and release/readback for every control; see the [physical checks](provisioning.md) and [compatibility evidence](compatibility.md).

## Managed app configuration

Kylok supports typed `managed_configurations` for required, verified assigned packages. Each value selects one of boolean, int32, string, string array, nested bundle or bundle array. The DPC journals its original bundle before effects and restores it on release; observations contain only a normalized digest. DPC readback and the target app applying settings are separate observations. See [configuration contracts and fixture](managed-configuration.md) for limits, examples and physical results.
