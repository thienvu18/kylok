// SPDX-License-Identifier: LGPL-3.0-only
// Package provisioning verifies signed host APKs and constructs Android QR payloads.
package provisioning

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const MaxAPKBytes = 64 << 20

type Tools struct{ APKSigner, APKAnalyzer string }
type Metadata struct {
	PackageName         string   `json:"package_name"`
	Receiver            string   `json:"receiver"`
	VersionCode         int64    `json:"version_code"`
	MinSDK              int      `json:"min_sdk"`
	MaxSDK              int      `json:"max_sdk"`
	SupportedABIs       []string `json:"supported_abis"`
	SizeBytes           int64    `json:"size_bytes"`
	TargetSDK           int      `json:"target_sdk"`
	Debuggable          bool     `json:"debuggable"`
	SHA256              string   `json:"sha256"`
	SigningCertificates []string `json:"signing_certificates_sha256"`
}

// APK retains the exact verified bytes; callers cannot mutate metadata or content.
type APK struct {
	metadata    Metadata
	data        []byte
	verified    bool
	deviceAdmin bool
}

// Verified distinguishes an inspected immutable artifact from a zero-value APK.
// Usage: if !apk.Verified() { return errors.New("verified APK required") }.
func (a *APK) Verified() bool { return a != nil && a.verified }

// IsDPC distinguishes a verified provisioning-capable host from an ordinary signed application.
// Usage: if !apk.IsDPC() { return errors.New("provisioning host required") }.
func (a *APK) IsDPC() bool { return a.Verified() && a.deviceAdmin }

// Metadata returns detached verified properties so callers cannot mutate registered identity.
// Usage: metadata := apk.Metadata().
func (a *APK) Metadata() Metadata {
	m := a.metadata
	m.SigningCertificates = append([]string(nil), m.SigningCertificates...)
	m.SupportedABIs = append([]string(nil), m.SupportedABIs...)
	return m
}

// Bytes returns a copy of the exact verified APK content for immutable publication.
// Usage: content := apk.Bytes().
func (a *APK) Bytes() []byte { return bytes.Clone(a.data) }

// Inspect snapshots the input before invoking Android's signature verifier and XML decoder.
// Both tools inspect the same bytes that are later registered and served.
// Usage: apk, err := provisioning.Inspect(ctx, signedAPKPath, provisioning.Tools{}).
func Inspect(ctx context.Context, path string, tools Tools) (*APK, error) {
	return inspect(ctx, path, tools, true)
}

// InspectApplication verifies a single/universal signed APK without requiring DPC components.
// Usage: apk, err := provisioning.InspectApplication(ctx, applicationPath, provisioning.Tools{}).
func InspectApplication(ctx context.Context, path string, tools Tools) (*APK, error) {
	return inspect(ctx, path, tools, false)
}

// Snapshot one bounded APK, verify its signature and parse the same bytes before granting verified status.
func inspect(ctx context.Context, path string, tools Tools, requireDPC bool) (*APK, error) {
	source, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open APK: %w", err)
	}
	defer source.Close()
	data, err := io.ReadAll(io.LimitReader(source, MaxAPKBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxAPKBytes {
		return nil, errors.New("APK must be between 1 byte and 64 MiB")
	}
	temp, err := os.CreateTemp("", "kylok-verified-*.apk")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return nil, err
	}
	if err = temp.Close(); err != nil {
		return nil, err
	}
	signer := tools.APKSigner
	if signer == "" {
		signer = "apksigner"
	}
	analyzer := tools.APKAnalyzer
	if analyzer == "" {
		analyzer = "apkanalyzer"
	}
	// A finite deadline also bounds misconfigured external tools during construction.
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	certs, err := exec.CommandContext(ctx, signer, "verify", "--verbose", "--print-certs", temp.Name()).Output()
	if err != nil {
		return nil, fmt.Errorf("APK signature verification failed: %w", err)
	}
	matches := regexp.MustCompile(`(?m)^.*Signer[^\n]*certificate SHA-256 digest: ([0-9a-fA-F]{64})\s*$`).FindAllSubmatch(certs, -1)
	if len(matches) == 0 {
		return nil, errors.New("signature verifier returned no signing certificates")
	}
	manifest, err := exec.CommandContext(ctx, analyzer, "manifest", "print", temp.Name()).Output()
	if err != nil {
		return nil, fmt.Errorf("decode merged APK manifest: %w", err)
	}
	m, err := parseManifest(manifest, requireDPC)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	m.SHA256 = hex.EncodeToString(digest[:])
	seen := map[string]bool{}
	for _, match := range matches {
		cert := strings.ToLower(string(match[1]))
		if !seen[cert] {
			m.SigningCertificates = append(m.SigningCertificates, cert)
			seen[cert] = true
		}
	}
	sort.Strings(m.SigningCertificates)
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid APK archive: %w", err)
	}
	abis := map[string]bool{}
	for _, entry := range archive.File {
		parts := strings.Split(entry.Name, "/")
		if len(parts) >= 3 && parts[0] == "lib" && strings.HasSuffix(entry.Name, ".so") {
			abis[parts[1]] = true
		}
	}
	for abi := range abis {
		m.SupportedABIs = append(m.SupportedABIs, abi)
	}
	sort.Strings(m.SupportedABIs)
	m.SizeBytes = int64(len(data))
	_, dpcErr := parseManifest(manifest, true)
	return &APK{metadata: m, data: data, verified: true, deviceAdmin: dpcErr == nil}, nil
}

