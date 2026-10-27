// SPDX-License-Identifier: LGPL-3.0-only
// Package hosting demonstrates a consumer-owned application boundary without a web or business framework.
package hosting

import (
	"context"
	"errors"
)

// DeviceRecord is host-owned projection data; business asset/user records stay in the host.
type DeviceRecord struct{ ID, ScopeID, CorrelationID, State string }

// EnrollmentTicket contains the private token returned only at creation, never by a device read.
type EnrollmentTicket struct{ Token string }

// Registry describes only the application capabilities required by this example consumer.
type Registry interface {
	// Create starts a scoped enrollment with an opaque association key.
	// Usage: ticket, err := registry.Create(ctx, scope, intentID).
	Create(context.Context, string, string) (EnrollmentTicket, error)
	// Find reads a single scoped device for host-owned association/projection.
	// Usage: device, err := registry.Find(ctx, scope, deviceID).
	Find(context.Context, string, string) (DeviceRecord, error)
}

// Authorize resolves the authenticated actor's permission before any registry call.
type Authorize func(context.Context, string) error

// Service owns application authorization; bootstrap supplies an adapter and actor-aware authorizer.
type Service struct {
	Registry  Registry
	Authorize Authorize
}

// check rejects incomplete wiring and invokes host authorization for this request's scope.
func (s Service) check(ctx context.Context, scope string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Registry == nil || s.Authorize == nil || scope == "" {
		return errors.New("registry, authorizer and explicit scope required")
	}
	return s.Authorize(ctx, scope)
}

// Enroll authorizes a caller before creating a private scoped enrollment ticket.
// The host persists its association intent and handles lost creation responses explicitly.
// Usage: ticket, err := service.Enroll(ctx, authorizedScope, intentID).
func (s Service) Enroll(ctx context.Context, scope, correlation string) (EnrollmentTicket, error) {
	if err := s.check(ctx, scope); err != nil {
		return EnrollmentTicket{}, err
	}
	return s.Registry.Create(ctx, scope, correlation)
}

// Device authorizes every read, including requests that carry a previously issued device ID.
// Usage: device, err := service.Device(ctx, authorizedScope, deviceID).
func (s Service) Device(ctx context.Context, scope, id string) (DeviceRecord, error) {
	if err := s.check(ctx, scope); err != nil {
		return DeviceRecord{}, err
	}
	return s.Registry.Find(ctx, scope, id)
}
