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
| `required_packages` | Up to 64 apps that have already been assigned. Removing an app from policy does not uninstall it. |
| `lock_task` | Apps allowed in kiosk and the optional kiosk launcher. Kylok confirms kiosk after Android reports managed Lock Task; screen pinning is not enough. |
| `camera_disabled`, `screen_capture_disabled` | Optional true/false settings. `false` explicitly requests that the feature remain enabled. |
| `maximum_time_to_lock_millis` | A value from 0 to 86400000. `0` removes the timeout restriction set by this admin. |
| `restrictions` | `no_add_user`, `no_debugging_features`, `no_modify_accounts` and `no_uninstall_apps`; set each rule to true or false. |
| `permissions` | Up to 64 runtime-permission rules for required apps: `package_name`, `permission` and `DEFAULT`/`GRANTED`/`DENIED`. Android can reject unsupported grants. |

Each policy is a **full replacement**. If you omit a setting Kylok previously managed, it restores the original value it recorded. Send `{}` to release all Kylok-managed settings. `DEFAULT` stops managing a permission but does not revoke a permission that was already granted. Do not change the same Android admin setting elsewhere while Kylok manages it.

Policy changes may take time. Each rule reports `APPLIED`, `PENDING`, `FAILED` or `UNSUPPORTED` with a reason. Previously received policies can continue to apply while the device is offline. Treat a successful API call as a request, not proof that Android applied the setting. Enforcement depends on the device and firmware, so test the policies on your target devices.

## Managed app configuration in current source

`managed_configurations` provides typed Android application restrictions for required verified apps. Supported values are boolean, int32, string, string array, nested bundle and bundle array. The DPC journals/restores its original bundle and reports a digest of readback; the target app must separately apply its values. The independent sample app receives, applies and releases configuration on the POCO Pad X1/API 35. On this firmware, Android still allows app removal while `no_uninstall_apps` reports `APPLIED`. See the [configuration contract](https://github.com/thienvu18/kylok/blob/main/docs/managed-configuration.md) and [device report](https://github.com/thienvu18/kylok/blob/main/docs/validation/2026-10-27-poco-pad-x1.md).
