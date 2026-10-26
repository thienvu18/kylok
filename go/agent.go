// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	kylokv1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/ports"
	"google.golang.org/protobuf/proto"
)

type agentService struct{ sdk *SDK }

// Encode a stable public reason/retryability detail without leaking backend errors or secrets.
func rpcError(code connect.Code, reason kylokv1.ErrorReason, retry bool) error {
	e := connect.NewError(code, errors.New(reason.String()))
	if detail, err := connect.NewErrorDetail(&kylokv1.ErrorDetail{Reason: reason, Retryable: retry}); err == nil {
		e.AddDetail(detail)
	}
	return e
}

// Map transactional/authentication failures into protocol status without revealing storage internals.
func storeError(err error) error {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return rpcError(connect.CodeUnauthenticated, kylokv1.ErrorReason_ERROR_REASON_TOKEN_INVALID, false)
	case errors.Is(err, ports.ErrExpired):
		return rpcError(connect.CodeFailedPrecondition, kylokv1.ErrorReason_ERROR_REASON_TOKEN_EXPIRED, false)
	case errors.Is(err, ports.ErrConsumed):
		return rpcError(connect.CodeAlreadyExists, kylokv1.ErrorReason_ERROR_REASON_TOKEN_CONSUMED, false)
	case errors.Is(err, ports.ErrRecoveryExpired):
		return rpcError(connect.CodeFailedPrecondition, kylokv1.ErrorReason_ERROR_REASON_RECOVERY_EXPIRED, false)
	case errors.Is(err, ports.ErrRevoked):
		return rpcError(connect.CodePermissionDenied, kylokv1.ErrorReason_ERROR_REASON_DEVICE_REVOKED, false)
	case errors.Is(err, ports.ErrUnauthorized):
		return rpcError(connect.CodeUnauthenticated, kylokv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED, false)
	case errors.Is(err, ports.ErrConflict):
		return rpcError(connect.CodeAlreadyExists, kylokv1.ErrorReason_ERROR_REASON_CONFLICT, false)
	case errors.Is(err, ports.ErrStale):
		return rpcError(connect.CodeFailedPrecondition, kylokv1.ErrorReason_ERROR_REASON_STALE_SEQUENCE, false)
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, context.DeadlineExceeded)
	default:
		return rpcError(connect.CodeUnavailable, kylokv1.ErrorReason_ERROR_REASON_STORAGE_UNAVAILABLE, true)
	}
}

// Bound and validate self-reported ownership, capabilities and installed package metadata before persistence.
func validateInventory(i *kylokv1.Inventory) (ports.Inventory, error) {
	if i == nil || i.ApiLevel < 33 || i.ApiLevel > 37 || !i.DeviceOwner || len(i.Model) < 1 || len(i.Model) > 256 || len(i.BuildFingerprint) > 1024 || len(i.AgentVersion) < 1 || len(i.AgentVersion) > 128 {
		return ports.Inventory{}, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
	}
	if len(i.Capabilities) > 32 || len(i.Applications) > 128 || len(i.SupportedAbis) > 16 {
		return ports.Inventory{}, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
	}
	for _, value := range append(append([]string(nil), i.Capabilities...), i.SupportedAbis...) {
		if len(value) == 0 || len(value) > 64 {
			return ports.Inventory{}, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
		}
	}
	result := ports.Inventory{APILevel: i.ApiLevel, Model: i.Model, BuildFingerprint: i.BuildFingerprint, AgentVersion: i.AgentVersion, DeviceOwner: i.DeviceOwner, Capabilities: i.Capabilities, SupportedABIs: i.SupportedAbis}
	seen := map[string]bool{}
	for _, app := range i.Applications {
		if app.PackageName == "" || len(app.PackageName) > 256 || app.VersionCode < 1 || seen[app.PackageName] || len(app.SigningCertificates) == 0 || len(app.SigningCertificates) > 8 {
			return result, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
		}
		for _, cert := range app.SigningCertificates {
			if !digestPattern.MatchString(cert) {
				return result, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
			}
		}
		seen[app.PackageName] = true
		result.Applications = append(result.Applications, ports.InstalledApplication{PackageName: app.PackageName, VersionCode: app.VersionCode, SigningCertificates: app.SigningCertificates})
	}
	return result, nil
}