type component struct {
	Name       string `xml:"http://schemas.android.com/apk/res/android name,attr"`
	Permission string `xml:"http://schemas.android.com/apk/res/android permission,attr"`
	Exported   string `xml:"http://schemas.android.com/apk/res/android exported,attr"`
	Enabled    string `xml:"http://schemas.android.com/apk/res/android enabled,attr"`
	Metadata   []struct {
		Name     string `xml:"http://schemas.android.com/apk/res/android name,attr"`
		Resource string `xml:"http://schemas.android.com/apk/res/android resource,attr"`
	} `xml:"meta-data"`
	Filters []struct {
		Actions []struct {
			Name string `xml:"http://schemas.android.com/apk/res/android name,attr"`
		} `xml:"action"`
		Categories []struct {
			Name string `xml:"http://schemas.android.com/apk/res/android name,attr"`
		} `xml:"category"`
	} `xml:"intent-filter"`
}

// Check whether an Android component declares the required intent action.
func (c component) action(name string) bool {
	for _, f := range c.Filters {
		for _, a := range f.Actions {
			if a.Name == name {
				return true
			}
		}
	}
	return false
}

// Require both the provisioning action and default category before accepting an exported Activity.
func (c component) defaultAction(name string) bool {
	for _, f := range c.Filters {
		action, category := false, false
		for _, a := range f.Actions {
			action = action || a.Name == name
		}
		for _, a := range f.Categories {
			category = category || a.Name == "android.intent.category.DEFAULT"
		}
		if action && category {
			return true
		}
	}
	return false
}

// Validate APK identity, SDK bounds and protected provisioning components from decoded XML.
func parseManifest(data []byte, requireDPC bool) (Metadata, error) {
	var manifest struct {
		XMLName      xml.Name   `xml:"manifest"`
		Package      string     `xml:"package,attr"`
		VersionCode  string     `xml:"http://schemas.android.com/apk/res/android versionCode,attr"`
		VersionMajor string     `xml:"http://schemas.android.com/apk/res/android versionCodeMajor,attr"`
		Split        string     `xml:"split,attr"`
		UsesSplit    []struct{} `xml:"uses-split"`
		SDK          struct {
			Min    int `xml:"http://schemas.android.com/apk/res/android minSdkVersion,attr"`
			Target int `xml:"http://schemas.android.com/apk/res/android targetSdkVersion,attr"`
			Max    int `xml:"http://schemas.android.com/apk/res/android maxSdkVersion,attr"`
		} `xml:"uses-sdk"`
		Application struct {
			SplitRequired bool        `xml:"http://schemas.android.com/apk/res/android isSplitRequired,attr"`
			Debuggable    bool        `xml:"http://schemas.android.com/apk/res/android debuggable,attr"`
			Enabled       string      `xml:"http://schemas.android.com/apk/res/android enabled,attr"`
			Receivers     []component `xml:"receiver"`
			Activities    []component `xml:"activity"`
		} `xml:"application"`
	}
	if err := xml.Unmarshal(data, &manifest); err != nil {
		return Metadata{}, fmt.Errorf("manifest XML: %w", err)
	}
	low, err := strconv.ParseUint(manifest.VersionCode, 10, 32)
	if err != nil {
		return Metadata{}, errors.New("invalid versionCode")
	}
	var major uint64
	if manifest.VersionMajor != "" {
		major, err = strconv.ParseUint(manifest.VersionMajor, 10, 31)
	}
	version := int64(major<<32 | low)
	if manifest.SDK.Min == 0 {
		manifest.SDK.Min = 1
	}
	if manifest.SDK.Target == 0 {
		manifest.SDK.Target = manifest.SDK.Min
	}
	if err != nil || version < 1 || manifest.SDK.Min < 1 || manifest.SDK.Target < 1 || manifest.Package == "" || manifest.Application.Enabled == "false" || manifest.Split != "" || len(manifest.UsesSplit) > 0 || manifest.Application.SplitRequired || manifest.SDK.Max != 0 && manifest.SDK.Max < manifest.SDK.Min {
		return Metadata{}, errors.New("APK requires a package, positive long version, valid SDK range and no split dependencies")
	}
	metadata := Metadata{PackageName: manifest.Package, VersionCode: version, MinSDK: manifest.SDK.Min, MaxSDK: manifest.SDK.Max, TargetSDK: manifest.SDK.Target, Debuggable: manifest.Application.Debuggable}
	if !requireDPC {
		return metadata, nil
	}
	if manifest.SDK.Min < 33 || manifest.SDK.Min > 37 || manifest.SDK.Target != 37 {
		return Metadata{}, errors.New("DPC must use min SDK 33–37 and target SDK 37")
	}
	var receiver string
	for _, c := range manifest.Application.Receivers {
		valid := c.Exported == "true" && c.Enabled != "false" && c.Permission == "android.permission.BIND_DEVICE_ADMIN" && c.action("android.app.action.DEVICE_ADMIN_ENABLED")
		metadata := false
		for _, m := range c.Metadata {
			metadata = metadata || m.Name == "android.app.device_admin" && m.Resource != ""
		}
		if valid && metadata {
			if receiver != "" {
				return Metadata{}, errors.New("ambiguous device admin receivers")
			}
			receiver = c.Name
		}
	}
	if receiver == "" {
		return Metadata{}, errors.New("APK has no exported, protected device admin receiver with metadata")
	}
	for _, action := range []string{"android.app.action.GET_PROVISIONING_MODE", "android.app.action.ADMIN_POLICY_COMPLIANCE"} {
		found := false
		for _, c := range manifest.Application.Activities {
			found = found || c.Exported == "true" && c.Enabled != "false" && c.Permission == "android.permission.BIND_DEVICE_ADMIN" && c.defaultAction(action)
		}
		if !found {
			return Metadata{}, fmt.Errorf("missing protected provisioning activity for %s", action)
		}
	}
	if strings.HasPrefix(receiver, ".") {
		receiver = manifest.Package + receiver
	} else if !strings.Contains(receiver, ".") {
		receiver = manifest.Package + "." + receiver
	}
	metadata.Receiver = receiver
	return metadata, nil
}

