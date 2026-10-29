// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"strings"
	"testing"

	"connectrpc.com/connect"
	kylokv1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
)

// TestAgentValidationEdgeCases tests inventory validation, non-P256 keys, and sync request validation.
func TestAgentValidationEdgeCases(t *testing.T) {
	sdk, _ := setup(t, nil)
	client, _ := start(t, sdk)
	ctx := context.Background()

	e := issue(t, sdk, "site")

	// Enroll with nil inventory -> InvalidArgument
	req, _ := request(t, e.Token)
	req.Inventory = nil
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error on nil inventory")
	}

	// Enroll with APILevel < 33 -> InvalidArgument
	req, _ = request(t, e.Token)
	req.Inventory.ApiLevel = 32
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error on API level < 33")
	}

	// Enroll with APILevel > 37 -> InvalidArgument
	req, _ = request(t, e.Token)
	req.Inventory.ApiLevel = 38
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error on API level > 37")
	}

	// Enroll with DeviceOwner = false -> InvalidArgument
	req, _ = request(t, e.Token)
	req.Inventory.DeviceOwner = false
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error when DeviceOwner is false")
	}

	// Enroll with empty model -> InvalidArgument
	req, _ = request(t, e.Token)
	req.Inventory.Model = ""
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error on empty model")
	}

	// Enroll with too many capabilities (>32) -> InvalidArgument
	req, _ = request(t, e.Token)
	for i := 0; i < 35; i++ {
		req.Inventory.Capabilities = append(req.Inventory.Capabilities, "cap")
	}
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error on too many capabilities")
	}

	// Enroll with application invalid cert digest -> InvalidArgument
	req, _ = request(t, e.Token)
	req.Inventory.Applications = []*kylokv1.InstalledApplication{
		{
			PackageName:         "com.test",
			VersionCode:         1,
			SigningCertificates: []string{"not-a-valid-sha256"},
		},
	}
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error on invalid cert digest")
	}

	// Enroll with non-P256 key (e.g. P-384) -> InvalidArgument
	p384Key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p384Der, err := x509.MarshalPKIXPublicKey(&p384Key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	req, _ = request(t, e.Token)
	req.PublicKey = p384Der
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error on non-P256 key")
	}

	// Enroll with invalid attempt ID (too short) -> InvalidArgument
	req, _ = request(t, e.Token)
	req.AttemptId = "short"
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error on short attempt ID")
	}

	// Successfully enroll a device for Sync tests
	e2 := issue(t, sdk, "site")
	enrollReq, p256Key := request(t, e2.Token)
	enrollResp, err := client.Enroll(ctx, connect.NewRequest(enrollReq))
	if err != nil {
		t.Fatal(err)
	}
	credential := decrypt(t, p256Key, enrollResp.Msg, enrollReq.AttemptId)
	devID := enrollResp.Msg.Device.Id

	// Sync with ProtocolVersion mismatch -> error
	syncReq := connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion: 999,
		DeviceId:        devID,
		Sequence:        1,
		Inventory:       inventory(),
	})
	syncReq.Header().Set("Authorization", "Bearer "+credential)
	if _, err := client.Sync(ctx, syncReq); err == nil {
		t.Fatal("expected error on protocol mismatch")
	}

	// Sync with invalid auth header -> Unauthenticated
	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion: 1,
		DeviceId:        devID,
		Sequence:        1,
		Inventory:       inventory(),
	})
	syncReq.Header().Set("Authorization", "Basic badauth")
	if _, err := client.Sync(ctx, syncReq); err == nil {
		t.Fatal("expected error on bad auth header")
	}

	// Sync with sequence 0 -> InvalidArgument
	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion: 1,
		DeviceId:        devID,
		Sequence:        0,
		Inventory:       inventory(),
	})
	syncReq.Header().Set("Authorization", "Bearer "+credential)
	if _, err := client.Sync(ctx, syncReq); err == nil {
		t.Fatal("expected error on sequence 0")
	}

	// Sync with duplicate retained operation IDs -> InvalidArgument
	validOpID := strings.Repeat("o", 43)
	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion:      1,
		DeviceId:             devID,
		Sequence:             1,
		Inventory:            inventory(),
		RetainedOperationIds: []string{validOpID, validOpID},
	})
	syncReq.Header().Set("Authorization", "Bearer "+credential)
	if _, err := client.Sync(ctx, syncReq); err == nil {
		t.Fatal("expected error on duplicate retained op ID")
	}

	// Sync with invalid command report (bad dispatch token length != 48) -> InvalidArgument
	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion: 1,
		DeviceId:        devID,
		Sequence:        1,
		Inventory:       inventory(),
		CommandReports: []*kylokv1.CommandReport{
			{
				CommandId:     strings.Repeat("c", 43),
				DispatchToken: "short_token",
				State:         "SUCCEEDED",
			},
		},
	})
	syncReq.Header().Set("Authorization", "Bearer "+credential)
	if _, err := client.Sync(ctx, syncReq); err == nil {
		t.Fatal("expected error on invalid command dispatch token")
	}

	// Inventory with app VersionCode < 1 -> InvalidArgument
	req, _ = request(t, e.Token)
	req.Inventory.Applications = []*kylokv1.InstalledApplication{
		{
			PackageName:         "com.example.badver",
			VersionCode:         0,
			SigningCertificates: []string{strings.Repeat("a", 64)},
		},
	}
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error on VersionCode < 1")
	}

	// Inventory with capability > 64 chars -> InvalidArgument
	req, _ = request(t, e.Token)
	req.Inventory.Capabilities = []string{strings.Repeat("c", 65)}
	if _, err := client.Enroll(ctx, connect.NewRequest(req)); err == nil {
		t.Fatal("expected error on capability > 64 chars")
	}

	// Cancelled context in Enroll -> CodeCanceled
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	req, _ = request(t, e.Token)
	if _, err := client.Enroll(cancelledCtx, connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("expected CodeCanceled, got %v", err)
	}

	// Cancelled context in Sync -> CodeCanceled
	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion: 1,
		DeviceId:        devID,
		Sequence:        1,
		Inventory:       inventory(),
	})
	syncReq.Header().Set("Authorization", "Bearer "+credential)
	if _, err := client.Sync(cancelledCtx, syncReq); connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("expected CodeCanceled, got %v", err)
	}

	// Revoke device and test Sync returns ErrorReason_ERROR_REASON_DEVICE_REVOKED
	if err := sdk.RevokeDevice(ctx, "site", devID); err != nil {
		t.Fatal(err)
	}
	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion: 1,
		DeviceId:        devID,
		Sequence:        1,
		Inventory:       inventory(),
	})
	syncReq.Header().Set("Authorization", "Bearer "+credential)
	_, err = client.Sync(ctx, syncReq)
	reason(t, err, kylokv1.ErrorReason_ERROR_REASON_DEVICE_REVOKED)

	// Additional agent validation edge cases:
	// 1. CommandReport with Observation (covers lines 248-250)
	// Enroll a new device on sdk
	e3 := issue(t, sdk, "site")
	req3, key3 := request(t, e3.Token)
	resp3, err := client.Enroll(ctx, connect.NewRequest(req3))
	if err != nil {
		t.Fatal(err)
	}
	cred3 := decrypt(t, key3, resp3.Msg, req3.AttemptId)
	devID3 := resp3.Msg.Device.Id

	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion: 1,
		DeviceId:        devID3,
		Sequence:        1,
		Inventory:       inventory(),
		CommandReports: []*kylokv1.CommandReport{
			{
				CommandId:     strings.Repeat("c", 43),
				DispatchToken: strings.Repeat("t", 48),
				State:         "SUCCEEDED",
				Observation: &kylokv1.CommandObservation{
					BootBefore: 1,
					BootAfter:  2,
					Secure:     true,
					Locked:     true,
				},
			},
		},
	})
	syncReq.Header().Set("Authorization", "Bearer "+cred3)
	_, _ = client.Sync(ctx, syncReq)

	// 2. NetworkReport with revision > ReportedRevision -> InvalidInput
	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion:  1,
		DeviceId:         devID3,
		Sequence:         2,
		ReportedRevision: 1,
		Inventory:        inventory(),
		NetworkReport: &kylokv1.NetworkReport{
			Revision: 2,
		},
	})
	syncReq.Header().Set("Authorization", "Bearer "+cred3)
	if _, err := client.Sync(ctx, syncReq); err == nil {
		t.Fatal("expected error on NetworkReport revision > ReportedRevision")
	}

	// 3. PolicyReport with revision > SnapshotRevision -> InvalidInput
	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion:  1,
		DeviceId:         devID3,
		Sequence:         2,
		ReportedRevision: 1,
		Inventory:        inventory(),
		PolicyReport: &kylokv1.PolicyReport{
			Revision:         5,
			SnapshotRevision: 2,
		},
	})
	syncReq.Header().Set("Authorization", "Bearer "+cred3)
	if _, err := client.Sync(ctx, syncReq); err == nil {
		t.Fatal("expected error on PolicyReport revision > SnapshotRevision")
	}

	// 4. PolicyReport with empty RuleId -> InvalidInput
	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion:  1,
		DeviceId:         devID3,
		Sequence:         2,
		ReportedRevision: 1,
		Inventory:        inventory(),
		PolicyReport: &kylokv1.PolicyReport{
			Revision:         1,
			SnapshotRevision: 1,
			Results: []*kylokv1.PolicyResult{
				{RuleId: "", State: kylokv1.ComplianceState_COMPLIANCE_STATE_APPLIED},
			},
		},
	})
	syncReq.Header().Set("Authorization", "Bearer "+cred3)
	if _, err := client.Sync(ctx, syncReq); err == nil {
		t.Fatal("expected error on empty RuleId in PolicyReport")
	}

	// 5. Canceled and DeadlineExceeded contexts
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	syncReq = connect.NewRequest(&kylokv1.SyncRequest{
		ProtocolVersion:  1,
		DeviceId:         devID3,
		Sequence:         2,
		ReportedRevision: 1,
		Inventory:        inventory(),
	})
	syncReq.Header().Set("Authorization", "Bearer "+cred3)
	if _, err := client.Sync(canceledCtx, syncReq); err == nil {
		t.Fatal("expected error on canceled context")
	}

	// 6. Additional Inventory bounds
	for _, mutate := range []func(inv *kylokv1.Inventory){
		func(inv *kylokv1.Inventory) { inv.BuildFingerprint = strings.Repeat("f", 1025) },
		func(inv *kylokv1.Inventory) { inv.AgentVersion = "" },
		func(inv *kylokv1.Inventory) { inv.AgentVersion = strings.Repeat("v", 129) },
		func(inv *kylokv1.Inventory) { inv.SupportedAbis = []string{strings.Repeat("a", 65)} },
		func(inv *kylokv1.Inventory) { inv.Capabilities = []string{""} },
		func(inv *kylokv1.Inventory) {
			inv.Applications = []*kylokv1.InstalledApplication{
				{PackageName: "app1", VersionCode: 1, SigningCertificates: nil},
			}
		},
		func(inv *kylokv1.Inventory) {
			inv.Applications = []*kylokv1.InstalledApplication{
				{PackageName: "app1", VersionCode: 1, SigningCertificates: []string{"bad_hex"}},
			}
		},
		func(inv *kylokv1.Inventory) {
			inv.Applications = []*kylokv1.InstalledApplication{
				{PackageName: strings.Repeat("p", 257), VersionCode: 1, SigningCertificates: []string{strings.Repeat("a", 64)}},
			}
		},
		func(inv *kylokv1.Inventory) {
			inv.Applications = []*kylokv1.InstalledApplication{
				{PackageName: "app1", VersionCode: 1, SigningCertificates: []string{strings.Repeat("a", 64)}},
				{PackageName: "app1", VersionCode: 2, SigningCertificates: []string{strings.Repeat("a", 64)}},
			}
		},
	} {
		inv := inventory()
		mutate(inv)
		syncReq = connect.NewRequest(&kylokv1.SyncRequest{
			ProtocolVersion:  1,
			DeviceId:         devID3,
			Sequence:         2,
			ReportedRevision: 1,
			Inventory:        inv,
		})
		syncReq.Header().Set("Authorization", "Bearer "+cred3)
		if _, err := client.Sync(ctx, syncReq); err == nil {
			t.Fatal("expected error on invalid inventory mutation")
		}
	}
}


