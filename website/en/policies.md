# Policies and kiosk

Use `SetPolicy` to replace desired policy for an authorized device and `GetPolicy` to inspect desired, reported and per-rule compliance. Reusing a request key with the same normalized document is idempotent; changed content under that key conflicts.

```json
{
  "required_packages": ["io.example.kiosk"],
  "lock_task": {"packages": ["io.example.kiosk"], "kiosk_package": "io.example.kiosk"},
  "screen_capture_disabled": true,
  "restrictions": [{"name": "no_modify_accounts", "enabled": true}]
}
```

Register and assign required APKs first. Exact desired versions/signers are prerequisites for kiosk. If they disappear or drift, the agent suspends its kiosk lock until repair is observed.

| Setting | Meaning |
| --- | --- |
| `required_packages` | Existing desired app assignments, up to 64. Policy removal does not uninstall them. |
| `lock_task` | Allowlist and designated launcher; managed locking must be observed, not just screen pinning. |
| `camera_disabled`, `screen_capture_disabled` | Optional booleans; false is an explicit setting. |
| `maximum_time_to_lock_millis` | 0–86400000; zero releases this admin's timeout restriction. |
| `restrictions` | `no_add_user`, `no_debugging_features`, `no_modify_accounts`, `no_uninstall_apps`, each with an explicit boolean. |
| `permissions` | Up to 64 required-package runtime rules: `package_name`, `permission`, and `DEFAULT`/`GRANTED`/`DENIED` state. Android may reject unsupported grants. |

This is a **full replacement**. Omitted previously managed settings restore their captured baseline. `{}` releases all Kylok policy controls. `DEFAULT` releases permission policy but does not revoke an existing grant. Hosts must not independently mutate the same admin setting while Kylok manages it.

Each rule reports `APPLIED`, `PENDING`, `FAILED` or `UNSUPPORTED` with a reason. A persistent baseline/attempt journal handles interruption and cleanup. Cached authenticated policies reconcile without server access. A current-boot system callback plus actual managed locking confirms kiosk entry. Physical OEM enforcement still requires device tests.