// Origin accepts an HTTP or HTTPS origin using a domain name or IP address.
// Route prefixes, credentials and token-bearing URLs are unsupported.
// Usage: origin, err := provisioning.Origin("http://192.168.1.5:8080").
func Origin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(value, "#") || u.Path != "" && u.Path != "/" || u.Opaque != "" {
		return "", errors.New("URL must be an HTTP or HTTPS origin without credentials, path, query or fragment")
	}
	return strings.TrimSuffix(value, "/"), nil
}

// Path returns the content-addressed public bootstrap route for these verified bytes.
// Usage: route := apk.Path().
func (a *APK) Path() string { return "/bootstrap/" + a.metadata.SHA256 + ".apk" }

// Handler serves this verified APK only at its immutable GET/HEAD bootstrap path.
// Usage: bootstrapServer.Handler = apk.Handler().
func (a *APK) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != a.Path() {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", `"`+a.metadata.SHA256+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, a.metadata.PackageName+".apk", time.Time{}, bytes.NewReader(a.data))
	})
}

// QR returns secret JSON. Hosts must not publish or log the payload.
// Usage: payload, err := apk.QR(bootstrapOrigin, map[string]string{"kylok.server": apiOrigin, "kylok.enrollment_token": token}).
func (a *APK) QR(publicURL string, extras map[string]string) ([]byte, error) {
	if !a.IsDPC() {
		return nil, errors.New("verified APK required")
	}
	origin, err := Origin(publicURL)
	if err != nil {
		return nil, err
	}
	return a.qr(origin, extras)
}

// Encode the verified component, SHA-256 and host extras using Android's provisioning field names.
func (a *APK) qr(origin string, extras map[string]string) ([]byte, error) {
	sum, _ := hex.DecodeString(a.metadata.SHA256)
	const prefix = "android.app.extra."
	return json.Marshal(map[string]any{
		prefix + "PROVISIONING_DEVICE_ADMIN_COMPONENT_NAME":            a.metadata.PackageName + "/" + a.metadata.Receiver,
		prefix + "PROVISIONING_DEVICE_ADMIN_PACKAGE_DOWNLOAD_LOCATION": origin + a.Path(),
		prefix + "PROVISIONING_DEVICE_ADMIN_PACKAGE_CHECKSUM":          base64.RawURLEncoding.EncodeToString(sum),
		prefix + "PROVISIONING_ALLOW_OFFLINE":                          true,
		prefix + "PROVISIONING_ADMIN_EXTRAS_BUNDLE":                    extras,
	})
}
