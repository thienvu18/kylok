// SPDX-License-Identifier: LGPL-3.0-only
package consumer_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
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
			enrollment, err := sdk.CreateEnrollment(ctx, "consumer", time.Minute)
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
			a, err := sdk.RegisterApplication(ctx, "consumer", apk)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = sdk.AssignApplication(ctx, "consumer", devices[0].ID, a.SHA256, "consumer-install", time.Hour); err != nil {
				t.Fatal(err)
			}
			if _, err = sdk.SetPolicy(ctx, "consumer", devices[0].ID, "consumer-policy", kylok.Policy{RequiredPackages: []string{a.PackageName}, LockTask: &kylok.LockTaskPolicy{Packages: []string{a.PackageName}, KioskPackage: a.PackageName}}); err != nil {
				t.Fatal(err)
			}
			status, err := sdk.GetPolicy(ctx, "consumer", devices[0].ID)
			if err != nil || status.Desired.Revision != 2 {
				t.Fatal(status, err)
			}
			select {
			case e := <-events:
				if e.Kind != "device_enrolled" || e.DeviceID != devices[0].ID {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("event hook did not receive committed identity")
			}
			if err = sdk.RevokeDevice(ctx, "consumer", devices[0].ID); err != nil {
				t.Fatal(err)
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
}
