// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"context"
	"fmt"
	"time"
)

// Event contains identifiers and committed revisions, never tokens, credentials,
// enrollment keys, QR contents or raw inventory. ID is stable across RPC retries.
type Event struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	ScopeID  string    `json:"scope_id"`
	DeviceID string    `json:"device_id"`
	Revision uint64    `json:"revision"`
	At       time.Time `json:"at"`
}

// EventSink is an optional best-effort notification hook. Calls are serialized on
// one SDK-owned worker and must honor cancellation. Errors/panics never change
// committed results. Delivery can be dropped, duplicated, or lost on shutdown;
// use ListChanges/ReadProjection for durable projection recovery and export permanent audit records to host storage before retention expires.
type EventSink func(context.Context, Event) error

type eventDispatcher struct {
	ctx     context.Context
	cancel  context.CancelFunc
	queue   chan Event
	done    chan struct{}
	timeout time.Duration
	sink    EventSink
}

// Create one bounded notification worker only when a host supplies a sink.
func newEvents(sink EventSink, timeout time.Duration) *eventDispatcher {
	if sink == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Keep at most 64 pending notifications; slow hosts drop events instead of blocking committed SDK work.
	d := &eventDispatcher{ctx: ctx, cancel: cancel, queue: make(chan Event, 64), done: make(chan struct{}), timeout: timeout, sink: sink}
	go func() {
		defer close(d.done)
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-d.queue:
				if ctx.Err() != nil {
					return
				}
				d.deliver(e)
			}
		}
	}()
	return d
}

// Give each host callback a deadline and contain its errors/panics outside committed SDK work.
func (d *eventDispatcher) deliver(e Event) {
	ctx, cancel := context.WithTimeout(d.ctx, d.timeout)
	defer cancel()
	defer func() { _ = recover() }()
	_ = d.sink(ctx, e)
}

// Cancel notification delivery and bound shutdown even if a host callback ignores cancellation.
func (d *eventDispatcher) close() {
	if d == nil {
		return
	}
	d.cancel()
	timer := time.NewTimer(d.timeout)
	defer timer.Stop()
	select {
	case <-d.done:
	case <-timer.C:
	}
}

// Queue sanitized committed identifiers without delaying authentication or leaking enrollment credentials.
func (s *SDK) emit(kind, scope, device string, revision uint64, suffix string) {
	if s.events == nil {
		return
	}
	e := Event{ID: fmt.Sprintf("%s:%s:%s", kind, device, suffix), Kind: kind, ScopeID: scope, DeviceID: device, Revision: revision, At: s.clock().UTC()}
	select {
	case <-s.events.ctx.Done():
	case s.events.queue <- e:
	default:
	}
}
