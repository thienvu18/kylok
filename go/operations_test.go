// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/thienvu18/kylok/go"
	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/gen/kylok/v1/kylokv1connect"
	"github.com/thienvu18/kylok/go/internal/testapk"
	"github.com/thienvu18/kylok/go/ports"
	"google.golang.org/protobuf/proto"
)

func enrolled(t *testing.T, s *kylok.SDK, client kylokv1connect.AgentServiceClient, scope string) (string, string) {
	t.Helper()
	r, key := request(t, issue(t, s, scope).Token)
	response, err := client.Enroll(ctx, connect.NewRequest(r))
	if err != nil {
		t.Fatal(err)
	}
	return response.Msg.Device.Id, decrypt(t, key, response.Msg, r.AttemptId)
}
func exchange(t *testing.T, client kylokv1connect.AgentServiceClient, id, credential string, sequence, revision uint64, reports ...*v1.OperationReport) *v1.SyncResponse {
	t.Helper()
	request := syncRequest(id, credential)
	request.Msg.Sequence = sequence
	request.Msg.ReportedRevision = revision
	request.Msg.Reports = reports
	request.Msg.Inventory.Capabilities = []string{"install_apk"}
	response, err := client.Sync(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	return response.Msg
}
func TestDurableInstallLifecycleAndAuthorizedDownloads(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, o := setup(t, func() time.Time { return now })
	client, server := start(t, s)
	id, credential := enrolled(t, s, client, "site")
	apk := testapk.Make(t, strings.Replace(testapk.Manifest, `android:versionCode="3"`, `android:versionCode="4"`, 1))
	a, err := s.RegisterApplication(ctx, "site", apk)
	if err != nil {
		t.Fatal(err)
	}
	op, err := s.AssignApplication(ctx, "site", id, a.SHA256, "install-request", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.AssignApplication(ctx, "site", id, a.SHA256, "install-request", 2*time.Hour)
	if err != nil || duplicate.ID != op.ID {
		t.Fatal("assignment not idempotent", err)
	}
	snapshot := exchange(t, client, id, credential, 1, 0)
	if len(snapshot.Operations) != 1 || snapshot.Device.DesiredRevision != 1 || snapshot.Compliance[0].State != v1.ComplianceState_COMPLIANCE_STATE_PENDING {
		t.Fatalf("snapshot: %v", snapshot)
	}
	leased := snapshot.Operations[0]
	fetch := func(auth, device, lease, path, rangeValue string) *http.Response {
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("X-Kylok-Device-ID", device)
		req.Header.Set("X-Kylok-Lease-ID", lease)
		if rangeValue != "" {
			req.Header.Set("Range", rangeValue)
		}
		r, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	path := "/artifacts/" + op.ID + "/" + a.SHA256 + ".apk"
	response := fetch(credential, id, leased.LeaseId, path, "bytes=10-")
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 206 || !bytes.Equal(data, apk.Bytes()[10:]) {
		t.Fatal("range download", response.StatusCode)
	}
	otherID, otherCredential := enrolled(t, s, client, "other")
	for _, tc := range []struct{ credential, id, lease, path string }{
		{strings.Repeat("x", 43), id, leased.LeaseId, path}, {otherCredential, otherID, leased.LeaseId, path},
		{credential, id, strings.Repeat("0", 48), path}, {credential, id, leased.LeaseId, "/bootstrap/" + a.SHA256 + ".apk"},
	} {
		r := fetch(tc.credential, tc.id, tc.lease, tc.path, "")
		r.Body.Close()
		if r.StatusCode == 200 {
			t.Fatal("unauthorized APK access")
		}
	}
	if err = s.RemoveApplication(ctx, "site", a.SHA256); !errors.Is(err, ports.ErrReferenced) {
		t.Fatal("removed referenced artifact", err)
	}
	// Restart preserves the exact sync receipt and lease; a lost response cannot claim new work.
	server.Close()
	s.Close()
	s2, err := kylok.New(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	client, _ = start(t, s2)
	replay := exchange(t, client, id, credential, 1, 0)
	if !proto.Equal(snapshot, replay) {
		t.Fatal("sync replay changed after restart")
	}
	bad := &v1.OperationReport{Id: "report-mismatch", OperationId: op.ID, LeaseId: leased.LeaseId, Sequence: 1, State: v1.OperationState_OPERATION_STATE_SUCCEEDED,
		Observed: &v1.InstalledApplication{PackageName: a.PackageName, VersionCode: a.VersionCode - 1, SigningCertificates: a.SigningCertificates}}
	mismatch := exchange(t, client, id, credential, 2, 1, bad)
	if mismatch.Receipts[0].Accepted || mismatch.Receipts[0].Reason != "OBSERVATION_MISMATCH" {
		t.Fatal("false success accepted")
	}
	pending := &v1.OperationReport{Id: "report-user", OperationId: op.ID, LeaseId: leased.LeaseId, Sequence: 2, State: v1.OperationState_OPERATION_STATE_WAITING_USER}
	waiting := exchange(t, client, id, credential, 3, 1, pending)
	if waiting.Compliance[0].Reason != "USER_ACTION_REQUIRED" {
		t.Fatal("pending user marked success")
	}
	done := &v1.OperationReport{Id: "report-done", OperationId: op.ID, LeaseId: leased.LeaseId, Sequence: 3, State: v1.OperationState_OPERATION_STATE_SUCCEEDED, DownloadedBytes: a.SizeBytes,
		Observed: &v1.InstalledApplication{PackageName: a.PackageName, VersionCode: a.VersionCode, SigningCertificates: a.SigningCertificates}}
	finished := exchange(t, client, id, credential, 4, 1, done)
	if !finished.Receipts[0].Accepted || len(finished.Operations) != 0 {
		t.Fatal("terminal report not persisted")
	}
	ops, err := s2.ListOperations(ctx, "site", id, "", 100)
	if err != nil || len(ops) != 1 || ops[0].State != "SUCCEEDED" {
		t.Fatal("operation result", ops, err)
	}
	// Observed inventory drives compliance independently of an install operation result.
	request := syncRequest(id, credential)
	request.Msg.Sequence = 5
	request.Msg.ReportedRevision = 1
	request.Msg.Inventory.Applications = []*v1.InstalledApplication{done.Observed}
	observed, err := client.Sync(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Msg.Compliance[0].State != v1.ComplianceState_COMPLIANCE_STATE_APPLIED {
		t.Fatal("observed compliance not applied")
	}
	drift := exchange(t, client, id, credential, 6, 1)
	if len(drift.Operations) != 1 || drift.Operations[0].Id == op.ID || drift.Operations[0].Revision != 1 {
		t.Fatal("observed drift did not reconcile into new work")
	}
	if _, err = s2.ListOperations(ctx, "other", id, "", 100); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("cross scope operations", err)
	}
	if err = s2.RevokeDevice(ctx, "site", id); err != nil {
		t.Fatal(err)
	}
	request.Msg.Sequence = 7
	_, err = client.Sync(ctx, request)
	reason(t, err, v1.ErrorReason_ERROR_REASON_DEVICE_REVOKED)
}

func TestLeaseReclaimSupersessionStaleReportsAndDeadlines(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, _ := setup(t, func() time.Time { return now })
	client, _ := start(t, s)
	id, credential := enrolled(t, s, client, "site")
	apk := testapk.Make(t, testapk.Manifest)
	a, err := s.RegisterApplication(ctx, "site", apk)
	if err != nil {
		t.Fatal(err)
	}
	op, err := s.AssignApplication(ctx, "site", id, a.SHA256, "first", 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	first := exchange(t, client, id, credential, 1, 0).Operations[0]
	now = now.Add(31 * time.Minute)
	next := exchange(t, client, id, credential, 2, 1).Operations[0]
	if first.LeaseId == next.LeaseId {
		t.Fatal("expired lease not reclaimed")
	}
	stale := &v1.OperationReport{Id: "stale-report", OperationId: op.ID, LeaseId: first.LeaseId, Sequence: 1, State: v1.OperationState_OPERATION_STATE_RUNNING}
	result := exchange(t, client, id, credential, 3, 1, stale)
	if result.Receipts[0].Reason != "STALE_LEASE" {
		t.Fatal("stale lease accepted")
	}
	secondAPK := testapk.Make(t, strings.Replace(testapk.Manifest, `android:versionCode="3"`, `android:versionCode="4"`, 1))
	secondArtifact, err := s.RegisterApplication(ctx, "site", secondAPK)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AssignApplication(ctx, "site", id, secondArtifact.SHA256, "first", time.Hour); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("idempotency conflict not rejected", err)
	}
	second, err := s.AssignApplication(ctx, "site", id, secondArtifact.SHA256, "second", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	superseded := exchange(t, client, id, credential, 4, 1, &v1.OperationReport{Id: "old-completion", OperationId: op.ID, LeaseId: next.LeaseId, Sequence: 2, State: v1.OperationState_OPERATION_STATE_RUNNING})
	if superseded.Receipts[0].Reason != "SUPERSEDED" || superseded.Device.DesiredRevision != 2 || len(superseded.Operations) != 1 || superseded.Operations[0].Id != second.ID {
		t.Fatal("supersession")
	}
	now = now.Add(time.Minute)
	expired := exchange(t, client, id, credential, 5, 2)
	if len(expired.Operations) != 0 || expired.Compliance[0].State != v1.ComplianceState_COMPLIANCE_STATE_FAILED || expired.Compliance[0].Reason != "DEADLINE_EXCEEDED" {
		t.Fatal("deadline")
	}
	// Cached response replay is bound to the complete payload.
	request := syncRequest(id, credential)
	request.Msg.Sequence = 5
	request.Msg.ReportedRevision = 2
	request.Msg.Inventory.Model = "changed"
	_, err = client.Sync(ctx, request)
	reason(t, err, v1.ErrorReason_ERROR_REASON_CONFLICT)
	request.Msg.Sequence = 4
	_, err = client.Sync(ctx, request)
	reason(t, err, v1.ErrorReason_ERROR_REASON_STALE_SEQUENCE)
	if err = s.RemoveApplication(ctx, "site", a.SHA256); err != nil {
		t.Fatal("unreferenced old artifact cannot be removed", err)
	}
}

func TestConcurrentAssignmentsKeepOneRevision(t *testing.T) {
	s, _ := setup(t, nil)
	client, _ := start(t, s)
	id, _ := enrolled(t, s, client, "site")
	a, err := s.RegisterApplication(ctx, "site", testapk.Make(t, testapk.Manifest))
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	ids := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			op, err := s.AssignApplication(ctx, "site", id, a.SHA256, "same-key", time.Hour)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- op.ID
		}()
	}
	wait.Wait()
	close(ids)
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatal("concurrent assignments created duplicates", unique)
	}
	devices, err := s.ListDevices(ctx, "site", "", 100)
	if err != nil || devices[0].DesiredRevision != 1 {
		t.Fatal("revision overcount", devices, err)
	}
}

func TestOperationClaimAttemptsAreBounded(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	s, _ := setup(t, func() time.Time { return now })
	client, _ := start(t, s)
	id, credential := enrolled(t, s, client, "site")
	a, err := s.RegisterApplication(ctx, "site", testapk.Make(t, testapk.Manifest))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AssignApplication(ctx, "site", id, a.SHA256, "bounded", 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	var previousLease string
	for sequence := uint64(1); sequence <= 9; sequence++ {
		revision := uint64(1)
		if sequence == 1 {
			revision = 0
		}
		snapshot := exchange(t, client, id, credential, sequence, revision)
		if sequence <= 8 {
			if len(snapshot.Operations) != 1 || snapshot.Operations[0].LeaseId == previousLease {
				t.Fatal("claim was not renewed", sequence)
			}
			previousLease = snapshot.Operations[0].LeaseId
		} else if len(snapshot.Operations) != 0 || snapshot.Compliance[0].Reason != "ATTEMPTS_EXHAUSTED" {
			t.Fatal("claims did not stop", snapshot)
		}
		now = now.Add(31 * time.Minute)
	}
}
