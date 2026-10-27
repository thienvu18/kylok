// SPDX-License-Identifier: LGPL-3.0-only
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/thienvu18/kylok/go"
)

func TestExampleNetworkSecretsArePrivateImmutableScopedSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(path, []byte(`{"site":{"key-v1":"private-pass"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	provider, err := loadNetworkSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := provider.ReadNetworkSecret(context.Background(), "site", "key-v1")
	if err != nil || string(value) != "private-pass" {
		t.Fatal(err)
	}
	value[0] = 'x'
	if next, err := provider.ReadNetworkSecret(context.Background(), "site", "key-v1"); err != nil || string(next) != "private-pass" {
		t.Fatal("aliased value", err)
	}
	if _, err := provider.ReadNetworkSecret(context.Background(), "foreign", "key-v1"); !errors.Is(err, kylok.ErrNotFound) {
		t.Fatal("scope fallback", err)
	}
	if err := os.WriteFile(path, []byte(`{"site":{"key-v1":"changed-pass"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if next, err := provider.ReadNetworkSecret(context.Background(), "site", "key-v1"); err != nil || string(next) != "private-pass" {
		t.Fatal("running provider was mutable", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.ReadNetworkSecret(cancelled, "site", "key-v1"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadNetworkSecrets(path); err == nil {
		t.Fatal("public vault file accepted")
	}
	if provider, err := loadNetworkSecrets(""); err != nil || provider != nil {
		t.Fatal(provider, err)
	}
}
