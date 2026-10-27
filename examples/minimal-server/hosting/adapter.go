// SPDX-License-Identifier: LGPL-3.0-only
package hosting

import (
	"context"
	"time"

	"github.com/thienvu18/kylok/go"
)

// SDKRegistry adapts public Kylok APIs into a consumer-owned application port.
// Bootstrap owns SDK readiness/listeners/shutdown; the application layer owns business transactions.
type SDKRegistry struct{ SDK *kylok.SDK }

// Create uses the host's scope/correlation and an explicit fifteen-minute token lifetime.
// Usage: adapter := SDKRegistry{SDK: sdk}; ticket, err := adapter.Create(ctx, scope, intentID).
func (a SDKRegistry) Create(ctx context.Context, scope, correlation string) (EnrollmentTicket, error) {
	result, err := a.SDK.CreateEnrollmentWithOptions(ctx, kylok.EnrollmentOptions{ScopeID: scope, TTL: 15 * time.Minute, CorrelationID: correlation})
	return EnrollmentTicket{Token: result.Token}, err
}

// Find maps only association/status fields into the host model, without storage or transport DTOs.
// Usage: record, err := adapter.Find(ctx, scope, deviceID).
func (a SDKRegistry) Find(ctx context.Context, scope, id string) (DeviceRecord, error) {
	result, err := a.SDK.GetDevice(ctx, scope, id)
	return DeviceRecord{ID: result.ID, ScopeID: result.ScopeID, CorrelationID: result.CorrelationID, State: result.State}, err
}
