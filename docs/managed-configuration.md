# Android host and managed app configuration

## Early host configuration

Call the public API in your host's `Application.onCreate` on every process creation, including worker/receiver starts:

```kotlin
Kylok.configure(this, HostOptions(
    syncIntervalMinutes = 30,
    retryBackoffSeconds = 60,
    wakeJitterSeconds = 5
))
```

Defaults work without configuration or an opened Activity. Options are immutable and process-local: sync period 15–1440 minutes, retry backoff 30–3600 seconds, jitter 0–300 seconds. Capture/scheduling/worker starts freeze the options under one lock. Repeating equal configuration is allowed; conflicting configuration after work starts throws. Configure early rather than racing SDK components. No option can change enrolled origin, authentication, ownership, APK verification or policy checks.

The SDK owns its executor; WorkManager owns persistent workers. Close each status-listener registration with its host UI lifecycle; do not shut down global WorkManager or SDK executors. Scheduling is best-effort Android background work, not realtime. Changing process options does not promise immediate replacement of an already enqueued periodic request.

## Typed app restrictions

A managed configuration is part of the device's full replacement policy. Its target must be a required package with an existing verified APK assignment. Kylok waits for that exact package/version/signer before applying it. Only the target app interprets its configuration; Kylok does not inject business authorization or arbitrary privileged plugins.

```json
{
  "required_packages": ["dev.kylok.configuration"],
  "managed_configurations": [{
    "package_name": "dev.kylok.configuration",
    "values": {"entries": [
      {"key": "display_name", "value": {"string": "Reception"}},
      {"key": "allow_refresh", "value": {"boolean": false}}
    ]}
  }]
}
```

Go exposes `ManagedConfiguration`, `ConfigurationBundle`, `ConfigurationEntry` and `ConfigurationValue`; each value selects exactly one nonnil pointer (`Boolean`, `Integer`, `String`, `Strings`, `Bundle` or `Bundles`). Integers are signed 32-bit; empty strings/arrays/bundles are valid. The wire uses typed protobuf oneofs. Keys are unique ASCII identifiers up to 128 characters, sorted recursively; array order is significant. Limits: 64 configurations, 64 keys per bundle, 256 total entries, depth 4, 64 elements per array, 4096 UTF-8 bytes per string and 64 KiB of normalized JSON configuration content.

The DPC uses `DevicePolicyManager.setApplicationRestrictions` with Android Bundles. It captures this administrator's original bundle in protected state and persists each attempt before mutation. Readback confirms DPC state; release restores only that journaled baseline. Missing apps remain pending, unsupported baseline types fail before effects, and interrupted changes/releases retain their journal. The host must not concurrently mutate the same managed bundles.

Rule `configuration:PACKAGE` reports `APPLIED`, `PENDING`, `FAILED` or `UNSUPPORTED`; `policy.managed_configuration` is its capability. Observed values contain only `sha256:` plus the digest of normalized typed protobuf bytes. Raw configuration and baseline values stay out of status callbacks/diagnostics and event notifications. Authorized Go policy/projection reads still contain desired configuration values, so protect administrative routes and storage accordingly.

A matching readback does not mean the target app applied settings. The target reads `RestrictionsManager.applicationRestrictions` at startup and handles `ACTION_APPLICATION_RESTRICTIONS_CHANGED`. Its business acknowledgement belongs in the host product, separately from DPC compliance. [Android's contract](https://developer.android.com/reference/android/app/admin/DevicePolicyManager#setApplicationRestrictions(android.content.ComponentName,java.lang.String,android.os.Bundle)) defines platform-supported types and ownership.

## Independent app fixture and manual check

`examples/configuration-app` has no Kylok dependency. Build with `:configuration-app:assembleDebug`; its APK is `examples/configuration-app/build/outputs/apk/debug/configuration-app-debug.apk`. Register/assign that signed APK, then submit the policy above through the example server. The app applies `display_name` to its title and `allow_refresh` to its actual refresh button; it reads persisted values again after process restart. Changing/removing these settings demonstrates app receipt, update and baseline release separately from the DPC's digest report.

On POCO Pad X1/API 35, the independent app fixture displayed assigned title/button values, retained them after app force-stop/reopen and offline device reboot, and returned to defaults after policy release. A same-signer v1→v2 update also completed. These results cover this device and build; other firmware and physical `HostOptions` behavior remain untested. Automated JVM/HTTP/HTTPS checks simulate OS effects; see the [device report](validation/2026-10-27-poco-pad-x1.md).
