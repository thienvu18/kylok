// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/thienvu18/kylok/go/internal/testapk"
	"github.com/thienvu18/kylok/go/ports"
)

func TestQRSelectedOriginsPreserveTokenAndVerifiedArtifact(t *testing.T) {
	s, _ := setup(t, time.Now)
	enrollment := issue(t, s, "site")
	for _, origin := range []string{"http://192.168.1.5:8080", "https://mdm.example.com", "http://[2001:db8::1]:8080"} {
		payload, err := s.QRWithOrigins(ctx, enrollment.Token, origin, origin)
		if err != nil {
			t.Fatal(err)
		}
		var qr map[string]any
		if err = json.Unmarshal(payload, &qr); err != nil {
			t.Fatal(err)
		}
		extras := qr["android.app.extra.PROVISIONING_ADMIN_EXTRAS_BUNDLE"].(map[string]any)
		if extras["kylok.server"] != origin || extras["kylok.enrollment_token"] != enrollment.Token {
			t.Fatal("QR rewrite lost enrollment binding", extras)
		}
		if qr["android.app.extra.PROVISIONING_DEVICE_ADMIN_PACKAGE_CHECKSUM"] == "" {
			t.Fatal("missing verified checksum")
		}
	}
	for _, origin := range []string{"http://user:password@mdm.test", "https://mdm.test/path", "file:///tmp/host"} {
		if _, err := s.QRWithOrigins(ctx, enrollment.Token, origin, "http://192.168.1.5"); err == nil {
			t.Fatal("invalid origin accepted", origin)
		}
	}
}

func TestManagedCatalogPaginationIsScopedAndExcludesBootstrap(t *testing.T) {
	s, _ := setup(t, time.Now)
	apps, err := s.ListApplications(ctx, "site", "", 100)
	if err != nil || len(apps) != 0 {
		t.Fatal(apps, err)
	}
	a, err := s.RegisterApplication(ctx, "site", testapk.Make(t, testapk.Manifest))
	if err != nil {
		t.Fatal(err)
	}
	apps, err = s.ListApplications(ctx, "site", "", 1)
	if err != nil || len(apps) != 1 || apps[0].SHA256 != a.SHA256 {
		t.Fatal(apps, err)
	}
	apps, err = s.ListApplications(ctx, "site", a.SHA256, 1)
	if err != nil || len(apps) != 0 {
		t.Fatal("cursor repeated artifact", apps, err)
	}
	apps, err = s.ListApplications(ctx, "other", "", 100)
	if err != nil || len(apps) != 0 {
		t.Fatal("cross-scope catalog", apps, err)
	}
	if _, err = s.ListApplications(ctx, "", "", 100); err == nil || errors.Is(err, ports.ErrNotFound) {
		t.Fatal("missing scope not validated", err)
	}
}
