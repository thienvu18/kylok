// SPDX-License-Identifier: LGPL-3.0-only
package provisioning_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/thienvu18/kylok/go/internal/testapk"
	"github.com/thienvu18/kylok/go/provisioning"
)

func TestOriginRejectsUnsafeAddresses(t *testing.T) {
	for _, url := range []string{"ftp://host", "https://user:pass@host", "https://host/path", "https://host?secret=1", "https://host#fragment", "https://", "https://host?"} {
		if _, err := provisioning.Origin(url); err == nil {
			t.Fatal("accepted", url)
		}
	}
	if origin, err := provisioning.Origin("https://host:8443/"); err != nil || origin != "https://host:8443" {
		t.Fatal(origin, err)
	}
}
func TestVerifiedBytesAndMetadataAreImmutable(t *testing.T) {
	a := testapk.Make(t, testapk.Manifest)
	original := a.Bytes()
	changed := a.Bytes()
	changed[0] ^= 1
	m := a.Metadata()
	m.SigningCertificates[0] = "bad"
	if string(a.Bytes()) != string(original) || a.Metadata().SigningCertificates[0] == "bad" {
		t.Fatal("mutable APK")
	}
}
func TestMissingAndUnsignedAPKRejected(t *testing.T) {
	if _, err := provisioning.Inspect(context.Background(), "/does-not-exist", provisioning.Tools{}); err == nil {
		t.Fatal("missing accepted")
	}
	path := t.TempDir() + "/unsigned.apk"
	os.WriteFile(path, []byte("not signed"), 0600)
	if _, err := provisioning.Inspect(context.Background(), path, provisioning.Tools{APKSigner: "false"}); err == nil {
		t.Fatal("signature failure accepted")
	}
}
func TestRealSignedAPK(t *testing.T) {
	path := os.Getenv("KYLOK_TEST_APK")
	if path == "" {
		t.Skip("real signed APK supplied by make interop")
	}
	apk, err := provisioning.Inspect(context.Background(), path, provisioning.Tools{})
	if err != nil {
		t.Fatal(err)
	}
	m := apk.Metadata()
	if m.MinSDK != 33 || m.TargetSDK != 37 || len(m.SigningCertificates) == 0 || !strings.HasSuffix(m.Receiver, "KylokDeviceAdminReceiver") {
		t.Fatalf("metadata: %+v", m)
	}
	data := apk.Bytes()
	data[len(data)/2] ^= 1
	tampered := t.TempDir() + "/tampered.apk"
	if err := os.WriteFile(tampered, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := provisioning.Inspect(context.Background(), tampered, provisioning.Tools{}); err == nil {
		t.Fatal("real signature verifier accepted tampered APK")
	}
}

func TestMergedManifestValidation(t *testing.T) {
	for _, tt := range []struct{ name, from, to string }{
		{"receiver permission", "android.permission.BIND_DEVICE_ADMIN", "android.permission.INTERNET"},
		{"receiver action", "android.app.action.DEVICE_ADMIN_ENABLED", "android.app.action.OTHER"},
		{"receiver metadata", "android.app.device_admin", "android.app.wrong"},
		{"mode callback", "android.app.action.GET_PROVISIONING_MODE", "android.app.action.OTHER"},
		{"compliance callback", "android.app.action.ADMIN_POLICY_COMPLIANCE", "android.app.action.OTHER"},
		{"hidden receiver", `android:exported="true"`, `android:exported="false"`},
		{"target SDK", `android:targetSdkVersion="37"`, `android:targetSdkVersion="36"`},
		{"version", `android:versionCode="3"`, `android:versionCode="0"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			manifest := strings.Replace(testapk.Manifest, tt.from, tt.to, 1)
			if _, err := testapk.InspectManifest(t, manifest); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestOriginsSupportSchemesAndHosts(t *testing.T) {
	for _, origin := range []string{"http://192.168.101.5:8080", "https://192.168.101.5:8443", "http://mdm.lan:8080", "https://mdm.lan:8443"} {
		if got, err := provisioning.Origin(origin); err != nil || got != origin {
			t.Fatal(origin, got, err)
		}
		apk := testapk.Make(t, testapk.Manifest)
		payload, err := apk.QR(origin, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(payload), origin+apk.Path()) {
			t.Fatal("QR dropped origin", origin)
		}
	}
}

func TestGenericApplicationMetadataAndSplitRejection(t *testing.T) {
	manifest := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="io.test.app" android:versionCode="1" android:versionCodeMajor="1"><uses-sdk android:minSdkVersion="21" android:targetSdkVersion="35"/><application/></manifest>`
	apk, err := testapk.InspectApplication(t, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !apk.Verified() || apk.IsDPC() || apk.Metadata().VersionCode != 1<<32+1 || apk.Metadata().MinSDK != 21 {
		t.Fatal("generic metadata", apk.Metadata())
	}
	if _, err = apk.QR("http://server.local", nil); err == nil {
		t.Fatal("non-DPC APK can generate provisioning QR")
	}
	for _, bad := range []string{
		strings.Replace(manifest, `<application/>`, `<application android:isSplitRequired="true"/>`, 1),
		strings.Replace(manifest, `package="io.test.app"`, `package="io.test.app" split="config.arm64"`, 1),
		strings.Replace(manifest, `android:versionCode="1"`, `android:versionCode="invalid"`, 1),
		strings.Replace(manifest, `android:minSdkVersion="21"`, `android:minSdkVersion="-1"`, 1),
	} {
		if _, err := testapk.InspectApplication(t, bad); err == nil {
			t.Fatal("accepted invalid application", bad)
		}
	}
}

func TestAPKHandlerAndQRValidation(t *testing.T) {
	apk := testapk.Make(t, testapk.Manifest)

	// Test QR with invalid origin URL
	if _, err := apk.QR("https://user:pass@invalid/path", nil); err == nil {
		t.Fatal("expected error for invalid QR origin")
	}

	handler := apk.Handler()
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}

	// GET on valid path
	req := httptest.NewRequest(http.MethodGet, apk.Path(), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("ETag") != `"`+apk.Metadata().SHA256+`"` {
		t.Fatalf("unexpected ETag: %s", rec.Header().Get("ETag"))
	}
	if rec.Body.Len() != len(apk.Bytes()) {
		t.Fatalf("expected body length %d, got %d", len(apk.Bytes()), rec.Body.Len())
	}

	// HEAD on valid path
	req = httptest.NewRequest(http.MethodHead, apk.Path(), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// GET on wrong path -> 404
	req = httptest.NewRequest(http.MethodGet, "/wrong/path.apk", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// POST on valid path -> 405 MethodNotAllowed
	req = httptest.NewRequest(http.MethodPost, apk.Path(), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	if rec.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("expected Allow: GET, HEAD, got %s", rec.Header().Get("Allow"))
	}
}

// TestInspectErrorEdgeCases verifies Inspect error conditions on empty files and mock tool failures.
func TestInspectErrorEdgeCases(t *testing.T) {
	ctx := context.Background()

	// 1. Empty file -> error "APK must be between 1 byte and 64 MiB"
	emptyPath := t.TempDir() + "/empty.apk"
	if err := os.WriteFile(emptyPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := provisioning.Inspect(ctx, emptyPath, provisioning.Tools{}); err == nil {
		t.Fatal("expected error on 0 byte APK")
	}

	// 2. Signer outputs no certificates -> error
	nonEmptyPath := t.TempDir() + "/dummy.apk"
	if err := os.WriteFile(nonEmptyPath, []byte("dummy data"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := provisioning.Inspect(ctx, nonEmptyPath, provisioning.Tools{APKSigner: "echo"}); err == nil {
		t.Fatal("expected error when signer returns no certs")
	}

	// 3. Invalid manifest XML
	if _, err := testapk.InspectManifest(t, "<invalid xml>>>"); err == nil {
		t.Fatal("expected error on invalid manifest XML")
	}

	// 4. Ambiguous device admin receivers
	ambiguousManifest := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="io.kylok.unit" android:versionCode="3">
<uses-sdk android:minSdkVersion="33" android:targetSdkVersion="37"/>
<application android:allowBackup="false">
<receiver android:name=".Admin1" android:exported="true" android:permission="android.permission.BIND_DEVICE_ADMIN">
<meta-data android:name="android.app.device_admin" android:resource="@xml/admin"/>
<intent-filter><action android:name="android.app.action.DEVICE_ADMIN_ENABLED"/></intent-filter></receiver>
<receiver android:name=".Admin2" android:exported="true" android:permission="android.permission.BIND_DEVICE_ADMIN">
<meta-data android:name="android.app.device_admin" android:resource="@xml/admin"/>
<intent-filter><action android:name="android.app.action.DEVICE_ADMIN_ENABLED"/></intent-filter></receiver>
<activity android:name=".Mode" android:exported="true" android:permission="android.permission.BIND_DEVICE_ADMIN"><intent-filter><action android:name="android.app.action.GET_PROVISIONING_MODE"/><category android:name="android.intent.category.DEFAULT"/></intent-filter></activity>
<activity android:name=".Compliance" android:exported="true" android:permission="android.permission.BIND_DEVICE_ADMIN"><intent-filter><action android:name="android.app.action.ADMIN_POLICY_COMPLIANCE"/><category android:name="android.intent.category.DEFAULT"/></intent-filter></activity>
</application></manifest>`
	if _, err := testapk.InspectManifest(t, ambiguousManifest); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous receiver error, got %v", err)
	}

	// 5. Receiver without dot
	noDotManifest := strings.Replace(testapk.Manifest, `android:name=".Admin"`, `android:name="Admin"`, 1)
	apk, err := testapk.InspectManifest(t, noDotManifest)
	if err != nil {
		t.Fatalf("failed to inspect manifest with no-dot receiver: %v", err)
	}
	if apk.Metadata().Receiver != "io.kylok.unit.Admin" {
		t.Fatalf("expected resolved receiver io.kylok.unit.Admin, got %s", apk.Metadata().Receiver)
	}

	// 6. Application manifest without explicit min/target SDK (defaulting logic)
	minimalAppManifest := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="io.kylok.app" android:versionCode="1"><application/></manifest>`
	appApk, err := testapk.InspectApplication(t, minimalAppManifest)
	if err != nil {
		t.Fatalf("failed to inspect minimal application: %v", err)
	}
	if appApk.Metadata().MinSDK != 1 || appApk.Metadata().TargetSDK != 1 {
		t.Fatalf("expected defaulted min/target SDK 1, got min=%d target=%d", appApk.Metadata().MinSDK, appApk.Metadata().TargetSDK)
	}

	// 7. Analyzer failure -> line 134
	dir := t.TempDir()
	signerScript := dir + "/signer.sh"
	if err := os.WriteFile(signerScript, []byte("#!/bin/sh\nprintf '%s\\n' 'Signer #1 certificate SHA-256 digest: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := provisioning.Inspect(ctx, nonEmptyPath, provisioning.Tools{APKSigner: signerScript, APKAnalyzer: "false"}); err == nil {
		t.Fatal("expected error on failed APKAnalyzer")
	}

	// 8. Invalid APK zip archive -> line 153
	analyzerScript := dir + "/analyzer.sh"
	if err := os.WriteFile(analyzerScript, []byte("#!/bin/sh\ncat <<'EOF'\n"+testapk.Manifest+"\nEOF\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := provisioning.Inspect(ctx, nonEmptyPath, provisioning.Tools{APKSigner: signerScript, APKAnalyzer: analyzerScript}); err == nil {
		t.Fatal("expected error on invalid zip archive in Inspect")
	}

	// 9. Empty 0-byte APK -> line 99
	emptyPath = dir + "/empty2.apk"
	if err = os.WriteFile(emptyPath, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := provisioning.Inspect(ctx, emptyPath, provisioning.Tools{}); err == nil || !strings.Contains(err.Error(), "between 1 byte and 64 MiB") {
		t.Fatalf("expected size error on empty APK, got %v", err)
	}

	// 10. Default tools APKSigner and APKAnalyzer strings -> lines 115, 118
	_ = os.WriteFile(dir+"/dummy2.apk", []byte("dummy apk bytes"), 0600)
	_, _ = provisioning.Inspect(ctx, dir+"/dummy2.apk", provisioning.Tools{})
}


