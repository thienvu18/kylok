# Host integration

The Go SDK exposes domain models through direct methods; the host authorizes every management call. Neither a device credential, correlation ID nor cursor grants management access. Resolve business users/tenants into explicit authorized scope IDs. Never translate an all-tenant filter into an empty scope.

## Read and associate

```go
ticket, err := sdk.CreateEnrollmentWithOptions(ctx, kylok.EnrollmentOptions{
    ScopeID: scope, TTL: 15*time.Minute, CorrelationID: intentID,
})
// Handle err; present the private token/QR once through an authorized flow.
page, err := sdk.QueryDevices(ctx, scope, kylok.DeviceFilter{CorrelationID: intentID}, kylok.PageRequest{Limit: 20})
device, err := sdk.GetDevice(ctx, scope, deviceID)
operation, err := sdk.GetOperation(ctx, scope, deviceID, operationID)
```

Correlation IDs are optional opaque identifiers (1–128 ASCII identifier characters), unique per scope while the enrollment record exists, including after expiry/consumption. Creation never stores a recoverable token: duplicate/conflicting creation returns `ErrConflict`. If the creation response is lost, use a new correlation ID and let the unused token expire. Device enrollment retries reuse their attempt/key and preserve the original identity/correlation atomically. Keep the host's asset/site/user records in a host-owned mapping, keyed by scope/device ID.

`QueryDevices` and `QueryOperations` return typed pages and exact optional filters. Page size defaults to 20, maximum 100; cursors bind scope, device and filters. They are live keyset reads: concurrent inserts behind a cursor or changing filters require a new scan. `ErrInvalidArgument`, `ErrNotFound`, `ErrConflict`, `ErrExpired`, `ErrReferenced`, `ErrRevoked`, `ErrUnauthorized`, `ErrStale`, `ErrConsumed`, `ErrRecoveryExpired`, `ErrClosed` and `context` errors support `errors.Is`. Point reads hide foreign and absent resources behind the same not-found result. Operation results omit installer leases/checkpoints; desired, observed and compliance state remain distinct. Use `GetDeviceState` for a consistent aggregate, rather than combining independent reads into a purported snapshot.

The [hosting example](../examples/minimal-server/hosting/service.go) separates a consumer-owned application port/authorizer from its [SDK adapter](../examples/minimal-server/hosting/adapter.go). A business transaction and SDK effect are separate durable actions: persist an intent, use a stable operation/policy request key, and reconcile results after a crash.

## Mount, observe and close

`PublicURL` and `BootstrapURL` are origins: HTTP/HTTPS, IP/domain and optional port, with no path prefix, user information, query or fragment. `sdk.Handler()` serves fixed root paths `/kylok.v1.AgentService/`, `/bootstrap/` and `/artifacts/`. Mount those paths alongside host business routes or use the whole handler on a dedicated listener. Arbitrary prefixes and `StripPrefix` do not retarget QR/RPC/download paths. `BootstrapHandler()` may be mounted on its configured separate origin.

Enable HTTP/2; plain HTTP additionally requires h2c. HTTPS checks `r.TLS`; forwarded headers do not satisfy it. Terminate TLS in the host listener, or re-encrypt the proxy-to-host hop. An HTTP backend advertised as HTTPS is rejected; no forwarded-header trust or automatic downgrade is provided.

Hosts own readiness endpoints. `sdk.Ready(ctx)` checks persistence/bootstrap availability at that moment and returns `ErrClosed` after shutdown. Drain all host requests, call `sdk.Close()`, then close supplied providers/pools. The SDK cancels its optional event worker and closes only its own default database. Optional event failures cannot change results; use the durable feed below for projection recovery.

## PostgreSQL

```go
store, err := postgres.Open(ctx, db, postgres.Options{Schema: "kylok", Initialize: true})
// Handle err. Initialize once using a principal allowed to create the dedicated schema/tables.
// Runtime starts use Initialize: false; missing/incomplete/wrong-version schemas fail closed.
sdk, err := kylok.New(ctx, kylok.Options{
    PublicURL: origin, Bootstrap: apk, Store: store, Artifacts: artifactStore,
})
```

The host supplies a PostgreSQL `database/sql` driver/pool, credentials, TLS and connection limits. The adapter never changes the pool or `search_path`, never joins ambient host transactions and never closes the pool. Schema names are lowercase SQL identifiers of at most 63 characters (default `kylok`). Initialization requires CREATE privileges; runtime needs schema USAGE plus SELECT/INSERT/UPDATE/DELETE on its tables. Grant those explicitly after initialization. Multiple instances must share the same schema and immutable artifact storage; filesystem defaults are not shared across separate hosts.

State transactions use PostgreSQL read committed plus a transaction advisory lock per schema before state reads, matching SQLite's serialized writer semantics. Commit, rollback, cancellation or connection loss releases the lock. This deliberately serializes control-plane work; it is not a horizontal-write throughput guarantee. No automatic retry occurs after an ambiguous commit: retry enrollment/sync/assignments/policies using their original attempt/sequence/key. Hosts retry transient connection failures with bounded backoff, then reconcile authoritative reads. Scale qualification remains separate.

`make test-postgres` owns a disposable PostgreSQL 18.6 container pinned by digest. It runs shared conformance, concurrent instances, connection termination and server crash/restart tests. Never run these destructive tests against an existing database; the script creates/removes only its own fixture and initializes the current schema.

## Durable changes and resnapshot

`ListChanges(ctx, scope, cursor, limit)` returns scoped `device_changed`, `application_changed` and `application_removed` records with increasing per-scope sequence. Records commit with the corresponding effects, and replayed enrollment/sync/assignment/policy requests do not create another logical change. The log retains the latest 10,000 records per scope, not permanent audit history. Device changes invalidate the complete aggregate; fetch `GetDeviceState`. Catalog changes/tombstones require `GetApplication` to resolve current existence, including removal followed by re-registration.

Start with `ReadProjection(ctx, scope, "", 100)`. Stage all immutable pages, following `NextCursor`. After the final page, atomically replace the host's scoped projection and store `ChangesCursor`. Snapshot records include device desired/observed state, operation history, policies and catalog entries. The watermark and records come from one transaction, survive server restarts and remain unchanged during concurrent writes. Scans expire after fifteen minutes; at most four scans per scope are retained. Expired/evicted scans return `ErrCursorExpired`; discard staged pages and restart. Large scopes materialize complete aggregate records and need deployment-specific scale qualification.

Then fetch changes from the committed checkpoint, refresh each distinct resource once, and commit replacements/tombstones plus the new checkpoint in one host transaction. Failed commits replay the batch; replacements must be idempotent. `ErrCursorExpired` on the change feed requires a new full snapshot. Feed cursors do not override authorization. Export permanent audit data to host storage before retention expires. Read-only `GetDeviceState`/snapshot reads do not expire operations; deadline-aware operation reads may persist expiry and its corresponding change.

The [projection adapter](../examples/minimal-server/hosting/projection.go) demonstrates this loop with a host-owned atomic projection sink. Optional callbacks can wake it, but losing every callback must not lose state.
