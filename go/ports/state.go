// SPDX-License-Identifier: LGPL-3.0-only
// Package ports defines persistence contracts shared by default and custom adapters.
package ports

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrNotFound        = errors.New("not found")
	ErrExpired         = errors.New("enrollment expired")
	ErrConsumed        = errors.New("enrollment consumed by another attempt")
	ErrRecoveryExpired = errors.New("enrollment recovery expired")
	ErrRevoked         = errors.New("device revoked")
	ErrUnauthorized    = errors.New("invalid device credential")
	ErrConflict        = errors.New("conflicting idempotency payload")
	ErrUnsupported     = errors.New("device capability unsupported")
	ErrStale           = errors.New("stale sync sequence")
	ErrReferenced      = errors.New("artifact is referenced by desired state or pending work")
)

type Inventory struct {
	APILevel         uint32                 `json:"api_level"`
	Model            string                 `json:"model"`
	BuildFingerprint string                 `json:"build_fingerprint"`
	AgentVersion     string                 `json:"agent_version"`
	DeviceOwner      bool                   `json:"device_owner"`
	Capabilities     []string               `json:"capabilities"`
	Applications     []InstalledApplication `json:"applications"`
	SupportedABIs    []string               `json:"supported_abis"`
}
type InstalledApplication struct {
	PackageName         string   `json:"package_name"`
	VersionCode         int64    `json:"version_code"`
	SigningCertificates []string `json:"signing_certificates"`
}
type Device struct {
	CorrelationID    string    `json:"correlation_id,omitempty"`
	ID               string    `json:"id"`
	ScopeID          string    `json:"scope_id"`
	State            string    `json:"state"`
	Inventory        Inventory `json:"inventory"`
	CreatedAt        time.Time `json:"created_at"`
	LastSeenAt       time.Time `json:"last_seen_at"`
	DesiredRevision  uint64    `json:"desired_revision"`
	ReportedRevision uint64    `json:"reported_revision"`
}
type Enrollment struct {
	CorrelationID  string
	TokenHash      []byte
	ScopeID        string
	ExpiresAt      time.Time
	ArtifactSHA256 string
}
type Attempt struct {
	TokenHash      []byte
	ID             string
	KeyHash        []byte
	Device         Device
	CredentialHash []byte
	// Encoded encrypted response; never contains a plaintext credential.
	Receipt      []byte
	Now          time.Time
	RecoverUntil time.Time
}

// StateStore methods are safe for concurrent calls. Every operation respects context
// cancellation. Consume atomically validates expiry, consumes the token, inserts the
// device and persists its encrypted receipt. A matching ID AND key hash retrieves
// the original receipt until RecoverUntil, including after token expiry. Other
// attempts fail; revoked devices cannot recover. Revocation and Sync are serialized.
// Returned slices/values belong to the caller. Hosts close custom stores themselves.
// SetPolicy atomically validates scope/required assignments, binds the normalized
// document to the device/key, and advances the shared desired revision once.
// Sync binds policy observations to its replay body and the desired snapshot;
// stale observations cannot establish current compliance. GetPolicy reads a
// consistent scoped desired/reported/compliance view. See portstest.Policies.
// DeviceQuery is a live keyset read. Mutable filters are evaluated at read time;
// inserts behind After need a new scan. CorrelationID is an exact opaque match.
type DeviceQuery struct {
	ScopeID, After, State, CorrelationID string
	Limit                                int
}

// OperationQuery filters a single authorized device's live operation history.
type OperationQuery struct {
	ScopeID, DeviceID, After, State string
	Limit                           int
	Now                             time.Time
}