// Reject an unsupported wire version before interpreting its enrollment or sync fields.
func protocol(version uint32) error {
	if version != ProtocolVersion {
		return rpcError(connect.CodeFailedPrecondition, kylokv1.ErrorReason_ERROR_REASON_PROTOCOL_UNSUPPORTED, false)
	}
	return nil
}

// Enroll validates the attempt key and atomically claims or recovers an encrypted scoped identity.
// Usage: mounted AgentService handlers invoke this RPC after host listener registration.
func (a *agentService) Enroll(ctx context.Context, r *connect.Request[kylokv1.EnrollRequest]) (*connect.Response[kylokv1.EnrollResponse], error) {
	m := r.Msg
	if err := protocol(m.ProtocolVersion); err != nil {
		return nil, err
	}
	inventory, err := validateInventory(m.Inventory)
	if err != nil {
		return nil, err
	}
	if len(m.Token) != 43 || len(m.AttemptId) < 16 || !idPattern.MatchString(m.AttemptId) || len(m.PublicKey) > 256 {
		return nil, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
	}
	public, err := x509.ParsePKIXPublicKey(m.PublicKey)
	if err != nil {
		return nil, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
	}
	ec, ok := public.(*ecdsa.PublicKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
	}
	recipient, err := ec.ECDH()
	if err != nil {
		return nil, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
	}
	canonical, err := x509.MarshalPKIXPublicKey(ec)
	if err != nil {
		return nil, storeError(err)
	}
	keyHash := sha256.Sum256(canonical)
	enrollment, err := a.sdk.store.Enrollment(ctx, hash(m.Token))
	if err != nil {
		return nil, storeError(err)
	}
	id, err := secret()
	if err != nil {
		return nil, storeError(err)
	}
	credential, err := secret()
	if err != nil {
		return nil, storeError(err)
	}
	now := a.sdk.clock().UTC().Truncate(time.Millisecond)
	device := ports.Device{ID: id, ScopeID: enrollment.ScopeID, State: "ENROLLED", Inventory: inventory, CreatedAt: now}
	envelope, err := sealCredential(recipient, credential, device.ID, device.ScopeID, m.AttemptId)
	if err != nil {
		return nil, storeError(err)
	}
	response := &kylokv1.EnrollResponse{Device: deviceMessage(device), Credential: envelope, ProtocolVersion: ProtocolVersion}
	receipt, err := proto.Marshal(response)
	if err != nil {
		return nil, storeError(err)
	}
	receipt, err = a.sdk.store.Consume(ctx, ports.Attempt{TokenHash: hash(m.Token), ID: m.AttemptId, KeyHash: keyHash[:], Device: device, CredentialHash: hash(credential), Receipt: receipt, Now: now, RecoverUntil: now.Add(24 * time.Hour)})
	if err != nil {
		return nil, storeError(err)
	}
	if err = proto.Unmarshal(receipt, response); err != nil {
		return nil, storeError(err)
	}
	a.sdk.emit("device_enrolled", response.Device.ScopeId, response.Device.Id, response.Device.DesiredRevision, "")
	if response.Device == nil || response.Device.ScopeId != enrollment.ScopeID || response.Credential == nil || response.ProtocolVersion != ProtocolVersion {
		return nil, storeError(errors.New("invalid store receipt"))
	}
	return connect.NewResponse(response), nil
}

