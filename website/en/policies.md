# Policies and kiosk

Use `SetPolicy` to update policy for a device your application is authorized to manage. Call `GetPolicy` to see the requested policy, the device's report and the result of each rule. Reuse a request key only when retrying the same policy; the same key cannot be used for different content.

```json
{
  "required_packages": ["io.example.kiosk"],
  "lock_task": {"packages": ["io.example.kiosk"], "kiosk_package": "io.example.kiosk"},
  "screen_capture_disabled": true,
  "restrictions": [{"name": "no_modify_accounts", "enabled": true}]
}
```

Register and assign required APKs before adding them to `required_packages`. Kiosk starts only after the device reports the required app at the expected version and signing identity. If an app is removed or changes, Kylok pauses kiosk until the device reports that the requirement is met again.

| Setting | Meaning |
| --- | --- |
| `required_packages` | Up to 64 apps that have already been assigned. Required apps are automatically blocked from uninstallation at the OS level (`setUninstallBlocked`). Removing an app from policy does not uninstall it. |
| `lock_task` | Apps allowed in kiosk and the optional kiosk launcher. Kylok confirms kiosk after Android reports managed Lock Task; screen pinning is not enough. The AAR automatically merges the persistent HOME launcher and boot recovery receiver into the host app; the host must be provisioned as Device Owner and the target kiosk app must declare a launchable Activity. |
| `camera_disabled`, `screen_capture_disabled` | Optional true/false settings. `false` explicitly requests that the feature remain enabled. |
| `maximum_time_to_lock_millis` | A value from 0 to 86400000. `0` removes the timeout restriction set by this admin. |
| `restrictions` | `no_add_user`, `no_debugging_features`, `no_modify_accounts` and `no_uninstall_apps`; set each rule to true or false. `no_uninstall_apps` blocks uninstallation across all installed apps at the OS level (`setUninstallBlocked`) in addition to user restrictions. |
| `permissions` | Up to 64 runtime-permission rules for required apps: `package_name`, `permission` and `DEFAULT`/`GRANTED`/`DENIED`. Android can reject unsupported grants. |

Each policy is a **full replacement**. If you omit a setting Kylok previously managed, it restores the original value it recorded. Send `{}` to release all Kylok-managed settings. `DEFAULT` stops managing a permission but does not revoke a permission that was already granted. Do not change the same Android admin setting elsewhere while Kylok manages it.

Policy changes may take time. Each rule reports `APPLIED`, `PENDING`, `FAILED` or `UNSUPPORTED` with a reason. Previously received policies can continue to apply while the device is offline. Treat a successful API call as a request, not proof that Android applied the setting. Enforcement depends on the device and firmware, so test the policies on your target devices.

## OS-level API enforcement

All device restrictions and policies are enforced directly at the OS level using standard Android platform APIs (`DevicePolicyManager.setUninstallBlocked`, `setCameraDisabled`, `setScreenCaptureDisabled`, `setMaximumTimeToLock`, `addUserRestriction`, `setPermissionGrantState`, and `setLockTaskPackages`). Other layers (such as reconciliation re-installations or in-app enforcement) serve strictly as supplementary defense-in-depth mechanisms. If Android provides no OS-level API for a required capability or restriction, this limitation must be clearly and prominently documented in the user guides.

## Managed app configuration in current source

`managed_configurations` provides typed Android application restrictions for required verified apps. Supported values are boolean, int32, string, string array, nested bundle and bundle array. The DPC journals/restores its original bundle and reports a digest of readback; the target app must separately apply its values. Enforcement depends on Android and OEM firmware. Qualify actual behavior and policy release on every supported device. See the [configuration contract](https://github.com/thienvu18/kylok/blob/main/docs/managed-configuration.md) and [compatibility evidence](compatibility.html).
