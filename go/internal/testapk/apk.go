// SPDX-License-Identifier: LGPL-3.0-only
// Package testapk supplies mocked external-tool output for engine unit tests.
// These fixtures do not establish real APK signature or Android compatibility.
package testapk

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/thienvu18/kylok/go/provisioning"
)

const Manifest = `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="io.kylok.unit" android:versionCode="3">
<uses-sdk android:minSdkVersion="33" android:targetSdkVersion="37"/>
<application android:allowBackup="false">
<receiver android:name=".Admin" android:exported="true" android:permission="android.permission.BIND_DEVICE_ADMIN">
<meta-data android:name="android.app.device_admin" android:resource="@xml/admin"/>
<intent-filter><action android:name="android.app.action.DEVICE_ADMIN_ENABLED"/></intent-filter></receiver>
<activity android:name=".Mode" android:exported="true" android:permission="android.permission.BIND_DEVICE_ADMIN"><intent-filter><action android:name="android.app.action.GET_PROVISIONING_MODE"/><category android:name="android.intent.category.DEFAULT"/></intent-filter></activity>
<activity android:name=".Compliance" android:exported="true" android:permission="android.permission.BIND_DEVICE_ADMIN"><intent-filter><action android:name="android.app.action.ADMIN_POLICY_COMPLIANCE"/><category android:name="android.intent.category.DEFAULT"/></intent-filter></activity>
</application></manifest>`

// Make creates a verified deterministic APK fixture using isolated fake Android tools.
// Usage: apk := testapk.Make(t, manifest).
func Make(t testing.TB, manifest string) *provisioning.APK {
	t.Helper()
	apk, err := InspectManifest(t, manifest)
	if err != nil {
		t.Fatal(err)
	}
	return apk
}

// InspectManifest exercises DPC validation against a supplied decoded manifest fixture.
// Usage: apk, err := testapk.InspectManifest(t, manifest).
func InspectManifest(t testing.TB, manifest string) (*provisioning.APK, error) {
	return inspect(t, manifest, false)
}

// InspectApplication exercises ordinary APK validation without DPC component requirements.
// Usage: apk, err := testapk.InspectApplication(t, manifest).
func InspectApplication(t testing.TB, manifest string) (*provisioning.APK, error) {
	return inspect(t, manifest, true)
}

// Build isolated fake verifier tools while keeping the same public APK inspection path under test.
func inspect(t testing.TB, manifest string, application bool) (*provisioning.APK, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "unit.apk")
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	entry, err := writer.Create("AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	signer := filepath.Join(dir, "apksigner")
	analyzer := filepath.Join(dir, "apkanalyzer")
	if err := os.WriteFile(signer, []byte("#!/bin/sh\nprintf '%s\\n' 'Signer #1 certificate SHA-256 digest: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(analyzer, []byte("#!/bin/sh\ncat <<'MANIFEST'\n"+manifest+"\nMANIFEST\n"), 0700); err != nil {
		t.Fatal(err)
	}
	tools := provisioning.Tools{APKSigner: signer, APKAnalyzer: analyzer}
	if application {
		return provisioning.InspectApplication(context.Background(), path, tools)
	}
	return provisioning.Inspect(context.Background(), path, tools)
}
