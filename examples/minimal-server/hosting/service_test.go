// SPDX-License-Identifier: LGPL-3.0-only
package hosting

import (
	"context"
	"errors"
	"testing"
)

type recordingRegistry struct {
	calls              int
	scope, correlation string
}

func (r *recordingRegistry) Create(ctx context.Context, scope, correlation string) (EnrollmentTicket, error) {
	r.calls++
	r.scope = scope
	r.correlation = correlation
	return EnrollmentTicket{Token: "private"}, nil
}
func (r *recordingRegistry) Find(ctx context.Context, scope, id string) (DeviceRecord, error) {
	r.calls++
	return DeviceRecord{ID: id, ScopeID: scope}, nil
}
func TestApplicationAuthorizationPrecedesRegistry(t *testing.T) {
	denied := errors.New("denied")
	registry := &recordingRegistry{}
	service := Service{Registry: registry, Authorize: func(ctx context.Context, scope string) error {
		if scope != "own" {
			return denied
		}
		return nil
	}}
	if _, err := service.Enroll(context.Background(), "other", "intent"); !errors.Is(err, denied) || registry.calls != 0 {
		t.Fatal(err, registry)
	}
	if _, err := service.Device(context.Background(), "other", "device"); !errors.Is(err, denied) || registry.calls != 0 {
		t.Fatal(err, registry)
	}
	if _, err := service.Enroll(context.Background(), "own", "intent"); err != nil || registry.calls != 1 || registry.scope != "own" || registry.correlation != "intent" {
		t.Fatal(err, registry)
	}
	service.Authorize = nil
	if _, err := service.Enroll(context.Background(), "own", "intent"); err == nil || registry.calls != 1 {
		t.Fatal("incomplete host bypassed authorization")
	}
}