type StateStore interface {
	ChangeStore
	FleetStore
	CommandStore
	NetworkStore
	// Check verifies that the persistence backend is reachable without changing state.
	// Usage: err := store.Check(ctx).
	Check(context.Context) error
	// GetDevice returns one scoped identity, never its credential verifier.
	// Usage: device, err := store.GetDevice(ctx, scope, deviceID).
	GetDevice(context.Context, string, string) (Device, error)
	// QueryDevices reads an exact scoped live page; callers own returned values.
	// Usage: devices, err := store.QueryDevices(ctx, DeviceQuery{ScopeID: scope, Limit: 20}).
	QueryDevices(context.Context, DeviceQuery) ([]Device, error)
	// GetOperation expires overdue work and reads one operation belonging to the scoped device.
	// Usage: operation, err := store.GetOperation(ctx, scope, deviceID, operationID, now).
	GetOperation(context.Context, string, string, string, time.Time) (Operation, error)
	// QueryOperations expires overdue work and reads a scoped, state-filtered live page.
	// Usage: operations, err := store.QueryOperations(ctx, OperationQuery{ScopeID: scope, DeviceID: deviceID, Limit: 20, Now: now}).
	QueryOperations(context.Context, OperationQuery) ([]Operation, error)
	// Persist a verifier-only enrollment bound to scope, expiry and bootstrap bytes.
	// Usage: err := store.CreateEnrollment(ctx, enrollment).
	CreateEnrollment(context.Context, Enrollment) error
	// Read the original enrollment binding using its token verifier.
	// Usage: enrollment, err := store.Enrollment(ctx, tokenHash).
	Enrollment(context.Context, []byte) (Enrollment, error)
	// Atomically claim a token or recover the same encrypted receipt for its bound attempt and key.
	// Usage: receipt, err := store.Consume(ctx, attempt).
	Consume(context.Context, Attempt) ([]byte, error)
	// Authenticate one device credential and reject revoked identities.
	// Usage: device, err := store.Authenticate(ctx, deviceID, credentialHash).
	Authenticate(context.Context, string, []byte) (Device, error)
	// Atomically bind replay input, accept reports and persist the returned desired/observed snapshot.
	// Usage: snapshot, err := store.Sync(ctx, input).
	Sync(context.Context, SyncInput) (Snapshot, error)
	// Read one authorized scope's stable device cursor page.
	// Usage: devices, err := store.ListDevices(ctx, scope, "", 100).
	ListDevices(context.Context, string, string, int) ([]Device, error)
	// Serialize revocation with enrollment recovery and authenticated sync.
	// Usage: err := store.Revoke(ctx, scope, deviceID).
	Revoke(context.Context, string, string) error
	// Register immutable verified metadata for a scope/digest.
	// Usage: err := store.RegisterArtifact(ctx, artifact).
	RegisterArtifact(context.Context, Artifact) error
	// ListArtifacts returns scoped managed metadata in ascending digest order.
	// Usage: page, err := store.ListArtifacts(ctx, scope, lastDigest, 100).
	ListArtifacts(context.Context, string, string, int) ([]Artifact, error)
	// Read one scope/digest without crossing tenant boundaries.
	// Usage: artifact, err := store.Artifact(ctx, scope, digest).
	Artifact(context.Context, string, string) (Artifact, error)
	// Remove only unreferenced scoped metadata, retaining shared content safely.
	// Usage: err := store.RemoveArtifact(ctx, scope, digest).
	RemoveArtifact(context.Context, string, string) error
	// Atomically bind an install request key, desired revision and supersession of pending work.
	// Usage: operation, err := store.Assign(ctx, assignment).
	Assign(context.Context, Assignment) (Operation, error)
	// Expire overdue work and page scoped durable progress/results.
	// Usage: operations, err := store.ListOperations(ctx, scope, deviceID, "", 100, now).
	ListOperations(context.Context, string, string, string, int, time.Time) ([]Operation, error)
	// Verify device credential, scope, active operation/lease and deadline before returning metadata.
	// Usage: artifact, err := store.AuthorizeDownload(ctx, deviceID, credentialHash, operationID, digest, leaseID, now).
	AuthorizeDownload(context.Context, string, []byte, string, string, string, time.Time) (Artifact, error)
	// Atomically bind a normalized replacement and validate every required app assignment.
	// Usage: desired, err := store.SetPolicy(ctx, assignment).
	SetPolicy(context.Context, PolicyAssignment) (DesiredPolicy, error)
	// Return consistent scoped desired/reported/compliance state.
	// Usage: status, err := store.GetPolicy(ctx, scope, deviceID).
	GetPolicy(context.Context, string, string) (PolicyStatus, error)
}

