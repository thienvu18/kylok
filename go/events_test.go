// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/thienvu18/kylok/go"
	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/internal/testapk"
)

func eventSDK(t *testing.T, sink kylok.EventSink, timeout time.Duration) *kylok.SDK {
	t.Helper()
	s, err := kylok.New(ctx, kylok.Options{PublicURL: "https://events.test", Bootstrap: testapk.Make(t, testapk.Manifest), DataDir: t.TempDir(), Events: sink, EventTimeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestEventsAreAfterCommitSanitizedAndRetryIdentified(t *testing.T) {
	events := make(chan kylok.Event, 8)
	s := eventSDK(t, func(_ context.Context, e kylok.Event) error { events <- e; return errors.New("host queue unavailable") }, time.Second)
	client, _ := start(t, s)
	r, _ := request(t, issue(t, s, "site").Token)
	first, err := client.Enroll(ctx, connect.NewRequest(r))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Enroll(ctx, connect.NewRequest(r)); err != nil {
		t.Fatal(err)
	}
	var previous string
	for range 2 {
		select {
		case e := <-events:
			if e.Kind != "device_enrolled" || e.DeviceID != first.Msg.Device.Id || e.ScopeID != "site" {
				t.Fatal(e)
			}
			if strings.Contains(e.ID, r.Token) {
				t.Fatal("secret in event")
			}
			if previous != "" && e.ID != previous {
				t.Fatal("retry changed event identity")
			}
			previous = e.ID
			devices, err := s.ListDevices(ctx, "site", "", 10)
			if err != nil || len(devices) != 1 {
				t.Fatal("event preceded commit", devices, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("event not delivered")
		}
	}
	if err = s.RevokeDevice(ctx, "site", first.Msg.Device.Id); err != nil {
		t.Fatal(err)
	}
	_, err = client.Enroll(ctx, connect.NewRequest(r))
	reason(t, err, v1.ErrorReason_ERROR_REASON_DEVICE_REVOKED)
}
func TestPanickingEventSinkCannotBreakAuthorizationOrManagement(t *testing.T) {
	var calls atomic.Int32
	s := eventSDK(t, func(_ context.Context, _ kylok.Event) error { calls.Add(1); panic("host observer") }, time.Second)
	client, _ := start(t, s)
	id, credential := enrolled(t, s, client, "site")
	exchange(t, client, id, credential, 1, 0)
	if err := s.RevokeDevice(ctx, "site", id); err != nil {
		t.Fatal(err)
	}
	_, err := client.Sync(ctx, syncRequest(id, credential))
	reason(t, err, v1.ErrorReason_ERROR_REASON_DEVICE_REVOKED)
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() < 3 {
		t.Fatal("dispatcher stopped after host panic")
	}
}
func TestBlockedEventSinkDoesNotBlockRPCAndCloseCancelsIt(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	s := eventSDK(t, func(c context.Context, _ kylok.Event) error {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-c.Done()
		return c.Err()
	}, 20*time.Millisecond)
	client, _ := start(t, s)
	id, credential := enrolled(t, s, client, "site")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("observer not started")
	}
	exchange(t, client, id, credential, 1, 0)
	start := time.Now()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("sink blocked Close")
	}
}
