// SPDX-License-Identifier: LGPL-3.0-only
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"github.com/thienvu18/kylok/go"
	"github.com/thienvu18/kylok/go/ports"
	"github.com/thienvu18/kylok/go/provisioning"
)

// Embed assets so the example remains one executable, without a frontend toolchain.
//
//go:embed web/*
var consoleFiles embed.FS

// console owns example-only authorization; the SDK exposes no administrator routes.
type console struct {
	sdk                                              *kylok.SDK
	apk                                              *provisioning.APK
	tools                                            provisioning.Tools
	apiOrigin, bootstrapOrigin, agentListen, dataDir string
	tokenHash                                        [32]byte
}

// originChoice names a reachable candidate; custom domains remain an explicit host choice.
type originChoice struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
}

// consoleToken persists one private administrator credential without replacing an existing one.
func consoleToken(path string) (string, error) {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return "", errors.New("console token must be a private regular file (0600)")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		token := strings.TrimSpace(string(data))
		if len(token) != 43 {
			return "", errors.New("console token must contain 43 base64url characters")
		}
		return token, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	bytes := make([]byte, 32) // 256 random bits; base64url produces a clipboard-friendly 43-character token.
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	if err := writeSecret(path, []byte(token+"\n")); err != nil {
		return "", err
	}
	return token, nil
}

// origins enumerates addresses accepted by the listener, rather than advertising unreachable NICs.
func origins(origin, listen string) []originChoice {
	u, err := url.Parse(origin)
	if err != nil {
		return nil
	}
	bound, port, err := net.SplitHostPort(listen)
	if err != nil {
		return []originChoice{{"Configured endpoint", origin}}
	}
	result := []originChoice{{"Configured endpoint", origin}}
	seen := map[string]bool{origin: true}
	interfaces, _ := net.Interfaces()
	for _, nic := range interfaces {
		if nic.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, _ := nic.Addrs()
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
				continue
			}
			if bound != "" && bound != "0.0.0.0" && bound != "::" && bound != ip.String() {
				continue
			}
			if bound == "0.0.0.0" && ip.To4() == nil {
				continue
			}
			candidate := u.Scheme + "://" + net.JoinHostPort(ip.String(), port)
			if !seen[candidate] {
				result = append(result, originChoice{nic.Name + " · " + ip.String(), candidate})
				seen[candidate] = true
			}
		}
	}
	sort.SliceStable(result[1:], func(i, j int) bool { return result[i+1].Name < result[j+1].Name })
	return result
}

// handler protects every management request with an explicit token and same-origin browser writes.
func (c *console) handler() http.Handler {
	assets, _ := fs.Sub(consoleFiles, "web")
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			files.ServeHTTP(w, r)
			return
		}
		hash := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare(hash[:], c.tokenHash[:]) != 1 {
			http.Error(w, "Administrator token required", http.StatusUnauthorized)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host || u.Scheme != "http" && u.Scheme != "https" {
				http.Error(w, "Cross-origin management is disabled", http.StatusForbidden)
				return
			}
		}
		c.api(w, r)
	})
}

// reply serializes only public management results and keeps failures visible to the operator.
func reply(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ports.ErrNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, ports.ErrConflict) || errors.Is(err, ports.ErrReferenced) {
			status = http.StatusConflict
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

// readJSON rejects unknown fields and multiple documents before any SDK mutation.
func readJSON(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10) // Policy documents plus envelope fit in 128 KiB; APKs use a separate bounded upload.
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("Expected one JSON document")
	}
	return nil
}

