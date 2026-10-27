// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"context"
	"strings"
	"time"

	"github.com/thienvu18/kylok/go/ports"
	"github.com/thienvu18/kylok/go/provisioning"
)

// ListApplications lists verified managed APKs in one authorized scope.
// Usage: page, err := sdk.ListApplications(ctx, scope, "", 100); use the final SHA256 as the next cursor.
func (s *SDK) ListApplications(ctx context.Context, scope, after string, limit int) ([]Artifact, error) {
	if !idPattern.MatchString(scope) || limit < 1 || limit > 100 || (after != "" && !digestPattern.MatchString(after)) {
		return nil, invalidArgument("scope and valid cursor required; limit must be 1–100")
	}
	if err := s.checkOpen(ctx); err != nil {
		return nil, err
	}
	values, err := s.store.ListArtifacts(ctx, scope, after, limit)
	result := make([]Artifact, 0, len(values))
	for _, value := range values {
		result = append(result, publicArtifact(value))
	}
	return result, err
}

// Bind verified APK properties to an explicit management scope before catalog publication.
func artifactMetadata(scope string, apk *provisioning.APK) ports.Artifact {
	m := apk.Metadata()
	return ports.Artifact{ScopeID: scope, SHA256: m.SHA256, PackageName: m.PackageName, VersionCode: m.VersionCode, MinSDK: uint32(m.MinSDK), MaxSDK: uint32(m.MaxSDK), SigningCertificates: m.SigningCertificates, SupportedABIs: m.SupportedABIs, SizeBytes: m.SizeBytes}
}

// RegisterApplication publishes verified immutable metadata in one authorized scope.
// Usage: artifact, err := sdk.RegisterApplication(ctx, scope, verifiedAPK).
func (s *SDK) RegisterApplication(ctx context.Context, scope string, apk *provisioning.APK) (Artifact, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Artifact{}, err
	}
	if !idPattern.MatchString(scope) || !apk.Verified() {
		return Artifact{}, invalidArgument("scope and verified application APK required")
	}
	if apk.Metadata().PackageName == s.bootstrap.Metadata().PackageName && !apk.IsDPC() {
		return Artifact{}, invalidArgument("self-update APK must retain the DPC provisioning components")
	}
	if apk.Metadata().Debuggable && !s.allowDebugAPK {
		return Artifact{}, invalidArgument("debuggable application requires AllowDebugAPK")
	}
	a := artifactMetadata(scope, apk)
	if err := s.artifacts.Put(ctx, a.SHA256, apk.Bytes()); err != nil {
		return publicArtifact(a), err
	}
	if err := s.store.RegisterArtifact(ctx, a); err != nil {
		return publicArtifact(a), err
	}
	return publicArtifact(a), nil
}

// AssignApplication creates a desired revision and one durable install operation.
// Reusing a key with the same device/artifact returns the original operation.
// Usage: operation, err := sdk.AssignApplication(ctx, scope, deviceID, digest, requestKey, 24*time.Hour).
func (s *SDK) AssignApplication(ctx context.Context, scope, device, digest, key string, timeout time.Duration) (Operation, error) {
	if err := s.checkOpen(ctx); err != nil {
		return Operation{}, err
	}
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || !digestPattern.MatchString(digest) || !idPattern.MatchString(key) || timeout < time.Minute || timeout > 7*24*time.Hour {
		return Operation{}, invalidArgument("valid scope/device/digest/idempotency key and timeout (1m–7d) required")
	}
	id, err := secret()
	if err != nil {
		return Operation{}, err
	}
	now := s.clock().UTC().Truncate(time.Millisecond)
	op, err := s.store.Assign(ctx, ports.Assignment{ScopeID: scope, DeviceID: device, ArtifactSHA256: digest, IdempotencyKey: key, OperationID: id, Now: now, Deadline: now.Add(timeout)})
	if err == nil {
		s.emit("application_assigned", scope, device, op.Revision, op.ID)
	}
	return publicOperation(op), err
}

// ListOperations returns durable scoped progress, failures and observed installation results.
// Usage: operations, err := sdk.ListOperations(ctx, scope, deviceID, "", 100).
func (s *SDK) ListOperations(ctx context.Context, scope, device, after string, limit int) ([]Operation, error) {
	if !idPattern.MatchString(scope) || !devicePattern.MatchString(device) || limit < 1 || limit > 100 {
		return nil, invalidArgument("scope/device required; limit must be 1–100")
	}
	if err := s.checkOpen(ctx); err != nil {
		return nil, err
	}
	if after != "" && !devicePattern.MatchString(after) {
		return nil, ErrInvalidArgument
	}
	values, err := s.store.ListOperations(ctx, scope, device, after, limit, s.clock().UTC())
	return publicOperations(values), err
}

// RemoveApplication removes unreferenced scoped catalog metadata. Content bytes
// are retained: deleting shared files concurrently with registrations is unsafe.
// Usage: err := sdk.RemoveApplication(ctx, scope, digest).
func (s *SDK) RemoveApplication(ctx context.Context, scope, digest string) error {
	if err := s.checkOpen(ctx); err != nil {
		return err
	}
	if !idPattern.MatchString(scope) || !digestPattern.MatchString(digest) {
		return invalidArgument("scope and digest required")
	}
	return s.store.RemoveArtifact(ctx, scope, digest)
}

// Accept only the exact opaque device credential form, never cookies or URL query credentials.
func bearer(value string) (string, bool) {
	if !strings.HasPrefix(value, "Bearer ") || len(value) != 50 {
		return "", false
	}
	return strings.TrimPrefix(value, "Bearer "), true
}
