// SPDX-License-Identifier: LGPL-3.0-only
package provisioning_test

import (
	"context"
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
