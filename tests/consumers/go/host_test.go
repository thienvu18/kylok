// SPDX-License-Identifier: LGPL-3.0-only
package consumer_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/thienvu18/kylok/go"
	"github.com/thienvu18/kylok/go/adapters/filesystem"
	"github.com/thienvu18/kylok/go/adapters/sqlite"
	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/gen/kylok/v1/kylokv1connect"
	"github.com/thienvu18/kylok/go/ports"
	"github.com/thienvu18/kylok/go/ports/portstest"
	"github.com/thienvu18/kylok/go/provisioning"
)

func TestFreshHostPublicAPIsAndCustomProviderOwnership(t *testing.T) {
	ctx := context.Background()
	apk, err := provisioning.Inspect(ctx, os.Getenv("KYLOK_CONSUMER_APK"), provisioning.Tools{})
	if err != nil {
		t.Fatal(err)
	}
	if kylok.Version() != os.Getenv("KYLOK_GO_VERSION") || kylok.ProtocolVersion != 1 {
		t.Fatal("wrong packaged SDK identity")
	}
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "defaults", true: "custom-providers"}[custom], func(t *testing.T) {
			opts := kylok.Options{PublicURL: "http://127.0.0.1", Bootstrap: apk, DataDir: t.TempDir()}
			var store *sqlite.Store
			if custom {
				store, err = sqlite.Open(ctx, filepath.Join(opts.DataDir, "host.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				artifacts, err := filesystem.Open(filepath.Join(opts.DataDir, "host-artifacts"))
				if err != nil {
					t.Fatal(err)
				}
				opts.Store, opts.Artifacts = store, artifacts
				opts.DataDir = ""
			}
			events := make(chan kylok.Event, 16)
			opts.Events = func(ctx context.Context, e kylok.Event) error {
				select {
				case events <- e:
				case <-ctx.Done():
				}
				return nil
			}
			sdk, err := kylok.New(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer sdk.Close()
			protocols := new(http.Protocols)
			protocols.SetHTTP1(true)
			protocols.SetUnencryptedHTTP2(true)
			server := httptest.NewUnstartedServer(sdk.Handler())
			server.Config.Protocols = protocols
			server.Start()
			defer server.Close()
			enrollment, err := sdk.CreateEnrollmentWithOptions(ctx, kylok.EnrollmentOptions{ScopeID: "consumer", TTL: time.Minute, CorrelationID: "host-intent"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = sdk.QR(ctx, enrollment.Token); err != nil {
				t.Fatal(err)
			}
			key, err := ecdh.P256().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			public, err := x509.MarshalPKIXPublicKey(key.PublicKey())
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{Protocols: protocols}
			defer transport.CloseIdleConnections()
			client := kylokv1connect.NewAgentServiceClient(&http.Client{Transport: transport}, server.URL, connect.WithGRPC())
			response, err := client.Enroll(ctx, connect.NewRequest(&v1.EnrollRequest{Token: enrollment.Token, AttemptId: "fresh-consumer-attempt", PublicKey: public, ProtocolVersion: kylok.ProtocolVersion,
				Inventory: &v1.Inventory{ApiLevel: 35, Model: "consumer fixture", AgentVersion: "consumer", DeviceOwner: true}}))
			if err != nil {
				t.Fatal(err)
			}
			devices, err := sdk.ListDevices(ctx, "consumer", "", 10)
			if err != nil || len(devices) != 1 || devices[0].ID != response.Msg.Device.Id {
				t.Fatal(devices, err)
			}
			record, err := sdk.GetDevice(ctx, "consumer", devices[0].ID)
			if err != nil || record.CorrelationID != "host-intent" {
				t.Fatal(record, err)
			}
			page, err := sdk.QueryDevices(ctx, "consumer", kylok.DeviceFilter{CorrelationID: "host-intent"}, kylok.PageRequest{})
			if err != nil || len(page.Items) != 1 {
				t.Fatal(page, err)
			}
			if _, err = sdk.GetDevice(ctx, "foreign", record.ID); !errors.Is(err, kylok.ErrNotFound) {
				t.Fatal(err)
			}
			if err = sdk.Ready(ctx); err != nil {
				t.Fatal(err)
			}
			a, err := sdk.RegisterApplication(ctx, "consumer", apk)
			if err != nil {
				t.Fatal(err)
			}
			operation, err := sdk.AssignApplication(ctx, "consumer", devices[0].ID, a.SHA256, "consumer-install", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = sdk.GetOperation(ctx, "consumer", record.ID, operation.ID); err != nil {
				t.Fatal(err)
			}
			display := "consumer fixture"
			configuration := kylok.ManagedConfiguration{PackageName: a.PackageName, Values: kylok.ConfigurationBundle{Entries: []kylok.ConfigurationEntry{{Key: "display_name", Value: kylok.ConfigurationValue{String: &display}}}}}
			if _, err = sdk.SetPolicy(ctx, "consumer", devices[0].ID, "consumer-policy", kylok.Policy{RequiredPackages: []string{a.PackageName}, ManagedConfigurations: []kylok.ManagedConfiguration{configuration}, LockTask: &kylok.LockTaskPolicy{Packages: []string{a.PackageName}, KioskPackage: a.PackageName}}); err != nil {
				t.Fatal(err)
			}
			status, err := sdk.GetPolicy(ctx, "consumer", devices[0].ID)
			if err != nil || status.Desired.Revision != 2 {
				t.Fatal(status, err)
			}
			group, err := sdk.ReplaceGroup(ctx, "consumer", "consumer-group", []string{record.ID}, 0)
			if err != nil || group.Revision != 1 {
				t.Fatal(group, err)
			}
			now := time.Now().UTC().Truncate(time.Millisecond)
			batch, err := sdk.CreateFleetAssignment(ctx, kylok.FleetRequest{ScopeID: "consumer", ID: "consumer-batch", GroupID: group.ID, ArtifactSHA256: a.SHA256, NotBefore: now, Deadline: now.Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			batch, err = sdk.AdvanceFleetAssignment(ctx, "consumer", batch.Request.ID, 20)
			if err != nil || !batch.Complete || batch.Items[0].State != "ASSIGNED" {
				t.Fatal(batch, err)
			}
			if batch, err = sdk.CancelFleetAssignment(ctx, "consumer", batch.Request.ID); err != nil || !batch.Cancelled {
				t.Fatal(batch, err)
			}
			scheduled, err := sdk.ScheduleApplication(ctx, "consumer", record.ID, a.SHA256, "consumer-window", now.Add(time.Hour), now.Add(2*time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if cancelled, err := sdk.CancelOperation(ctx, "consumer", record.ID, scheduled.ID); err != nil || cancelled.State != "CANCELLED" {
				t.Fatal(cancelled, err)
			}
			if _, err = sdk.SetNetworkPolicy(ctx, "consumer", record.ID, "consumer-network", kylok.NetworkPolicy{PrimaryID: "office", Profiles: []kylok.WifiProfile{{ID: "office", SSID: "Office", Security: "OPEN"}}}); err != nil {
				t.Fatal(err)
			}
			if network, err := sdk.GetNetworkPolicy(ctx, "consumer", record.ID); err != nil || network.Desired.Policy.PrimaryID != "office" || network.State != "UNSUPPORTED" {
				t.Fatal(network, err)
			}
			if _, err = sdk.SubmitCommand(ctx, "consumer", record.ID, kylok.CommandReboot, "unsupported-command", time.Hour); !errors.Is(err, kylok.ErrUnsupported) {
				t.Fatal("missing capability accepted", err)
			}
			if commands, err := sdk.QueryCommands(ctx, "consumer", record.ID, kylok.PageRequest{}); err != nil || len(commands.Items) != 0 {
				t.Fatal(commands, err)
			}
			select {
			case e := <-events:
				if e.Kind != "device_enrolled" || e.DeviceID != devices[0].ID {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("event hook did not receive committed identity")
			}
			projection, err := sdk.ReadProjection(ctx, "consumer", "", 100)
			if err != nil || len(projection.Items) != 4 || projection.NextCursor != "" {
				t.Fatal(projection, err)
			}
			if _, err = sdk.ListChanges(ctx, "foreign", projection.ChangesCursor, 100); !errors.Is(err, kylok.ErrInvalidArgument) {
				t.Fatal(err)
			}
			if err = sdk.RevokeDevice(ctx, "consumer", devices[0].ID); err != nil {
				t.Fatal(err)
			}
			changes, err := sdk.ListChanges(ctx, "consumer", projection.ChangesCursor, 100)
			if err != nil || len(changes.Items) != 1 || changes.Items[0].ResourceID != record.ID {
				t.Fatal(changes, err)
			}
			observed, err := sdk.GetDeviceState(ctx, "consumer", record.ID)
			if err != nil || observed.Device.State != "REVOKED" || len(observed.Policy.Desired.Policy.ManagedConfigurations) != 1 {
				t.Fatal(observed, err)
			}
			server.Close()
			sdk.Close()
			if custom {
				if _, err = store.ListDevices(ctx, "consumer", "", 10); err != nil {
					t.Fatal("SDK closed host store", err)
				}
			}
		})
	}
}

func TestCustomAdapterConformanceFromModule(t *testing.T) {
	factory := func(t *testing.T) ports.StateStore {
		s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "state.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	portstest.State(t, factory)
	portstest.Operations(t, factory)
	portstest.Policies(t, factory)
	portstest.ManagedConfigurations(t, factory)
	portstest.Management(t, factory)
	portstest.Changes(t, factory)
	portstest.Fleet(t, factory)
	portstest.Commands(t, factory)
	portstest.Network(t, factory)
}
