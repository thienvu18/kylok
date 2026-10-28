# API reference

Use the API reference to find the Go and Android operations your product can call:

- [Go server SDK](../api/go.html)
- [Android SDK and device status](../api/android/index.html)

If you are adding Kylok to a product, start with the [integration guide](install.html) for package setup, host responsibilities and a working usage path.

## Manage devices from your Go service

| Goal | API |
| --- | --- |
| Start and stop the service | `New`, `Close`, `Handler`, `BootstrapHandler`, `Version` |
| Enroll a device | `CreateEnrollment`, `QR`, `QRWithOrigins` |
| Review or revoke devices | `ListDevices`, `RevokeDevice` |
| Deliver and update apps | `RegisterApplication`, `ListApplications`, `AssignApplication`, `ListOperations`, `RemoveApplication` |
| Set and inspect policies | `SetPolicy`, `GetPolicy` |
| Use custom storage or events | `ports.StateStore`, `ArtifactStore`, `Options.Events`, `Clock` |

## Check device status from your Android app

| Goal | API |
| --- | --- |
| Schedule a sync | `Kylok.retry(context)` |
| Read the latest device state | `Kylok.snapshot(context) { AgentStatus -> … }` |
| Receive status updates | `Kylok.observe(StatusListener { … })`; close the registration with your app lifecycle |
| Open Android's install confirmation | `Kylok.confirmPendingInstall(context)` after a user action |
| Show SDK/device diagnostics | `Kylok.status`, `sdkVersion`, `protocolVersion` |

`AgentStatus` tells your app which app versions are requested and which are installed, the outcome of each policy rule, and any pending install progress. It does not expose enrollment tokens, credentials or private keys.

Current source also documents scoped reads/correlation, PostgreSQL, durable changes and `HostOptions`/typed managed configuration. See [host integration](https://github.com/thienvu18/kylok/blob/main/docs/host-integration.md) and [Android configuration](https://github.com/thienvu18/kylok/blob/main/docs/managed-configuration.md); generated references below follow current source.

Fleet APIs include `ReplaceGroup`, `CreateFleetAssignment`, `AdvanceFleetAssignment`, `ScheduleApplication`, cancellation and scoped pages. One-shot actions use `SubmitCommand`, `GetCommand`, `QueryCommands` and `CancelCommand`. Android `AgentStatus.commands` reports local progress without dispatch tokens. See the [fleet contract](https://github.com/thienvu18/kylok/blob/main/docs/fleet-operations.md).

Managed connectivity uses `SetNetworkPolicy`, `GetNetworkPolicy` and optional host-owned `Options.NetworkSecrets`. Android `AgentStatus.network` contains sanitized observation flags. See the [network lifecycle contract](https://github.com/thienvu18/kylok/blob/main/docs/managed-network.md).
