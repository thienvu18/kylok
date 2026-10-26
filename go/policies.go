// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"context"
	"errors"
	"fmt"

	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/ports"
)

type Policy = ports.Policy
type LockTaskPolicy = ports.LockTaskPolicy
type PermissionRule = ports.PermissionRule
type RestrictionRule = ports.RestrictionRule
type DesiredPolicy = ports.DesiredPolicy
type PolicyStatus = ports.PolicyStatus

// SetPolicy replaces this device's policy. Required packages must already have
// scoped APK assignments; callers authorize scope/device before calling the SDK.
// An empty document releases only settings captured by Kylok's policy journal.
// Usage: desired, err := sdk.SetPolicy(ctx, scope, deviceID, requestKey, policy).
func (s *SDK) SetPolicy(ctx context.Context, scope, device, key string, p Policy) (DesiredPolicy, error) {
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || !idPattern.MatchString(key) {
		return DesiredPolicy{}, errors.New("scope/device/idempotency key required")
	}
	p, err := ports.NormalizePolicy(p)
	if err != nil {
		return DesiredPolicy{}, err
	}
	want, err := s.store.SetPolicy(ctx, ports.PolicyAssignment{ScopeID: scope, DeviceID: device, IdempotencyKey: key, Policy: p})
	if err == nil {
		s.emit("policy_assigned", scope, device, want.Revision, fmt.Sprint(want.Revision))
	}
	return want, err
}

// GetPolicy reads scoped desired/reported policy and per-rule compliance without treating assignment as enforcement.
// Usage: status, err := sdk.GetPolicy(ctx, scope, deviceID).
func (s *SDK) GetPolicy(ctx context.Context, scope, device string) (PolicyStatus, error) {
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) {
		return PolicyStatus{}, errors.New("scope/device required")
	}
	return s.store.GetPolicy(ctx, scope, device)
}

// Preserve optional setting presence and typed permission states when encoding the policy document.
func policyMessage(p ports.Policy) *v1.Policy {
	m := &v1.Policy{RequiredPackages: p.RequiredPackages, CameraDisabled: p.CameraDisabled, ScreenCaptureDisabled: p.ScreenCaptureDisabled, MaximumTimeToLockMillis: p.MaximumTimeToLockMillis}
	if p.LockTask != nil {
		m.LockTask = &v1.LockTaskPolicy{Packages: p.LockTask.Packages, KioskPackage: p.LockTask.KioskPackage}
	}
	for _, r := range p.Restrictions {
		m.Restrictions = append(m.Restrictions, &v1.RestrictionRule{Name: r.Name, Enabled: r.Enabled})
	}
	for _, r := range p.Permissions {
		m.Permissions = append(m.Permissions, &v1.PermissionRule{PackageName: r.PackageName, Permission: r.Permission, State: v1.PermissionState(v1.PermissionState_value["PERMISSION_STATE_"+r.State])})
	}
	return m
}
