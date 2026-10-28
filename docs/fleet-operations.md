# Fleet operations and device commands

Hosts authorize every scope and device before calling these APIs. The SDK stores opaque groups and request IDs; business labels, users and permissions stay in the host. Current software has automated coverage on SQLite/PostgreSQL and the Android engine. Reboot, secure lock and scheduled installation still require physical qualification.

## Groups and bounded fan-out

`ReplaceGroup` replaces up to 100 unique device IDs. Revision zero creates; subsequent changes supply the observed revision. An identical retry at the original revision returns its committed result. A conflicting update fails with `ErrConflict`; foreign/revoked members fail before membership changes. An empty membership keeps an empty group.

```go
group, err := sdk.ReplaceGroup(ctx, scope, "lobby", deviceIDs, 0)
// Handle err before using group.Revision in the next replacement.
batch, err := sdk.CreateFleetAssignment(ctx, kylok.FleetRequest{
    ScopeID: scope, ID: "nightly-install", GroupID: group.ID,
    ArtifactSHA256: digest, NotBefore: start, Deadline: start.Add(time.Hour),
})
// Handle err, then advance until batch.Complete; keep the same batch ID across restarts.
batch, err = sdk.AdvanceFleetAssignment(ctx, scope, batch.Request.ID, 20)
```

Select exactly one group or explicit device set. Creation freezes at most 100 targets and the group revision. Retries bind the original selector, APK and absolute window; membership changes do not change an existing batch. Each advance atomically submits a bounded chunk with deterministic per-device keys and saves its progress. A storage/context error rolls back that chunk. Permanent per-device rejections become `FAILED` items; successful submissions become `ASSIGNED` with an operation ID. `Complete` describes fan-out completion, not installation success. Read operations and device inventory separately.

The host owns scheduling and worker lifecycle. Scan `QueryFleetAssignments` after restart, resume incomplete batches, and periodically start a new scan to catch IDs inserted behind a live cursor. `QueryGroups` and batch cursors are bound to their authorized scope. `group_changed`, `fleet_changed` and `device_changed` are transactional invalidations; consistent `ReadProjection` includes all three resource types.

## Windows, precedence and cancellation

`ScheduleApplication` and fleet requests use an absolute UTC `[NotBefore, Deadline)` interval of at most seven days. Scheduled retries bind the exact window. The server does not lease work before its start; Android checks the window before staging and again immediately before commit. Both clocks matter: maintain device/server time. A clock rollback delays work; it cannot extend the absolute deadline. No daemon or in-memory timer is needed to retain schedules.

The latest committed assignment wins for a device/package, including a bulk item committed after a direct assignment. An already submitted bulk item is never submitted again by retrying its batch. Cancel or supersede unwanted pending intent explicitly.

`CancelOperation` stops dispatch/download and signals retained Android work to abandon uncommitted sessions. `CancelFleetAssignment` also stops unsubmitted targets. `CANCELLED` does not establish that a delivered installation stopped: a cached response or a committed Android installer session may finish. Cancellation preserves desired intent and observed inventory, creates no rollback/uninstall and does not automatically retry drift. Submit a new request key to resume. Deadline expiry similarly cannot reverse a committed platform effect.

## One-shot reboot and secure lock

```go
command, err := sdk.SubmitCommand(ctx, scope, deviceID,
    kylok.CommandReboot, "maintenance-reboot", time.Hour)
// Handle err before reading command.ID.
command, err = sdk.GetCommand(ctx, scope, deviceID, command.ID)
```

`CommandReboot` and `CommandLock` require Device Owner and an advertised `command.reboot`/`command.lock` capability. At most sixteen actions can be active; one is delivered per sync in submission order. `GetCommand`, `QueryCommands`, `CancelCommand`, device snapshots and durable device changes expose lifecycle without dispatch tokens. Android's `AgentStatus.commands` exposes local progress until the server acknowledges a result.

| State | Meaning |
| --- | --- |
| `QUEUED` | No dispatch committed. Cancellation is safe at this boundary. |
| `DISPATCHED` | One stable dispatch token is committed; redelivery reuses it. |
| `SUCCEEDED` | Agent platform readback meets the command's observation contract. |
| `FAILED` | A precondition/platform rejection or undispatched expiry was reported. |
| `UNCERTAIN` | Delivery/effect may have happened; no automatic replacement is authorized. |
| `CANCELLED` | Undispatched work was stopped. |

Android atomically checkpoints before the effect. Reboot success requires a boot-counter increase; reconnection alone is insufficient. A process restart with the same boot counter after arming produces `UNCERTAIN`, including a crash before Android was called. Lock requires a configured PIN/password/pattern, then secure and locked keyguard readback. A swipe lock is rejected. Readback describes device state, not causal attribution or attestation.

After dispatch, cancellation/expiry becomes `UNCERTAIN`; a later valid readback can resolve it. An exact repeated report is acknowledged; changing a committed report conflicts. Reusing the same host key returns the original action and deadline. Create a new key only after the host has made an explicit retry decision.

Platform contracts: [DevicePolicyManager reboot/lock](https://developer.android.com/reference/android/app/admin/DevicePolicyManager). See [testing](testing.md) and [physical checks](physical-device-tests.md) for evidence boundaries.
