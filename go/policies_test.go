// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go"
	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/internal/testapk"
	"github.com/thienvu18/kylok/go/ports"
	"google.golang.org/protobuf/proto"
)

func ptr[T any](value T) *T { return &value }

func TestPolicyCannotClaimRequiredAppOrKioskWithoutMatchingInventory(t *testing.T) {
	s, _ := setup(t, time.Now)
	client, _ := start(t, s)
	id, credential := enrolled(t, s, client, "site")
	a, err := s.RegisterApplication(ctx, "site", testapk.Make(t, testapk.Manifest))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AssignApplication(ctx, "site", id, a.SHA256, "install", time.Hour); err != nil {
		t.Fatal(err)
	}
	p := kylok.Policy{RequiredPackages: []string{a.PackageName}, LockTask: &kylok.LockTaskPolicy{Packages: []string{a.PackageName}, KioskPackage: a.PackageName},
		Permissions: []kylok.PermissionRule{{PackageName: a.PackageName, Permission: "android.permission.CAMERA", State: "GRANTED"}}}
	want, err := s.SetPolicy(ctx, "site", id, "policy", p)
	if err != nil {
		t.Fatal(err)
	}
	request := syncRequest(id, credential)
	request.Msg.ReportedRevision = want.Revision
	request.Msg.Inventory.Capabilities = []string{"install_apk", "durable_sync", "policy.kiosk", "policy.permissions"}
	request.Msg.PolicyReport = &v1.PolicyReport{Revision: want.Revision, SnapshotRevision: want.Revision}
	for key, value := range ports.PolicyExpectations(p, []ports.DesiredApplication{{Artifact: a, Revision: 1}}) {
		request.Msg.PolicyReport.Results = append(request.Msg.PolicyReport.Results, &v1.PolicyResult{RuleId: key, State: v1.ComplianceState_COMPLIANCE_STATE_APPLIED, ObservedValue: value})
	}
	for i, actual := range []*v1.InstalledApplication{nil,
		{PackageName: a.PackageName, VersionCode: a.VersionCode + 1, SigningCertificates: a.SigningCertificates},
		{PackageName: a.PackageName, VersionCode: a.VersionCode, SigningCertificates: []string{strings.Repeat("c", 64)}},
		{PackageName: a.PackageName, VersionCode: a.VersionCode, SigningCertificates: a.SigningCertificates},
	} {
		request.Msg.Sequence = uint64(i + 1)
		request.Msg.Inventory.Applications = nil
		if actual != nil {
			request.Msg.Inventory.Applications = []*v1.InstalledApplication{actual}
		}
		response, err := client.Sync(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range response.Msg.PolicyCompliance {
			if r.RuleId == "cleanup" {
				continue
			}
			if i < 3 && r.State == v1.ComplianceState_COMPLIANCE_STATE_APPLIED {
				t.Fatal("contradicting inventory accepted", i, r)
			}
			if i == 3 && r.State != v1.ComplianceState_COMPLIANCE_STATE_APPLIED {
				t.Fatal("matching inventory rejected", r)
			}
		}
	}
}

func TestPolicyAssignmentSyncReadbackAndReplacement(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, options := setup(t, func() time.Time { return now })
	client, server := start(t, s)
	id, credential := enrolled(t, s, client, "site")
	apk := testapk.Make(t, testapk.Manifest)
	a, err := s.RegisterApplication(ctx, "site", apk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AssignApplication(ctx, "site", id, a.SHA256, "install", time.Hour); err != nil {
		t.Fatal(err)
	}
	p := kylok.Policy{RequiredPackages: []string{a.PackageName}, CameraDisabled: ptr(true), ScreenCaptureDisabled: ptr(false), MaximumTimeToLockMillis: ptr(int64(60000)),
		LockTask:     &kylok.LockTaskPolicy{Packages: []string{a.PackageName}, KioskPackage: a.PackageName},
		Permissions:  []kylok.PermissionRule{{PackageName: a.PackageName, Permission: "android.permission.CAMERA", State: "GRANTED"}},
		Restrictions: []kylok.RestrictionRule{{Name: "no_uninstall_apps", Enabled: true}, {Name: "no_modify_accounts", Enabled: true}}}
	want, err := s.SetPolicy(ctx, "site", id, "policy-1", p)
	if err != nil || want.Revision != 2 {
		t.Fatal(want, err)
	}
	p.Restrictions[0], p.Restrictions[1] = p.Restrictions[1], p.Restrictions[0]
	again, err := s.SetPolicy(ctx, "site", id, "policy-1", p)
	if err != nil || !reflect.DeepEqual(want, again) {
		t.Fatal("idempotency after normalization", again, err)
	}
	p.CameraDisabled = ptr(false)
	if _, err = s.SetPolicy(ctx, "site", id, "policy-1", p); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("changed idempotency payload", err)
	}
	if _, err = s.SetPolicy(ctx, "other", id, "policy-2", p); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("cross-scope write", err)
	}
	if _, err = s.GetPolicy(ctx, "other", id); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("cross-scope read", err)
	}
	if _, err = s.SetPolicy(ctx, "site", id, "missing", kylok.Policy{RequiredPackages: []string{"io.missing.app"}}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("unassigned required app", err)
	}
	request := syncRequest(id, credential)
	request.Msg.Inventory.Capabilities = []string{"durable_sync", "install_apk", "policy.camera", "policy.screen_capture", "policy.kiosk", "policy.permissions", "policy.restrictions", "policy.maximum_time_to_lock"}
	response, err := client.Sync(ctx, request)
	if err != nil || response.Msg.DesiredPolicy.Revision != 2 || len(response.Msg.PolicyCompliance) != 9 {
		t.Fatal("policy snapshot", response, err)
	}
	for _, r := range response.Msg.PolicyCompliance {
		if r.State == v1.ComplianceState_COMPLIANCE_STATE_APPLIED {
			t.Fatal("unobserved policy marked applied")
		}
	}
	if field := response.Msg.DesiredPolicy.Policy.ScreenCaptureDisabled; field == nil || *field {
		t.Fatal("explicit false lost during policy delivery")
	}
	request.Msg.Sequence = 2
	request.Msg.ReportedRevision = 2
	request.Msg.Inventory.Applications = []*v1.InstalledApplication{{PackageName: a.PackageName, VersionCode: a.VersionCode, SigningCertificates: a.SigningCertificates}}
	report := &v1.PolicyReport{Revision: 2, SnapshotRevision: 2}
	expected := ports.PolicyExpectations(want.Policy, []ports.DesiredApplication{{Artifact: a, Revision: 1}})
	for key, value := range expected {
		report.Results = append(report.Results, &v1.PolicyResult{RuleId: key, State: v1.ComplianceState_COMPLIANCE_STATE_APPLIED, ObservedValue: value})
	}
	for _, r := range report.Results {
		if r.RuleId == "camera" {
			r.ObservedValue = "false"
		}
	}
	request.Msg.PolicyReport = report
	response, err = client.Sync(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range response.Msg.PolicyCompliance {
		if r.RuleId == "camera" && (r.State != v1.ComplianceState_COMPLIANCE_STATE_FAILED || r.Reason != "OBSERVATION_MISMATCH") {
			t.Fatal("setter assertion accepted as observation", r)
		}
	}
	// The replay binds policy observations and survives server/storage restart.
	snapshot := proto.Clone(response.Msg)
	server.Close()
	s.Close()
	s, err = kylok.New(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	client, _ = start(t, s)
	replay, err := client.Sync(ctx, request)
	if err != nil || !proto.Equal(snapshot, replay.Msg) {
		t.Fatal("policy replay changed", err)
	}
	request.Msg.PolicyReport = proto.Clone(report).(*v1.PolicyReport)
	request.Msg.PolicyReport.Results[0].Reason = "changed"
	_, err = client.Sync(ctx, request)
	reason(t, err, v1.ErrorReason_ERROR_REASON_CONFLICT)
	request.Msg.Sequence = 3
	for _, r := range request.Msg.PolicyReport.Results {
		r.ObservedValue = expected[r.RuleId]
		r.Reason = ""
	}
	response, err = client.Sync(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range response.Msg.PolicyCompliance {
		if r.State != v1.ComplianceState_COMPLIANCE_STATE_APPLIED {
			t.Fatal("correct observation not applied", r)
		}
	}
	status, err := s.GetPolicy(ctx, "site", id)
	if err != nil || status.Reported == nil || status.Reported.SnapshotRevision != 2 {
		t.Fatal(status, err)
	}
	// An APK revision invalidates earlier policy observations without changing
	// the policy document. Required-app and kiosk readback must run again.
	if _, err = s.AssignApplication(ctx, "site", id, a.SHA256, "refresh-app", time.Hour); err != nil {
		t.Fatal(err)
	}
	status, err = s.GetPolicy(ctx, "site", id)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range status.Compliance {
		if r.State == "APPLIED" {
			t.Fatal("stale snapshot remained compliant", r)
		}
	}
	cleared, err := s.SetPolicy(ctx, "site", id, "release", kylok.Policy{})
	if err != nil || cleared.Revision != 4 {
		t.Fatal(cleared, err)
	}
	request.Msg.Sequence = 4
	request.Msg.ReportedRevision = 2 // Old observations can be in flight during replacement.
	response, err = client.Sync(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Msg.PolicyCompliance) != 1 || response.Msg.PolicyCompliance[0].RuleId != "cleanup" || response.Msg.PolicyCompliance[0].State == v1.ComplianceState_COMPLIANCE_STATE_APPLIED {
		t.Fatal("old policy report closed cleanup", response.Msg)
	}
	request.Msg.Sequence = 5
	request.Msg.ReportedRevision = 4
	request.Msg.PolicyReport = &v1.PolicyReport{Revision: 4, SnapshotRevision: 4, Results: []*v1.PolicyResult{{RuleId: "cleanup", State: v1.ComplianceState_COMPLIANCE_STATE_APPLIED, ObservedValue: "complete"}}}
	response, err = client.Sync(ctx, request)
	if err != nil || response.Msg.PolicyCompliance[0].State != v1.ComplianceState_COMPLIANCE_STATE_APPLIED {
		t.Fatal(response, err)
	}
	if err = s.RevokeDevice(ctx, "site", id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetPolicy(ctx, "site", id, "after-revoke", kylok.Policy{}); !errors.Is(err, ports.ErrRevoked) {
		t.Fatal("revoked assignment", err)
	}
	status, err = s.GetPolicy(ctx, "site", id)
	if err != nil || status.Compliance[0].Reason != "DEVICE_REVOKED" {
		t.Fatal("revoked policy status", status, err)
	}
	_, err = client.Sync(ctx, request)
	reason(t, err, v1.ErrorReason_ERROR_REASON_DEVICE_REVOKED)
}

func TestPolicyValidationConcurrentAssignmentAndReportBounds(t *testing.T) {
	s, _ := setup(t, time.Now)
	client, _ := start(t, s)
	id, credential := enrolled(t, s, client, "site")
	for _, p := range []kylok.Policy{
		{RequiredPackages: []string{"bad"}}, {RequiredPackages: []string{"io.a", "io.a"}},
		{LockTask: &kylok.LockTaskPolicy{Packages: []string{"io.a"}, KioskPackage: "io.a"}},
		{Restrictions: []kylok.RestrictionRule{{Name: "no_factory_reset", Enabled: true}}},
		{MaximumTimeToLockMillis: ptr(int64(-1))}, {MaximumTimeToLockMillis: ptr(int64(86400001))},
		{Permissions: []kylok.PermissionRule{{PackageName: "io.a", Permission: "android.permission.CAMERA", State: "GRANTED"}}},
		{RequiredPackages: []string{"io.a"}, Permissions: []kylok.PermissionRule{{PackageName: "io.a", Permission: "android.permission.CAMERA", State: "bad"}}},
	} {
		if _, err := s.SetPolicy(ctx, "site", id, "invalid", p); err == nil {
			t.Fatal("invalid policy accepted", p)
		}
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := s.SetPolicy(ctx, "site", id, "one", kylok.Policy{CameraDisabled: ptr(true)})
			if err != nil || p.Revision != 1 {
				t.Error("concurrent policy", p, err)
			}
		}()
	}
	wg.Wait()
	request := syncRequest(id, credential)
	request.Msg.Sequence = 1
	response, err := client.Sync(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.Device.DesiredRevision != 1 {
		t.Fatal("duplicate policy revision")
	}
	for _, r := range response.Msg.PolicyCompliance {
		if r.State != v1.ComplianceState_COMPLIANCE_STATE_UNSUPPORTED {
			t.Fatal("missing capability", r)
		}
	}
	request.Msg.Sequence = 2
	request.Msg.ReportedRevision = 1
	valid := &v1.PolicyReport{Revision: 1, SnapshotRevision: 1, Results: []*v1.PolicyResult{{RuleId: "camera", State: v1.ComplianceState_COMPLIANCE_STATE_PENDING, Reason: "OBSERVATION_PENDING"}}}
	for _, mutate := range []func(*v1.PolicyReport){
		func(p *v1.PolicyReport) { p.Revision = 0 }, func(p *v1.PolicyReport) { p.SnapshotRevision = 2 },
		func(p *v1.PolicyReport) { p.Results = append(p.Results, proto.Clone(p.Results[0]).(*v1.PolicyResult)) },
		func(p *v1.PolicyReport) { p.Results[0].ObservedValue = strings.Repeat("x", 20001) },
		func(p *v1.PolicyReport) { p.Results[0].State = v1.ComplianceState_COMPLIANCE_STATE_UNSPECIFIED },
	} {
		p := proto.Clone(valid).(*v1.PolicyReport)
		mutate(p)
		request.Msg.PolicyReport = p
		_, err = client.Sync(ctx, request)
		reason(t, err, v1.ErrorReason_ERROR_REASON_INVALID_INPUT)
	}
	request.Msg.PolicyReport = proto.Clone(valid).(*v1.PolicyReport)
	request.Msg.PolicyReport.Results[0].RuleId = "unknown"
	_, err = client.Sync(ctx, request)
	reason(t, err, v1.ErrorReason_ERROR_REASON_CONFLICT)
	// Failed exchanges did not consume the sequence or partially persist state.
	request.Msg.PolicyReport = valid
	if _, err = client.Sync(ctx, request); err != nil {
		t.Fatal(err)
	}
}
