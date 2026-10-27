// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"time"

	"github.com/thienvu18/kylok/go/ports"
)

var ErrCursorExpired = ports.ErrCursorExpired

// Change is a durable scoped invalidation or application tombstone, without tokens or raw state.
// Refresh device_changed through GetDeviceState; application_changed through GetApplication.
// application_removed deletes that digest from the host projection. Sequence is strictly ascending per scope.
type Change struct {
	Sequence   uint64
	Kind       string
	ResourceID string
}

// ChangePage returns a checkpoint even when no records exist. Persist it with host projection updates;
// failures may replay a batch and must be handled idempotently. Changes are retained for 10,000 records per scope.
type ChangePage struct {
	Items  []Change
	Cursor string
	More   bool
}

// DesiredApplication is a host-facing verified desired package and revision.
type DesiredApplication struct {
	Artifact Artifact `json:"artifact"`
	Revision uint64   `json:"revision"`
}

// DeviceState is a consistent scoped projection, including complete operation history and desired/observed policy.
// Reading does not expire operations or trigger effects; deadline-aware reads use GetOperation/QueryOperations.
type DeviceState struct {
	Device     Device               `json:"device"`
	Desired    []DesiredApplication `json:"desired"`
	Operations []Operation          `json:"operations"`
	Policy     PolicyStatus         `json:"policy"`
}

// ProjectionItem is one snapshot resource: exactly one pointer is populated.
type ProjectionItem struct {
	Device      *DeviceState `json:"device,omitempty"`
	Application *Artifact    `json:"application,omitempty"`
}

// ProjectionPage contains immutable scoped records. Build a staged projection across all pages,
// atomically replace the old projection, then use ChangesCursor after the final page.
// NextCursor expires after fifteen minutes or when this scope starts four newer scans.
type ProjectionPage struct {
	Items         []ProjectionItem
	NextCursor    string
	ChangesCursor string
	ExpiresAt     time.Time
}

type feedCursor struct {
	Version  int    `json:"v"`
	Scope    string `json:"s"`
	Kind     string `json:"k"`
	Sequence uint64 `json:"q"`
	Snapshot string `json:"n,omitempty"`
	After    int    `json:"a,omitempty"`
}

