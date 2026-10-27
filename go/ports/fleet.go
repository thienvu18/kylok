// SPDX-License-Identifier: LGPL-3.0-only
package ports

import (
	"context"
	"time"
)

// MaximumFleetDevices bounds membership snapshots and per-request partial results.
const MaximumFleetDevices = 100

// Group is a scoped, opaque membership set. Revision is a compare-and-swap token.
// Groups select targets at submission; membership edits do not alter an existing fleet assignment.
type Group struct {
	ScopeID   string   `json:"scope_id"`
	ID        string   `json:"id"`
	Revision  uint64   `json:"revision"`
	DeviceIDs []string `json:"device_ids"`
}

// FleetRequest binds an immutable absolute deployment window and exactly one target selector.
// ID is a scope-local idempotency key. DeviceIDs are sorted and unique; GroupID freezes its members.
type FleetRequest struct {
	ScopeID        string    `json:"scope_id"`
	ID             string    `json:"id"`
	GroupID        string    `json:"group_id,omitempty"`
	DeviceIDs      []string  `json:"device_ids,omitempty"`
	ArtifactSHA256 string    `json:"artifact_sha256"`
	NotBefore      time.Time `json:"not_before"`
	Deadline       time.Time `json:"deadline"`
}

// FleetItem records fan-out progress, not device compliance. ASSIGNED has a durable OperationID;
// FAILED is a permanent per-device rejection; PENDING can be resumed; CANCELLED was not submitted.
// Read the device operation to observe installation outcome after ASSIGNED.
type FleetItem struct {
	DeviceID    string `json:"device_id"`
	State       string `json:"state"`
	OperationID string `json:"operation_id,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
}

// FleetAssignment persists the frozen target list and bounded per-device results.
// Complete means fan-out has no pending items; it does not mean APKs were installed.
type FleetAssignment struct {
	Request       FleetRequest `json:"request"`
	GroupRevision uint64       `json:"group_revision,omitempty"`
	Items         []FleetItem  `json:"items"`
	Complete      bool         `json:"complete"`
	Cancelled     bool         `json:"cancelled"`
}

// FleetStore extends the same serialized state boundary used by assignments and sync.
// Every mutation records scoped change-feed invalidations in its transaction.
type FleetStore interface {
	// ReplaceGroup atomically replaces membership if expectedRevision matches (zero creates).
	// An identical retry at expectedRevision+1 returns the committed group; foreign devices are rejected.
	// Usage: group, err := store.ReplaceGroup(ctx, Group{ScopeID: scope, ID: id, DeviceIDs: ids}, expectedRevision).
	ReplaceGroup(context.Context, Group, uint64) (Group, error)
	// GetGroup returns detached membership in exactly one authorized scope.
	// Usage: group, err := store.GetGroup(ctx, scope, groupID).
	GetGroup(context.Context, string, string) (Group, error)
	// ListGroups returns live ascending-ID pages, including empty groups.
	// Usage: groups, err := store.ListGroups(ctx, scope, afterID, 20).
	ListGroups(context.Context, string, string, int) ([]Group, error)
	// CreateFleetAssignment atomically freezes targets and binds the request; no device effect occurs here.
	// Usage: batch, err := store.CreateFleetAssignment(ctx, request, now).
	CreateFleetAssignment(context.Context, FleetRequest, time.Time) (FleetAssignment, error)
	// GetFleetAssignment returns the exact frozen request and fan-out checkpoint.
	// Usage: batch, err := store.GetFleetAssignment(ctx, scope, batchID).
	GetFleetAssignment(context.Context, string, string) (FleetAssignment, error)
	// ListFleetAssignments pages durable batches so a restarted host can resume incomplete fan-out.
	// Usage: batches, err := store.ListFleetAssignments(ctx, scope, afterID, 20).
	ListFleetAssignments(context.Context, string, string, int) ([]FleetAssignment, error)
	// AdvanceFleetAssignment atomically submits at most limit pending devices, with per-device stable keys.
	// Permanent domain rejections become item failures; storage/context errors roll back this advance.
	// Usage: batch, err := store.AdvanceFleetAssignment(ctx, scope, batchID, now, 20).
	AdvanceFleetAssignment(context.Context, string, string, time.Time, int) (FleetAssignment, error)
	// CancelFleetAssignment stops pending fan-out and cancels submitted active work, preserving completed effects.
	// Usage: batch, err := store.CancelFleetAssignment(ctx, scope, batchID, now).
	CancelFleetAssignment(context.Context, string, string, time.Time) (FleetAssignment, error)
	// CancelOperation stops future dispatch/download; an already delivered platform effect may still finish.
	// Desired assignment stays recorded; no implicit rollback/uninstall or future drift retry is created.
	// Usage: operation, err := store.CancelOperation(ctx, scope, deviceID, operationID, now).
	CancelOperation(context.Context, string, string, string, time.Time) (Operation, error)
}
