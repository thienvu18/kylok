// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/thienvu18/kylok/go"
	"github.com/thienvu18/kylok/go/gen/kylok/v1/kylokv1connect"
	"github.com/thienvu18/kylok/go/internal/testapk"
)

func TestManagementCorrelationRecoveryAndScope(t *testing.T) {
	sdk, options := setup(t, nil)
	client, server := start(t, sdk)
	created, err := sdk.CreateEnrollmentWithOptions(ctx, kylok.EnrollmentOptions{ScopeID: "site", TTL: time.Minute, CorrelationID: "host-intent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sdk.CreateEnrollmentWithOptions(ctx, kylok.EnrollmentOptions{ScopeID: "site", TTL: time.Minute, CorrelationID: "host-intent"}); !errors.Is(err, kylok.ErrConflict) {
		t.Fatal(err)
	}
	req, _ := request(t, created.Token)
	first, err := client.Enroll(ctx, connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Enroll(ctx, connect.NewRequest(req))
	if err != nil || first.Msg.Device.Id != second.Msg.Device.Id {
		t.Fatal(second, err)
	}
	value, err := sdk.GetDevice(ctx, "site", first.Msg.Device.Id)
	if err != nil || value.CorrelationID != created.CorrelationID {
		t.Fatal(value, err)
	}
	if _, err = sdk.GetDevice(ctx, "other", value.ID); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal(err)
	}
	result, err := sdk.QueryDevices(ctx, "site", kylok.DeviceFilter{CorrelationID: "host-intent"}, kylok.PageRequest{})
	if err != nil || len(result.Items) != 1 || result.NextCursor != "" {
		t.Fatal(result, err)
	}
	data, _ := json.Marshal(result)
	for _, secret := range []string{created.Token, "credential", "token_hash", "sync_snapshot"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("public data leaked", secret)
		}
	}
	server.Close()
	sdk.Close()
	reopened, err := kylok.New(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	value, err = reopened.GetDevice(ctx, "site", value.ID)
	if err != nil || value.CorrelationID != "host-intent" {
		t.Fatal(value, err)
	}
	if _, err = sdk.GetDevice(ctx, "site", value.ID); !errors.Is(err, kylok.ErrClosed) {
		t.Fatal(err)
	}
}

func TestManagementKeysetPaginationAndInvalidCursor(t *testing.T) {
	sdk, _ := setup(t, nil)
	client, _ := start(t, sdk)
	ids := []string{}
	for range 4 {
		id, _ := enrolled(t, sdk, client, "site")
		ids = append(ids, id)
	}
	slices.Sort(ids)
	first, err := sdk.QueryDevices(ctx, "site", kylok.DeviceFilter{}, kylok.PageRequest{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	// A concurrent insertion may be on either side of the cursor. A live keyset scan
	// must preserve order and never repeat IDs that were already returned.
	newID, _ := enrolled(t, sdk, client, "site")
	second, err := sdk.QueryDevices(ctx, "site", kylok.DeviceFilter{}, kylok.PageRequest{Limit: 100, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]string{}, ids[2:]...)
	if newID > first.Items[1].ID {
		expected = append(expected, newID)
		slices.Sort(expected)
	}
	got := []string{}
	for _, item := range second.Items {
		got = append(got, item.ID)
	}
	if !slices.Equal(expected, got) || second.NextCursor != "" {
		t.Fatal(expected, got)
	}
	for _, check := range []struct {
		scope  string
		filter kylok.DeviceFilter
		page   kylok.PageRequest
	}{
		{"other", kylok.DeviceFilter{}, kylok.PageRequest{Cursor: first.NextCursor}},
		{"site", kylok.DeviceFilter{State: "ENROLLED"}, kylok.PageRequest{Cursor: first.NextCursor}},
		{"site", kylok.DeviceFilter{}, kylok.PageRequest{Cursor: "invalid"}},
		{"site", kylok.DeviceFilter{}, kylok.PageRequest{Cursor: strings.Repeat("x", 2000)}},
		{"site", kylok.DeviceFilter{}, kylok.PageRequest{Limit: -1}},
		{"site", kylok.DeviceFilter{}, kylok.PageRequest{Limit: 101}},
		{"site", kylok.DeviceFilter{State: "invented"}, kylok.PageRequest{}},
	} {
		if _, err = sdk.QueryDevices(ctx, check.scope, check.filter, check.page); !errors.Is(err, kylok.ErrInvalidArgument) {
			t.Fatal(check, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = sdk.QueryDevices(cancelled, "site", kylok.DeviceFilter{}, kylok.PageRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestManagementOperationProjectionAndDeadline(t *testing.T) {
	now := time.Now().UTC()
	sdk, _ := setup(t, func() time.Time { return now })
	client, _ := start(t, sdk)
	id, credential := enrolled(t, sdk, client, "site")
	artifact, err := sdk.RegisterApplication(ctx, "site", testapk.Make(t, testapk.Manifest))
	if err != nil {
		t.Fatal(err)
	}
	op, err := sdk.AssignApplication(ctx, "site", id, artifact.SHA256, "request", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	exchange(t, client, id, credential, 1, 0)
	value, err := sdk.GetOperation(ctx, "site", id, op.ID)
	if err != nil || value.State != "RUNNING" {
		t.Fatal(value, err)
	}
	data, _ := json.Marshal(value)
	for _, internal := range []string{"lease_id", "lease_until", "report_sequence", "attempts"} {
		if strings.Contains(string(data), internal) {
			t.Fatal("operation leaked", internal)
		}
	}
	if _, err = sdk.GetOperation(ctx, "other", id, op.ID); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal(err)
	}
	another, _ := enrolled(t, sdk, client, "site")
	if _, err = sdk.GetOperation(ctx, "site", another, op.ID); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal(err)
	}
	page, err := sdk.QueryOperations(ctx, "site", id, kylok.OperationFilter{State: "RUNNING"}, kylok.PageRequest{})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	now = now.Add(2 * time.Minute)
	value, err = sdk.GetOperation(ctx, "site", id, op.ID)
	if err != nil || value.ErrorCode != "DEADLINE_EXCEEDED" {
		t.Fatal(value, err)
	}
}

func TestManagementReadyAndHostRoutes(t *testing.T) {
	for _, secure := range []bool{false, true} {
		sdk, options := setup(t, nil)
		if !secure {
			sdk.Close()
			options.PublicURL = "http://127.0.0.1:8080"
			var err error
			sdk, err = kylok.New(ctx, options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sdk.Close() })
		}
		if err := sdk.Ready(ctx); err != nil {
			t.Fatal(err)
		}
		mux := http.NewServeMux()
		mux.Handle("/kylok.v1.AgentService/", sdk.Handler())
		mux.Handle("/bootstrap/", sdk.Handler())
		mux.Handle("/artifacts/", sdk.Handler())
		mux.HandleFunc("/business", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
		mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
			if err := sdk.Ready(r.Context()); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		})
		server := httptest.NewUnstartedServer(mux)
		server.EnableHTTP2 = true
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		protocols.SetHTTP2(true)
		protocols.SetUnencryptedHTTP2(true)
		server.Config.Protocols = protocols
		if secure {
			server.StartTLS()
		} else {
			server.Start()
		}
		httpClient := server.Client()
		if !secure {
			transport := &http.Transport{Protocols: protocols}
			defer transport.CloseIdleConnections()
			httpClient = &http.Client{Transport: transport}
		}
		agent := kylokv1connect.NewAgentServiceClient(httpClient, server.URL, connect.WithGRPC())
		ticket := issue(t, sdk, "site")
		req, _ := request(t, ticket.Token)
		if _, err := agent.Enroll(ctx, connect.NewRequest(req)); err != nil {
			t.Fatal("agent alongside business routes", err)
		}
		for _, path := range []string{"/prefix/kylok.v1.AgentService/Enroll", "/management/devices"} {
			response, err := httpClient.Get(server.URL + path)
			if err != nil || response.StatusCode != 404 {
				t.Fatal(path, response, err)
			}
			response.Body.Close()
		}
		response, err := httpClient.Get(server.URL + "/business")
		if err != nil || response.StatusCode != 204 {
			t.Fatal(response, err)
		}
		response.Body.Close()
		response, err = server.Client().Get(server.URL + "/ready")
		if err != nil || response.StatusCode != 200 {
			t.Fatal(response, err)
		}
		response.Body.Close()
		sdk.Close()
		if err = sdk.Ready(ctx); !errors.Is(err, kylok.ErrClosed) {
			t.Fatal(err)
		}
		response, err = server.Client().Get(server.URL + "/ready")
		if err != nil || response.StatusCode != 503 {
			t.Fatal(response, err)
		}
		response.Body.Close()
		server.Close()
	}
}