// Encode a scope-bound projection/change checkpoint; it never grants authorization to its holder.
func encodeFeedCursor(cursor feedCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

// Strictly decode bounded cursor state, rejecting a different scope/kind and malformed snapshot continuations.
func decodeFeedCursor(value, scope, kind string) (feedCursor, error) {
	result := feedCursor{Version: 1, Scope: scope, Kind: kind}
	if value == "" {
		return result, nil
	}
	if len(value) > maximumCursorBytes {
		return result, ErrInvalidArgument
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return result, ErrInvalidArgument
	}
	result = feedCursor{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return feedCursor{}, ErrInvalidArgument
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || result.Version != 1 || result.Scope != scope || result.Kind != kind || result.After < 0 || result.Sequence > 1<<63-1 {
		return feedCursor{}, ErrInvalidArgument
	}
	if kind == "changes" && (result.Snapshot != "" || result.After != 0) || kind == "projection" && (!devicePattern.MatchString(result.Snapshot) || result.Sequence != 0) {
		return feedCursor{}, ErrInvalidArgument
	}
	return result, nil
}

// ListChanges reads a replayable bounded scope log. Empty cursor starts at sequence zero;
// ErrCursorExpired requires a consistent ReadProjection, never silently skipping missed changes.
// Hosts authorize the scope on every call; callbacks are only optional wakeups, not durable delivery.
// Usage: page, err := sdk.ListChanges(ctx, scope, savedCursor, 100).
func (s *SDK) ListChanges(ctx context.Context, scope, cursor string, limit int) (ChangePage, error) {
	if err := s.checkOpen(ctx); err != nil {
		return ChangePage{}, err
	}
	if !idPattern.MatchString(scope) || limit < 1 || limit > maximumPageSize {
		return ChangePage{}, ErrInvalidArgument
	}
	checkpoint, err := decodeFeedCursor(cursor, scope, "changes")
	if err != nil {
		return ChangePage{}, err
	}
	page, err := s.store.ListChanges(ctx, scope, checkpoint.Sequence, limit)
	if err != nil {
		return ChangePage{}, err
	}
	result := ChangePage{Items: []Change{}, More: page.Next < page.Head}
	for _, item := range page.Items {
		result.Items = append(result.Items, Change{Sequence: item.Sequence, Kind: item.Kind, ResourceID: item.ResourceID})
	}
	result.Cursor = encodeFeedCursor(feedCursor{Version: 1, Scope: scope, Kind: "changes", Sequence: page.Next})
	return result, nil
}

// GetDeviceState reads a transactionally consistent scoped aggregate without exposing agent lease tokens.
// Usage: state, err := sdk.GetDeviceState(ctx, scope, deviceID).
func (s *SDK) GetDeviceState(ctx context.Context, scope, id string) (DeviceState, error) {
	if err := s.checkOpen(ctx); err != nil {
		return DeviceState{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(id) {
		return DeviceState{}, ErrInvalidArgument
	}
	value, err := s.store.GetDeviceState(ctx, scope, id)
	if err != nil {
		return DeviceState{}, err
	}
	return publicDeviceState(value), nil
}

// GetApplication reads one scoped verified catalog entry; absent/foreign digests both return ErrNotFound.
// Usage: artifact, err := sdk.GetApplication(ctx, scope, digest).
func (s *SDK) GetApplication(ctx context.Context, scope, digest string) (Artifact, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Artifact{}, err
	}
	if !idPattern.MatchString(scope) || !digestPattern.MatchString(digest) {
		return Artifact{}, ErrInvalidArgument
	}
	value, err := s.store.Artifact(ctx, scope, digest)
	if err != nil {
		return Artifact{}, err
	}
	return publicArtifact(value), nil
}

// ReadProjection starts or continues an immutable scoped snapshot and matching change watermark.
// Cursor "" starts a new scan. Complete all pages before committing ChangesCursor; expiry/eviction
// returns ErrCursorExpired and requires discarding the staged scan. Never combine pages from different scans.
// Usage: page, err := sdk.ReadProjection(ctx, scope, "", 100).
func (s *SDK) ReadProjection(ctx context.Context, scope, cursor string, limit int) (ProjectionPage, error) {
	if err := s.checkOpen(ctx); err != nil {
		return ProjectionPage{}, err
	}
	if !idPattern.MatchString(scope) || limit < 1 || limit > maximumPageSize {
		return ProjectionPage{}, ErrInvalidArgument
	}
	checkpoint, err := decodeFeedCursor(cursor, scope, "projection")
	if err != nil {
		return ProjectionPage{}, err
	}
	page, err := s.store.ReadProjection(ctx, scope, checkpoint.Snapshot, checkpoint.After, limit, s.clock().UTC())
	if err != nil {
		return ProjectionPage{}, err
	}
	result := ProjectionPage{Items: []ProjectionItem{}, ExpiresAt: page.ExpiresAt, ChangesCursor: encodeFeedCursor(feedCursor{Version: 1, Scope: scope, Kind: "changes", Sequence: page.Watermark})}
	for _, item := range page.Items {
		value := ProjectionItem{}
		if item.Device != nil {
			device := publicDeviceState(*item.Device)
			value.Device = &device
		}
		if item.Application != nil {
			artifact := publicArtifact(*item.Application)
			value.Application = &artifact
		}
		result.Items = append(result.Items, value)
	}
	if page.More {
		result.NextCursor = encodeFeedCursor(feedCursor{Version: 1, Scope: scope, Kind: "projection", Snapshot: page.SnapshotID, After: page.After})
	}
	return result, nil
}

// Project detached aggregate values, stripping installer authorization/checkpoints from operation records.
func publicDeviceState(value ports.DeviceState) DeviceState {
	result := DeviceState{Device: publicDevice(value.Device), Operations: publicOperations(value.Operations), Policy: value.Policy, Desired: []DesiredApplication{}}
	for _, desired := range value.Desired {
		result.Desired = append(result.Desired, DesiredApplication{Artifact: publicArtifact(desired.Artifact), Revision: desired.Revision})
	}
	return result
}