// Empty artifact scope is reserved for public bootstrap content; managed APKs
// always have an explicit scope. Store metadata is immutable for a scope/digest.
type Artifact struct {
	ScopeID             string   `json:"scope_id"`
	SHA256              string   `json:"sha256"`
	PackageName         string   `json:"package_name"`
	VersionCode         int64    `json:"version_code"`
	MinSDK              uint32   `json:"min_sdk"`
	MaxSDK              uint32   `json:"max_sdk"`
	SigningCertificates []string `json:"signing_certificates"`
	SupportedABIs       []string `json:"supported_abis"`
	SizeBytes           int64    `json:"size_bytes"`
}
type Assignment struct {
	ScopeID, DeviceID, ArtifactSHA256, IdempotencyKey, OperationID string
	Now, NotBefore, Deadline                                       time.Time
}
type DesiredApplication struct {
	Artifact Artifact `json:"artifact"`
	Revision uint64   `json:"revision"`
}
type Operation struct {
	ID              string                `json:"id"`
	DeviceID        string                `json:"device_id"`
	Artifact        Artifact              `json:"artifact"`
	Revision        uint64                `json:"revision"`
	State           string                `json:"state"`
	LeaseID         string                `json:"lease_id"`
	LeaseUntil      time.Time             `json:"lease_until"`
	Deadline        time.Time             `json:"deadline"`
	NotBefore       time.Time             `json:"not_before"`
	Attempts        int                   `json:"attempts"`
	ReportSequence  uint64                `json:"report_sequence"`
	DownloadedBytes int64                 `json:"downloaded_bytes"`
	ErrorCode       string                `json:"error_code"`
	ErrorMessage    string                `json:"error_message"`
	Observed        *InstalledApplication `json:"observed,omitempty"`
}
type OperationReport struct {
	ID, OperationID, LeaseID string
	Sequence                 uint64
	State                    string
	DownloadedBytes          int64
	ErrorCode, ErrorMessage  string
	Observed                 *InstalledApplication
}
type ReportReceipt struct {
	ReportID string `json:"report_id"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason"`
}
type ApplicationCompliance struct {
	PackageName string `json:"package_name"`
	Revision    uint64 `json:"revision"`
	State       string `json:"state"`
	Reason      string `json:"reason"`
}
type SyncInput struct {
	DeviceID             string
	CredentialHash       []byte
	Inventory            Inventory
	Sequence             uint64
	ReportedRevision     uint64
	RetainedOperationIDs []string
	CommandReports       []CommandReport
	Reports              []OperationReport
	NetworkReport        *NetworkReport
	PolicyReport         *PolicyReport
	Now                  time.Time
}
type Snapshot struct {
	Network             NetworkStatus           `json:"network"`
	Device              Device                  `json:"device"`
	Sequence            uint64                  `json:"sequence"`
	Desired             []DesiredApplication    `json:"desired"`
	Operations          []Operation             `json:"operations"`
	Commands            []Command               `json:"commands"`
	CommandReceipts     []string                `json:"command_receipts"`
	StoppedOperationIDs []string                `json:"stopped_operation_ids"`
	Receipts            []ReportReceipt         `json:"receipts"`
	Compliance          []ApplicationCompliance `json:"compliance"`
	DesiredPolicy       *DesiredPolicy          `json:"desired_policy,omitempty"`
	PolicyCompliance    []PolicyResult          `json:"policy_compliance"`
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// ArtifactStore puts complete content atomically by SHA-256 and must reject a
// digest mismatch or replacement with different bytes. Open returns an immutable
// snapshot. Only empty-scope bootstrap artifacts are publicly served; managed
// applications require authenticated device/operation/lease authorization.
type ArtifactStore interface {
	// Atomically publish complete immutable bytes and reject digest mismatches or conflicting replacements.
	// Usage: err := store.Put(ctx, digest, verifiedBytes).
	Put(context.Context, string, []byte) error
	// Return a detached seekable snapshot of immutable content; the caller closes the reader.
	// Usage: store, err := filesystem.Open("/var/lib/kylok/artifacts").
	Open(context.Context, string) (ReadSeekCloser, error)
}
