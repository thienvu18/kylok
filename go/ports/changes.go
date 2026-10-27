// SPDX-License-Identifier: LGPL-3.0-only
package ports

import (
	"context"
	"errors"
	"time"
)

// Changes are a bounded projection recovery log, never a permanent audit trail.
const ChangeRetention = 10000

// SnapshotLifetime bounds retained consistent snapshots; hosts restart expired scans.
const SnapshotLifetime = 15 * time.Minute

// SnapshotLimit bounds simultaneous scans per scope, with oldest scans evicted explicitly.
const SnapshotLimit = 4

var ErrCursorExpired = errors.New("cursor expired: resnapshot required")

// Change invalidates one scoped device aggregate or catalog entry. It carries no payload/secrets.
// Device changes include desired state, reports, operations, inventory and revocation.
// Application removals are tombstones; other changes require authoritative scoped reads.
type Change struct {
	Sequence   uint64 `json:"sequence"`
	Kind       string `json:"kind"`
	ResourceID string `json:"resource_id"`
}

// ChangesPage continues through a bounded scope log; Next always identifies the last delivered position.
type ChangesPage struct {
	Items []Change
	Next  uint64
	Head  uint64
}

// DeviceState is a consistent scoped aggregate for host projection, not an agent transport message.
type DeviceState struct {
	Device     Device               `json:"device"`
	Desired    []DesiredApplication `json:"desired"`
	Operations []Operation          `json:"operations"`
	Policy     PolicyStatus         `json:"policy"`
}

// ProjectionItem is one immutable snapshot record. Exactly one resource pointer is set.
type ProjectionItem struct {
	Device      *DeviceState `json:"device,omitempty"`
	Application *Artifact    `json:"application,omitempty"`
}

// ProjectionPage carries a snapshot continuation and the change watermark for after the final page.
type ProjectionPage struct {
	Items      []ProjectionItem
	SnapshotID string
	After      int
	More       bool
	Watermark  uint64
	ExpiresAt  time.Time
}

// ChangeStore is embedded in StateStore: state and invalidation records commit or roll back together.
type ChangeStore interface {
	// ListChanges reads only the explicit scope; stale cursors fail rather than silently skipping changes.
	// Usage: page, err := store.ListChanges(ctx, scope, lastSequence, 100).
	ListChanges(context.Context, string, uint64, int) (ChangesPage, error)
	// GetDeviceState atomically reads desired/observed state and complete operation history for one scoped device.
	// Usage: state, err := store.GetDeviceState(ctx, scope, deviceID).
	GetDeviceState(context.Context, string, string) (DeviceState, error)
	// ReadProjection starts a new consistent snapshot when snapshotID is empty; otherwise it reads saved immutable records.
	// Continuations require the same authorized scope, remain valid across process restart, and expire after SnapshotLifetime.
	// Usage: page, err := store.ReadProjection(ctx, scope, "", 0, 100, now).
	ReadProjection(context.Context, string, string, int, int, time.Time) (ProjectionPage, error)
}
