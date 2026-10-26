// SPDX-License-Identifier: LGPL-3.0-only
package filesystem_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/thienvu18/kylok/go/adapters/filesystem"
)

func TestImmutableArtifactIntegrityAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := filesystem.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("apk bytes")
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Put(ctx, digest, data); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := s.Put(ctx, digest, []byte("wrong")); err == nil {
		t.Fatal("mismatched content accepted")
	}
	if _, err := s.Open(ctx, "../../etc/passwd"); err == nil {
		t.Fatal("traversal accepted")
	}
	s, err = filesystem.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := s.Open(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, _ := io.ReadAll(reader)
	if string(body) != string(data) {
		t.Fatal("content")
	}
	path := filepath.Join(dir, digest+".apk")
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, digest); err == nil {
		t.Fatal("tampering undetected")
	}
	if err := s.Put(ctx, digest, data); err == nil {
		t.Fatal("silently replaced tampered artifact")
	}
}
