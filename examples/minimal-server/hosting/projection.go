// SPDX-License-Identifier: LGPL-3.0-only
package hosting

import (
	"context"
	"errors"
	"sort"

	"github.com/thienvu18/kylok/go"
)

// ProjectionSink is a host-owned durable projection boundary. Apply and Replace must atomically
// update records and checkpoint, so a crash can only replay a batch, never skip its state.
// Calls are serialized per scope by the host; records are replacements keyed by device ID/digest.
type ProjectionSink interface {
	// Checkpoint returns the last committed scope cursor, or empty when the scope needs a full snapshot.
	// Usage: cursor, err := sink.Checkpoint(ctx, scope).
	Checkpoint(context.Context, string) (string, error)
	// Replace atomically swaps a complete staged scope projection and its change checkpoint.
	// Usage: err := sink.Replace(ctx, scope, snapshotItems, cursor).
	Replace(context.Context, string, []kylok.ProjectionItem, string) error
	// Apply atomically replaces refreshed records, removes catalog tombstones and advances the checkpoint.
	// Usage: err := sink.Apply(ctx, scope, items, removedDigests, cursor).
	Apply(context.Context, string, []kylok.ProjectionItem, []string, string) error
}

// Projector is a host adapter: the caller authorizes scope and serializes workers before calling RunOnce.
// A callback may wake a worker; correctness depends only on persisted snapshots/changes, never notifications.
type Projector struct {
	SDK  *kylok.SDK
	Sink ProjectionSink
}

// RunOnce applies one durable change batch, or a complete resnapshot when absent/expired.
// Duplicate batches safely replace the same keys. A host retry after a failed sink commit reuses its checkpoint.
// Usage: err := (Projector{SDK: sdk, Sink: hostProjection}).RunOnce(ctx, authorizedScope).
func (p Projector) RunOnce(ctx context.Context, scope string) error {
	cursor, err := p.Sink.Checkpoint(ctx, scope)
	if err != nil {
		return err
	}
	if cursor == "" {
		return p.resnapshot(ctx, scope)
	}
	changes, err := p.SDK.ListChanges(ctx, scope, cursor, 100)
	if errors.Is(err, kylok.ErrCursorExpired) {
		return p.resnapshot(ctx, scope)
	} else if err != nil {
		return err
	}

	items := []kylok.ProjectionItem{}
	removed := []string{}
	// Resolve each resource once per batch; separate replacement/removal lists must never contain the same key.
	resources := map[string]kylok.Change{}
	for _, change := range changes.Items {
		kind := change.Kind
		if kind == "application_removed" {
			kind = "application_changed"
		}
		if kind != "application_changed" && kind != "device_changed" {
			return errors.New("unknown change kind; checkpoint retained")
		}
		resources[kind+":"+change.ResourceID] = change
	}
	keys := make([]string, 0, len(resources))
	for key := range resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		change := resources[key]
		if change.Kind == "device_changed" {
			state, err := p.SDK.GetDeviceState(ctx, scope, change.ResourceID)
			if err != nil {
				return err
			}
			items = append(items, kylok.ProjectionItem{Device: &state})
		} else {
			artifact, err := p.SDK.GetApplication(ctx, scope, change.ResourceID)
			if errors.Is(err, kylok.ErrNotFound) {
				removed = append(removed, change.ResourceID)
			} else if err != nil {
				return err
			} else {
				items = append(items, kylok.ProjectionItem{Application: &artifact})
			}
		}
	}

	return p.Sink.Apply(ctx, scope, items, removed, changes.Cursor)
}

// Stage an entire immutable scan before replacing the host's projection and watermark in one transaction.
func (p Projector) resnapshot(ctx context.Context, scope string) error {
	cursor := ""
	items := []kylok.ProjectionItem{}
	for {
		page, err := p.SDK.ReadProjection(ctx, scope, cursor, 100)
		if err != nil {
			return err
		}
		items = append(items, page.Items...)
		if page.NextCursor == "" {
			return p.Sink.Replace(ctx, scope, items, page.ChangesCursor)
		}
		cursor = page.NextCursor
	}
}
