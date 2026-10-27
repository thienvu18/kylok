// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go"
	"github.com/thienvu18/kylok/go/ports"
)

func TestHostProjectionHandoffRestartAndScope(t *testing.T) {
	now := time.Now().UTC()
	sdk, options := setup(t, func() time.Time { return now })
	client, server := start(t, sdk)
	id, _ := enrolled(t, sdk, client, "site")
	another, _ := enrolled(t, sdk, client, "site")
	first, err := sdk.ReadProjection(ctx, "site", "", 1)
	if err != nil || first.NextCursor == "" || len(first.Items) != 1 {
		t.Fatal(first, err)
	}
	if _, err = sdk.ReadProjection(ctx, "other", first.NextCursor, 1); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("foreign cursor accepted", err)
	}
	if _, err = sdk.ListChanges(ctx, "other", first.ChangesCursor, 1); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("foreign checkpoint accepted", err)
	}
	if _, err = sdk.ListChanges(ctx, "site", first.NextCursor, 1); !errors.Is(err, kylok.ErrInvalidArgument) {
		t.Fatal("mixed cursor kind", err)
	}
	if err = sdk.RevokeDevice(ctx, "site", another); err != nil {
		t.Fatal(err)
	}
	server.Close()
	sdk.Close()
	reopened, err := kylok.New(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	next, err := reopened.ReadProjection(ctx, "site", first.NextCursor, 1)
	if err != nil || next.NextCursor != "" || next.ChangesCursor != first.ChangesCursor {
		t.Fatal(next, err)
	}
	old := []kylok.ProjectionItem{first.Items[0], next.Items[0]}
	for _, item := range old {
		if item.Device == nil || item.Device.Device.State == "REVOKED" {
			t.Fatal("scan changed after creation", old)
		}
	}
	changes, err := reopened.ListChanges(ctx, "site", next.ChangesCursor, 100)
	if err != nil || len(changes.Items) != 1 || changes.Items[0].ResourceID != another {
		t.Fatal(changes, err)
	}
	// A restarted consumer can re-read its last checkpoint and idempotently replace the same aggregate.
	replay, err := reopened.ListChanges(ctx, "site", next.ChangesCursor, 100)
	if err != nil || replay.Cursor != changes.Cursor {
		t.Fatal(replay, err)
	}
	state, err := reopened.GetDeviceState(ctx, "site", changes.Items[0].ResourceID)
	if err != nil || state.Device.State != "REVOKED" {
		t.Fatal(state, err)
	}
	if _, err = reopened.GetDeviceState(ctx, "other", id); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal(err)
	}
	data, _ := json.Marshal(state)
	for _, secret := range []string{"credential_hash", "lease_id", "sync_hash", "token_hash"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("secret in projection", secret)
		}
	}
	now = now.Add(ports.SnapshotLifetime)
	if _, err = reopened.ReadProjection(ctx, "site", first.NextCursor, 100); !errors.Is(err, kylok.ErrCursorExpired) {
		t.Fatal("expired snapshot accepted", err)
	}
}

func TestMalformedFeedCursorsRejectMissingRequiredFields(t *testing.T) {
	sdk, _ := setup(t, nil)
	for _, payload := range []string{`{}`, `{"q":0}`, `{"v":1,"s":"site","q":0}`, `{"v":1,"s":"other","k":"changes","q":0}`, `{"v":2,"s":"site","k":"changes","q":0}`, `{"v":1,"s":"site","k":"changes","q":0,"unknown":true}`} {
		cursor := base64.RawURLEncoding.EncodeToString([]byte(payload))
		if _, err := sdk.ListChanges(ctx, "site", cursor, 20); !errors.Is(err, kylok.ErrInvalidArgument) {
			t.Fatal(payload, err)
		}
	}
}
