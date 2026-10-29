# Local messaging

Kylok's current durable sync and change feed remain authoritative for desired device/app state. Local messaging is a planned optional signal path for hosts that need to notify an agent or an authorized app while a device is connected to a private LAN.

## Recommended boundary

Keep a transport-neutral contract in the SDK and make the broker an optional host integration. The host owns broker lifecycle, persistence, network exposure, credentials and authorization. A device agent authenticates as one scoped device. An app receives only messages explicitly authorized for its verified package identity.

Messages identify a stable message ID, schema/type, target, creation/expiry time and the resource revision or event being signaled. Keep business payloads small and avoid secrets or business records in broker storage. The receiver deduplicates IDs, acknowledges its own processing boundary and fetches authoritative data through the host contract when needed.

An offline tablet with no route to the server cannot receive a new remote push. The host can retain an event within a bounded expiry and deliver it when LAN connectivity returns. Local communication between installed apps may work without the server, but requires an explicit on-device API and app participation.

## Transport choices

| Option | Strength | Cost |
| --- | --- | --- |
| MQTT 5.0 with a local broker | Standard publish/subscribe, device-specific subscriptions, persistent sessions, retained state and expiry. QoS 1 gives at-least-once delivery that can tolerate reconnects. | Adds a broker, topic ACLs, session/storage limits, client credentials and another service to operate. Duplicate application delivery remains possible, so consumers need stable IDs and idempotency. |
| WebSocket or gRPC stream | Reuses a host endpoint and can integrate with existing authenticated sessions and APIs. | The host must implement connection lifecycle, per-device queues, fan-out, acknowledgements and replay itself. A closed socket loses messages unless the host persists them. |
| WorkManager/network-return sync | Fits background repair and authoritative state reconciliation without a permanent socket. | Delivery is deferred and is not a low-latency push channel. It can be the fallback for every transport. |

MQTT 5.0 is the preferred first protocol candidate; it is not a runtime dependency of the current SDK. Use QoS 1 plus application idempotency for alerts/events. Retained messages should be limited to small current-state revision hints, not an audit log or user data. Bound session expiry and queue bytes/count; set message expiry so stale alerts are discarded. Use the normal sync/change feed to reconcile after every reconnect.

## Android delivery and lifecycle

A long-lived Android connection can be interrupted by process death, Doze, vendor battery management or network changes. Android documents that Doze restricts network access and defers WorkManager jobs; continuous delivery therefore needs device-specific evaluation and may require an appropriate foreground-service design. Do not promise immediate delivery while the tablet is idle or offline.

The agent-to-app bridge should use an explicit, package-targeted IPC contract with verified UID/package/signing identity, narrow message types, size limits, expiry and per-app authorization. Do not use unrestricted broadcasts or let a message invoke arbitrary device policy. Apps acknowledge their own message handling; Kylok cannot infer that a user saw or acted on a notification.

## Security and operation

Use TLS where the LAN threat model requires it, authenticate every client, bind device credentials to one device and scope broker ACLs to that identity. Separate broker credentials from enrollment tokens and Android app credentials. Prevent client-selected topic escalation. Apply quotas and expiry to limit disk growth and replay; log IDs and outcomes without storing message bodies unnecessarily.

Before selecting a broker/client library, evaluate maintenance, supported MQTT version, license compatibility, Android background behavior, TLS/private-CA setup, offline storage, reconnect behavior and measured battery/CPU use on declared hardware. Keep MQTT behind an adapter so a host may supply another transport without changing Kylok policy or application APIs.

References: [OASIS MQTT 5.0](https://docs.oasis-open.org/mqtt/mqtt/v5.0/mqtt-v5.0.html), [Android Doze and App Standby](https://developer.android.com/training/monitoring-device-state/doze-standby).