// enrollmentQR renders the selected origins from the original single-use token and verified APK.
func (c *console) enrollmentQR(ctx context.Context, token, api, bootstrap string) (map[string]any, error) {
	payload, err := c.sdk.QRWithOrigins(ctx, token, api, bootstrap)
	if err != nil {
		return nil, err
	}
	png, err := qrcode.Encode(string(payload), qrcode.Medium, 384)
	if err != nil {
		return nil, err
	}
	return map[string]any{"payload": json.RawMessage(payload), "image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), "token": token}, nil
}

// api maps the example's authenticated actions to public SDK methods, never raw database writes.
func (c *console) api(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	scope := q.Get("scope")
	device := q.Get("device")
	if r.Method == http.MethodGet {
		switch r.URL.Path {
		case "/api/config":
			reply(w, map[string]any{"version": kylok.Version(), "origins": origins(c.apiOrigin, c.agentListen), "api_origin": c.apiOrigin, "bootstrap_origin": c.bootstrapOrigin, "host_package": c.apk.Metadata().PackageName}, nil)
		case "/api/devices":
			v, e := c.sdk.ListDevices(ctx, scope, q.Get("after"), 100)
			reply(w, v, e)
		case "/api/applications":
			v, e := c.sdk.ListApplications(ctx, scope, q.Get("after"), 100)
			reply(w, v, e)
		case "/api/operations":
			v, e := c.sdk.ListOperations(ctx, scope, device, q.Get("after"), 100)
			reply(w, v, e)
		case "/api/policy":
			v, e := c.sdk.GetPolicy(ctx, scope, device)
			reply(w, v, e)
		default:
			http.NotFound(w, r)
		}
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/api/register" {
		c.register(w, r)
		return
	}
	var input struct {
		Scope      string       `json:"scope"`
		Device     string       `json:"device"`
		Token      string       `json:"token"`
		Origin     string       `json:"origin"`
		Bootstrap  string       `json:"bootstrap"`
		TTL        int          `json:"ttl_seconds"`
		Digest     string       `json:"digest"`
		RequestKey string       `json:"request_key"`
		Timeout    int          `json:"timeout_seconds"`
		Policy     kylok.Policy `json:"policy"`
	}
	if err := readJSON(w, r, &input); err != nil {
		reply(w, nil, err)
		return
	}
	switch r.URL.Path {
	case "/api/enroll":
		// Validate destinations before consuming server resources for a new enrollment.
		if _, err := provisioning.Origin(input.Origin); err != nil {
			reply(w, nil, err)
			return
		}
		if _, err := provisioning.Origin(input.Bootstrap); err != nil {
			reply(w, nil, err)
			return
		}
		enrollment, err := c.sdk.CreateEnrollment(ctx, input.Scope, time.Duration(input.TTL)*time.Second)
		if err != nil {
			reply(w, nil, err)
			return
		}
		value, err := c.enrollmentQR(ctx, enrollment.Token, input.Origin, input.Bootstrap)
		if err == nil {
			value["expires_at"] = enrollment.ExpiresAt
		}
		reply(w, value, err)
	case "/api/qr":
		v, e := c.enrollmentQR(ctx, input.Token, input.Origin, input.Bootstrap)
		reply(w, v, e)
	case "/api/revoke":
		err := c.sdk.RevokeDevice(ctx, input.Scope, input.Device)
		reply(w, map[string]bool{"revoked": err == nil}, err)
	case "/api/assign":
		v, e := c.sdk.AssignApplication(ctx, input.Scope, input.Device, input.Digest, input.RequestKey, time.Duration(input.Timeout)*time.Second)
		reply(w, v, e)
	case "/api/remove":
		err := c.sdk.RemoveApplication(ctx, input.Scope, input.Digest)
		reply(w, map[string]bool{"removed": err == nil}, err)
	case "/api/set-policy":
		v, e := c.sdk.SetPolicy(ctx, input.Scope, input.Device, input.RequestKey, input.Policy)
		reply(w, v, e)
	default:
		http.NotFound(w, r)
	}
}

// register verifies an uploaded APK in a private temporary file before scoped publication.
func (c *console) register(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, provisioning.MaxAPKBytes+(1<<20)) // Leave bounded room for multipart headers around one APK.
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		reply(w, nil, err)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	source, _, err := r.FormFile("apk")
	if err != nil {
		reply(w, nil, err)
		return
	}
	defer source.Close()
	temp, err := os.CreateTemp(c.dataDir, "console-upload-*.apk")
	if err != nil {
		reply(w, nil, errors.New("Cannot stage APK"))
		return
	}
	defer os.Remove(temp.Name())
	n, copyErr := io.Copy(temp, io.LimitReader(source, provisioning.MaxAPKBytes+1))
	closeErr := temp.Close()
	if err = errors.Join(copyErr, closeErr); err != nil || n > provisioning.MaxAPKBytes {
		reply(w, nil, errors.New("APK upload exceeds the size limit or could not be staged"))
		return
	}
	apk, err := provisioning.InspectApplication(r.Context(), temp.Name(), c.tools)
	if err != nil {
		reply(w, nil, errors.New("APK signature or manifest verification failed"))
		return
	}
	value, err := c.sdk.RegisterApplication(r.Context(), r.FormValue("scope"), apk)
	reply(w, value, err)
}

// startConsole binds an independent management listener and loads its private administrator token.
func startConsole(sdk *kylok.SDK, apk *provisioning.APK, tools provisioning.Tools, api, bootstrap, agentListen, data, listen, tokenPath string) (*http.Server, string, error) {
	if tokenPath == "" {
		tokenPath = filepath.Join(data, "console-token")
	}
	token, err := consoleToken(tokenPath)
	if err != nil {
		return nil, "", err
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, "", err
	}
	c := &console{sdk: sdk, apk: apk, tools: tools, apiOrigin: api, bootstrapOrigin: bootstrap, agentListen: agentListen, dataDir: data, tokenHash: sha256.Sum256([]byte(token))}
	server := &http.Server{Handler: c.handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 90 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() { _ = server.Serve(listener) }()
	return server, fmt.Sprintf("http://%s (token file: %s)", listener.Addr(), tokenPath), nil
}
