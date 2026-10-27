// SPDX-License-Identifier: LGPL-3.0-only
package hosting

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/thienvu18/kylok/go"
	v1 "github.com/thienvu18/kylok/go/gen/kylok/v1"
	"github.com/thienvu18/kylok/go/gen/kylok/v1/kylokv1connect"
	"github.com/thienvu18/kylok/go/provisioning"
)

type projectionSink struct {
	cursor       string
	devices      map[string]kylok.DeviceState
	applications map[string]kylok.Artifact
	failCommit   bool
}

func (s *projectionSink) Checkpoint(ctx context.Context, scope string) (string, error) {
	return s.cursor, nil
}
func (s *projectionSink) Replace(ctx context.Context, scope string, items []kylok.ProjectionItem, cursor string) error {
	if s.failCommit {
		return errors.New("host transaction failed")
	}
	s.devices = map[string]kylok.DeviceState{}
	s.applications = map[string]kylok.Artifact{}
	return s.Apply(ctx, scope, items, nil, cursor)
}
func (s *projectionSink) Apply(ctx context.Context, scope string, items []kylok.ProjectionItem, removed []string, cursor string) error {
	if s.failCommit {
		return errors.New("host transaction failed")
	}
	for _, item := range items {
		if item.Device != nil {
			s.devices[item.Device.Device.ID] = *item.Device
		}
		if item.Application != nil {
			s.applications[item.Application.SHA256] = *item.Application
		}
	}
	for _, digest := range removed {
		delete(s.applications, digest)
	}
	s.cursor = cursor
	return nil
}
func TestProjectionRestartsAfterMissedCallbacksAndFailedHostCommit(t *testing.T) {
	path := os.Getenv("KYLOK_CONSOLE_APK")
	if path == "" {
		t.Skip("run make test-console to exercise a real signed APK")
	}
	ctx := context.Background()
	apk, err := provisioning.Inspect(ctx, path, provisioning.Tools{})
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := kylok.New(ctx, kylok.Options{PublicURL: "http://127.0.0.1:8080", DataDir: t.TempDir(), Bootstrap: apk, AllowDebugAPK: true, Events: func(context.Context, kylok.Event) error { return errors.New("notifications unavailable") }})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	sink := &projectionSink{}
	projector := Projector{SDK: sdk, Sink: sink}
	if err = projector.RunOnce(ctx, "scope"); err != nil || sink.cursor == "" {
		t.Fatal(err)
	}
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := httptest.NewUnstartedServer(sdk.Handler())
	server.Config.Protocols = protocols
	server.Start()
	defer server.Close()
	transport := &http.Transport{Protocols: protocols}
	defer transport.CloseIdleConnections()
	client := kylokv1connect.NewAgentServiceClient(&http.Client{Transport: transport}, server.URL, connect.WithGRPC())
	ticket, err := (SDKRegistry{SDK: sdk}).Create(ctx, "scope", "host-association")
	if err != nil {
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
	enrolled, err := client.Enroll(ctx, connect.NewRequest(&v1.EnrollRequest{Token: ticket.Token, AttemptId: "host-projector-attempt", PublicKey: public, ProtocolVersion: 1, Inventory: &v1.Inventory{ApiLevel: 35, DeviceOwner: true, Model: "projection fixture", AgentVersion: "projection-test"}}))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := sdk.RegisterApplication(ctx, "scope", apk)
	if err != nil {
		t.Fatal(err)
	}
	if err = projector.RunOnce(ctx, "scope"); err != nil || len(sink.devices) != 1 || len(sink.applications) != 1 {
		t.Fatal(sink, err)
	}
	checkpoint := sink.cursor
	if err = sdk.RevokeDevice(ctx, "scope", enrolled.Msg.Device.Id); err != nil {
		t.Fatal(err)
	}
	sink.failCommit = true
	if err = projector.RunOnce(ctx, "scope"); err == nil || sink.cursor != checkpoint || sink.devices[enrolled.Msg.Device.Id].Device.State == "REVOKED" {
		t.Fatal("failed host commit advanced state", sink, err)
	}
	sink.failCommit = false
	restarted := Projector{SDK: sdk, Sink: sink}
	if err = restarted.RunOnce(ctx, "scope"); err != nil || sink.cursor == checkpoint || sink.devices[enrolled.Msg.Device.Id].Device.State != "REVOKED" {
		t.Fatal(sink, err)
	}
	if err = sdk.RemoveApplication(ctx, "scope", artifact.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err = sdk.RegisterApplication(ctx, "scope", apk); err != nil {
		t.Fatal(err)
	}
	if err = restarted.RunOnce(ctx, "scope"); err != nil || len(sink.applications) != 1 {
		t.Fatal("tombstone erased re-registered catalog", sink, err)
	}
}