// Encrypt the device credential to the attempt's P-256 key with domain-separated authenticated binding.
func sealCredential(recipient *ecdh.PublicKey, credential, id, scope, attempt string) (*kylokv1.CredentialEnvelope, error) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := ephemeral.ECDH(recipient)
	if err != nil {
		return nil, err
	}
	// The protocol label separates enrollment keys; 32 derived bytes select AES-256.
	key, err := hkdf.Key(sha256.New, shared, nil, "kylok/enrollment/v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	public, err := x509.MarshalPKIXPublicKey(ephemeral.PublicKey())
	if err != nil {
		return nil, err
	}
	// Bind ciphertext to the exact identity/attempt using the Android decoder's newline-separated encoding.
	aad := []byte(fmt.Sprintf("kylok/v1\n%s\n%s\n%s", id, scope, attempt))
	return &kylokv1.CredentialEnvelope{EphemeralPublicKey: public, Nonce: nonce, Ciphertext: gcm.Seal(nil, nonce, []byte(credential), aad)}, nil
}

// Sync validates bearer-bound requests and returns the transaction's durable snapshot using origin-relative APK paths.
// Usage: mounted AgentService handlers invoke this RPC after host listener registration.
func (a *agentService) Sync(ctx context.Context, r *connect.Request[kylokv1.SyncRequest]) (*connect.Response[kylokv1.SyncResponse], error) {
	if err := protocol(r.Msg.ProtocolVersion); err != nil {
		return nil, err
	}
	inventory, err := validateInventory(r.Msg.Inventory)
	if err != nil {
		return nil, err
	}
	header := r.Header().Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || len(header) != 7+43 || !devicePattern.MatchString(r.Msg.DeviceId) {
		return nil, rpcError(connect.CodeUnauthenticated, kylokv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED, false)
	}
	if r.Msg.Sequence == 0 || r.Msg.Sequence > 1<<62 || len(r.Msg.Reports) > 64 {
		return nil, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
	}
	input := ports.SyncInput{DeviceID: r.Msg.DeviceId, CredentialHash: hash(strings.TrimPrefix(header, "Bearer ")), Inventory: inventory, Now: a.sdk.clock().UTC(), Sequence: r.Msg.Sequence, ReportedRevision: r.Msg.ReportedRevision}
	if report := r.Msg.PolicyReport; report != nil {
		if report.Revision == 0 || report.Revision > report.SnapshotRevision || report.SnapshotRevision > r.Msg.ReportedRevision || len(report.Results) > 144 {
			return nil, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
		}
		input.PolicyReport = &ports.PolicyReport{Revision: report.Revision, SnapshotRevision: report.SnapshotRevision}
		seen := map[string]bool{}
		for _, result := range report.Results {
			state := strings.TrimPrefix(result.State.String(), "COMPLIANCE_STATE_")
			if result.RuleId == "" || len(result.RuleId) > 600 || seen[result.RuleId] || len(result.Reason) > 128 || len(result.ObservedValue) > 20000 || (state != "APPLIED" && state != "PENDING" && state != "FAILED" && state != "UNSUPPORTED") {
				return nil, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
			}
			seen[result.RuleId] = true
			input.PolicyReport.Results = append(input.PolicyReport.Results, ports.PolicyResult{RuleID: result.RuleId, State: state, Reason: result.Reason, ObservedValue: result.ObservedValue})
		}
	}
	for _, report := range r.Msg.Reports {
		state := strings.TrimPrefix(report.State.String(), "OPERATION_STATE_")
		if !idPattern.MatchString(report.Id) || !devicePattern.MatchString(report.OperationId) || len(report.LeaseId) != 48 || report.Sequence == 0 || report.Sequence > 1<<62 || report.DownloadedBytes < 0 || report.DownloadedBytes > 64<<20 || len(report.ErrorCode) > 128 || len(report.ErrorMessage) > 1024 || (state != "RUNNING" && state != "WAITING_USER" && state != "SUCCEEDED" && state != "FAILED") {
			return nil, rpcError(connect.CodeInvalidArgument, kylokv1.ErrorReason_ERROR_REASON_INVALID_INPUT, false)
		}
		var observed *ports.InstalledApplication
		if report.Observed != nil {
			observed = &ports.InstalledApplication{PackageName: report.Observed.PackageName, VersionCode: report.Observed.VersionCode, SigningCertificates: report.Observed.SigningCertificates}
		}
		input.Reports = append(input.Reports, ports.OperationReport{ID: report.Id, OperationID: report.OperationId, LeaseID: report.LeaseId, Sequence: report.Sequence, State: state, DownloadedBytes: report.DownloadedBytes, ErrorCode: report.ErrorCode, ErrorMessage: report.ErrorMessage, Observed: observed})
	}
	snapshot, err := a.sdk.store.Sync(ctx, input)
	if err != nil {
		return nil, storeError(err)
	}
	a.sdk.emit("device_synced", snapshot.Device.ScopeID, snapshot.Device.ID, snapshot.Device.DesiredRevision, fmt.Sprint(snapshot.Sequence))
	response := &kylokv1.SyncResponse{Device: deviceMessage(snapshot.Device), ProtocolVersion: ProtocolVersion, Sequence: snapshot.Sequence}
	if want := snapshot.DesiredPolicy; want != nil {
		response.DesiredPolicy = &kylokv1.DesiredPolicy{Revision: want.Revision, Policy: policyMessage(want.Policy)}
	}
	for _, r := range snapshot.PolicyCompliance {
		response.PolicyCompliance = append(response.PolicyCompliance, &kylokv1.PolicyResult{RuleId: r.RuleID, State: kylokv1.ComplianceState(kylokv1.ComplianceState_value["COMPLIANCE_STATE_"+r.State]), Reason: r.Reason, ObservedValue: r.ObservedValue})
	}
	for _, d := range snapshot.Desired {
		response.DesiredApplications = append(response.DesiredApplications, &kylokv1.DesiredApplication{Artifact: artifactMessage(d.Artifact), Revision: d.Revision})
	}
	for _, op := range snapshot.Operations {
		// Relative paths keep downloads on the endpoint selected at enrollment,
		// including another LAN interface or a host-controlled domain alias.
		response.Operations = append(response.Operations, &kylokv1.Operation{Id: op.ID, Revision: op.Revision, Artifact: artifactMessage(op.Artifact), LeaseId: op.LeaseID, LeaseExpiresAtUnixMillis: op.LeaseUntil.UnixMilli(), DeadlineUnixMillis: op.Deadline.UnixMilli(), State: kylokv1.OperationState(kylokv1.OperationState_value["OPERATION_STATE_"+op.State]), DownloadUrl: "/artifacts/" + op.ID + "/" + op.Artifact.SHA256 + ".apk"})
	}
	for _, r := range snapshot.Receipts {
		response.Receipts = append(response.Receipts, &kylokv1.ReportReceipt{ReportId: r.ReportID, Accepted: r.Accepted, Reason: r.Reason})
	}
	for _, c := range snapshot.Compliance {
		response.Compliance = append(response.Compliance, &kylokv1.ApplicationCompliance{PackageName: c.PackageName, DesiredRevision: c.Revision, State: kylokv1.ComplianceState(kylokv1.ComplianceState_value["COMPLIANCE_STATE_"+c.State]), Reason: c.Reason})
	}
	return connect.NewResponse(response), nil
}

// Convert verified store metadata to the shared wire shape without exposing artifact storage locations.
func artifactMessage(a ports.Artifact) *kylokv1.Artifact {
	return &kylokv1.Artifact{Sha256: a.SHA256, PackageName: a.PackageName, VersionCode: a.VersionCode, MinSdk: a.MinSDK, MaxSdk: a.MaxSDK, SigningCertificates: a.SigningCertificates, SupportedAbis: a.SupportedABIs, SizeBytes: a.SizeBytes}
}
