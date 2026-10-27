// SPDX-License-Identifier: LGPL-3.0-only
// This host deliberately imports only public SDK APIs.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/skip2/go-qrcode"
	"github.com/thienvu18/kylok/go"
	"github.com/thienvu18/kylok/go/provisioning"
)

// Run the standalone example host and terminate with an actionable error on configuration failure.
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// Create private provisioning/token files exclusively, preventing accidental overwrites or public permissions.
func writeSecret(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

// Validate CLI intent and configuration before choosing a local management action or starting host-owned listeners.
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: minimal-server <command> [flags]; see docs/local-server.md for enrollment, applications, policy, fleet and network commands")
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	public := flags.String("public-url", "", "device-reachable HTTP or HTTPS origin")
	apkPath := flags.String("apk", "", "signed host APK")
	data := flags.String("data", "", "persistent data directory")
	signer := flags.String("apksigner", "apksigner", "Android SDK apksigner")
	analyzer := flags.String("apkanalyzer", "apkanalyzer", "Android SDK apkanalyzer")
	allowDebug := flags.Bool("allow-debug-apk", false, "explicitly allow a debuggable proof APK")
	bootstrapURL := flags.String("bootstrap-url", "", "optional separate HTTP/HTTPS APK origin")
	bootstrapListen := flags.String("bootstrap-listen", "", "optional separate APK listener bind address")
	addr := flags.String("listen", "127.0.0.1:8080", "API/APK bind address")
	cert := flags.String("tls-cert", "", "PEM certificate chain")
	key := flags.String("tls-key", "", "PEM private key")
	scope := flags.String("scope", "", "authorized scope ID")
	ttl := flags.Duration("ttl", 15*time.Minute, "enrollment token lifetime (1m–24h)")
	out := flags.String("out", "", "new QR output prefix; creates .json and .png, mode 0600")
	after := flags.String("after", "", "opaque page cursor; list uses the last device ID")
	id := flags.String("device", "", "managed device ID")
	appPath := flags.String("application-apk", "", "signed single/universal application APK")
	digest := flags.String("digest", "", "registered application SHA-256")
	requestKey := flags.String("request-key", "", "assignment idempotency key")
	timeout := flags.Duration("timeout", 24*time.Hour, "installation/command timeout (1m–7d)")
	policyPath := flags.String("policy", "", "policy JSON file; an empty document releases Kylok controls")
	consoleListen := flags.String("ui-listen", "", "optional administrator UI bind address, e.g. 127.0.0.1:8081")
	consoleTokenPath := flags.String("ui-token-file", "", "private administrator token file; defaults to data/console-token")
	extra := registerFleetFlags(flags)
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if command != "serve" && command != "enroll" && command != "list" && command != "revoke" && command != "register" && command != "assign" && command != "operations" && command != "remove" && command != "set-policy" && command != "policy" && !extra.accepts(command) {
		return errors.New("unknown command")
	}
	origin, err := provisioning.Origin(*public)
	if err != nil {
		return err
	}
	primary, _ := url.Parse(origin)
	bootstrap := primary
	if *bootstrapURL != "" {
		value, err := provisioning.Origin(*bootstrapURL)
		if err != nil {
			return err
		}
		bootstrap, _ = url.Parse(value)
	}
	if *bootstrapListen != "" && *bootstrapURL == "" {
		return errors.New("bootstrap-listen requires bootstrap-url")
	}
	if command == "serve" && (primary.Scheme == "https" || *bootstrapListen != "" && bootstrap.Scheme == "https") && (*cert == "" || *key == "") {
		return errors.New("HTTPS listeners require tls-cert and tls-key")
	}
	if command == "enroll" && *out == "" {
		return errors.New("enroll requires a private QR output prefix")
	}
	ctx := context.Background()
	apk, err := provisioning.Inspect(ctx, *apkPath, provisioning.Tools{APKSigner: *signer, APKAnalyzer: *analyzer})
	if err != nil {
		return err
	}
	secrets, err := loadNetworkSecrets(*extra.secrets)
	if err != nil {
		return err
	}
	sdk, err := kylok.New(ctx, kylok.Options{NetworkSecrets: secrets, PublicURL: *public, Bootstrap: apk, DataDir: *data, AllowDebugAPK: *allowDebug, BootstrapURL: *bootstrapURL})
	if err != nil {
		return err
	}
	defer sdk.Close()
	if handled, err := runFleetCommand(ctx, sdk, command, *scope, *id, *digest, *requestKey, *after, *timeout, extra); handled {
		return err
	}
	switch command {
	case "set-policy":
		f, err := os.Open(*policyPath)
		if err != nil {
			return err
		}
		defer f.Close()
		decoder := json.NewDecoder(io.LimitReader(f, 65537))
		decoder.DisallowUnknownFields()
		var policy kylok.Policy
		if err = decoder.Decode(&policy); err != nil {
			return err
		}
		var extra any
		if err = decoder.Decode(&extra); err != io.EOF {
			return errors.New("policy must contain one JSON object within 64 KiB")
		}
		result, err := sdk.SetPolicy(ctx, *scope, *id, *requestKey, policy)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	case "policy":
		result, err := sdk.GetPolicy(ctx, *scope, *id)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	case "register":
		apk, err := provisioning.InspectApplication(ctx, *appPath, provisioning.Tools{APKSigner: *signer, APKAnalyzer: *analyzer})
		if err != nil {
			return err
		}
		artifact, err := sdk.RegisterApplication(ctx, *scope, apk)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(artifact)
	case "assign":
		op, err := sdk.AssignApplication(ctx, *scope, *id, *digest, *requestKey, *timeout)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(op)
	case "operations":
		ops, err := sdk.ListOperations(ctx, *scope, *id, *after, 100)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(ops)
	case "remove":
		return sdk.RemoveApplication(ctx, *scope, *digest)
	case "enroll":
		enrollment, err := sdk.CreateEnrollment(ctx, *scope, *ttl)
		if err != nil {
			return err
		}
		payload, err := sdk.QR(ctx, enrollment.Token)
		if err != nil {
			return err
		}
		png, err := qrcode.Encode(string(payload), qrcode.Medium, 768)
		if err != nil {
			return err
		}
		if err = writeSecret(*out+".json", payload); err != nil {
			return err
		}
		if err = writeSecret(*out+".png", png); err != nil {
			return err
		}
		fmt.Printf("QR saved to %s.json and %s.png; expires %s\n", *out, *out, enrollment.ExpiresAt.Format(time.RFC3339))
		return nil
	case "list":
		devices, err := sdk.ListDevices(ctx, *scope, *after, 100)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(devices)
	case "revoke":
		return sdk.RevokeDevice(ctx, *scope, *id)
	}
	server := &http.Server{Addr: *addr, Handler: sdk.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	defer server.Close()
	var adminServer *http.Server
	if *consoleListen != "" {
		var location string
		adminServer, location, err = startConsole(sdk, apk, provisioning.Tools{APKSigner: *signer, APKAnalyzer: *analyzer}, origin, bootstrap.String(), *addr, *data, *consoleListen, *consoleTokenPath)
		if err != nil {
			return err
		}
		defer adminServer.Close()
		log.Printf("Administrator console: %s", location)
	}
	var bootstrapServer *http.Server
	signals, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 2)
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	protocols.SetUnencryptedHTTP2(primary.Scheme == "http")
	server.Protocols = protocols
	go func() {
		if primary.Scheme == "https" {
			done <- server.ListenAndServeTLS(*cert, *key)
		} else {
			done <- server.ListenAndServe()
		}
	}()
	if *bootstrapListen != "" {
		bootstrapServer = &http.Server{Addr: *bootstrapListen, Handler: sdk.BootstrapHandler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
		defer bootstrapServer.Close()
		go func() {
			if bootstrap.Scheme == "https" {
				done <- bootstrapServer.ListenAndServeTLS(*cert, *key)
			} else {
				done <- bootstrapServer.ListenAndServe()
			}
		}()
		log.Printf("%s bootstrap APK listener at %s", bootstrap.Scheme, *bootstrapListen)
	}
	log.Printf("%s agent/bootstrap listener at %s", primary.Scheme, *addr)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signals.Done():
		shutdown, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		err := server.Shutdown(shutdown)
		if adminServer != nil {
			err = errors.Join(err, adminServer.Shutdown(shutdown))
		}
		if bootstrapServer != nil {
			err = errors.Join(err, bootstrapServer.Shutdown(shutdown))
		}
		return err
	}
}
