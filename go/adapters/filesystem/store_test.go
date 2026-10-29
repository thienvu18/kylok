// SPDX-License-Identifier: LGPL-3.0-only
package filesystem_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestFilesystemStoreEdgeCases(t *testing.T) {
	ctx := context.Background()
	if _, err := filesystem.Open(""); err == nil {
		t.Fatal("expected error on empty dir")
	}
	if _, err := filesystem.Open("/dev/null/forbidden"); err == nil {
		t.Fatal("expected error on invalid dir")
	}

	dir := t.TempDir()
	s, err := filesystem.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	data := []byte("valid content")
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])

	if err := s.Put(cancelled, digest, data); err == nil {
		t.Fatal("expected error on cancelled context in Put")
	}

	if err := s.Put(ctx, "not-a-sha256", data); err == nil {
		t.Fatal("expected error on invalid digest pattern in Put")
	}

	if _, err := s.Open(cancelled, digest); err == nil {
		t.Fatal("expected error on cancelled context in Open")
	}

	missingDigest := "1111111111111111111111111111111111111111111111111111111111111111"
	if _, err := s.Open(ctx, missingDigest); err == nil {
		t.Fatal("expected error on missing artifact")
	}

	if _, err := s.Open(ctx, "invalid-hex"); err == nil {
		t.Fatal("expected error on invalid hex digest in Open")
	}

	// Target is a directory causing os.ReadFile error after os.Link ErrExist (store.go:75)
	dirTargetDigest := "2222222222222222222222222222222222222222222222222222222222222222"
	if err := os.Mkdir(filepath.Join(dir, dirTargetDigest+".apk"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, dirTargetDigest, data); err == nil {
		t.Fatal("expected error on directory target conflict in Put")
	}
	// Reading from directory target causes ReadAll error in Open (store.go:112)
	if _, err := s.Open(ctx, dirTargetDigest); err == nil {
		t.Fatal("expected error reading from directory in Open")
	}

	// Test snapshot.Close()
	if err := s.Put(ctx, digest, data); err != nil {
		t.Fatal(err)
	}
	r, err := s.Open(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// Test oversized artifact on disk (> 64MiB)
	overBytes := make([]byte, (64<<20)+10)
	overSum := sha256.Sum256(overBytes)
	overDigest := hex.EncodeToString(overSum[:])
	overPath := filepath.Join(dir, overDigest+".apk")
	if err := os.WriteFile(overPath, overBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, overDigest); err == nil {
		t.Fatal("expected error opening oversized artifact")
	}

	// Test cancelAfterWriteContext to reach line 66 (ctx.Err() after file creation)
	called := false
	customCtx := &mockCancelContext{Context: context.Background(), called: &called}
	data2 := []byte("content2")
	sum2 := sha256.Sum256(data2)
	digest2 := hex.EncodeToString(sum2[:])
	if err := s.Put(customCtx, digest2, data2); err == nil {
		t.Fatal("expected error on context cancelled after write")
	}

	// Test read-only directory to reach line 48 (CreateTemp error)
	roDir := filepath.Join(t.TempDir(), "ro")
	_ = os.Mkdir(roDir, 0500)
	roStore, err := filesystem.Open(roDir)
	if err == nil {
		_ = roStore.Put(ctx, digest, data)
	}

	// Test link exists but ReadFile fails (directory as target) -> line 76
	data3 := []byte("content3")
	sum3 := sha256.Sum256(data3)
	digest3 := hex.EncodeToString(sum3[:])
	target3 := filepath.Join(dir, digest3+".apk")
	_ = os.Mkdir(target3, 0700)
	if err := s.Put(ctx, digest3, data3); err == nil {
		t.Fatal("expected error when target is a directory")
	}

	// Test invalid digest in Open
	if _, err := s.Open(ctx, "not-a-valid-hex-digest"); err == nil {
		t.Fatal("expected error on invalid digest")
	}
	// Test non-existent artifact in Open
	if _, err := s.Open(ctx, strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected error on missing artifact")
	}

	// Test cancelled context in Open
	cancCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Open(cancCtx, strings.Repeat("0", 64)); !errors.Is(err, context.Canceled) {
		t.Fatal("expected context.Canceled on Open")
	}

	// Test cancelled context in Put
	putCalled := false
	mCtx := &mockCancelContext{Context: context.Background(), called: &putCalled}
	dCancel := []byte("cancel test")
	sumCancel := sha256.Sum256(dCancel)
	digCancel := hex.EncodeToString(sumCancel[:])
	if err := s.Put(mCtx, digCancel, dCancel); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Put, got %v", err)
	}

	// Test empty directory in filesystem.Open
	if _, err := filesystem.Open(""); err == nil {
		t.Fatal("expected error on empty artifact directory")
	}

	// Test conflicting Put when target file already exists with different content
	diffBytes := []byte("different content")
	targetPath := filepath.Join(dir, digest+".apk")
	// Ensure target is writable before writing conflicting content
	_ = os.Chmod(targetPath, 0600)
	if err := os.WriteFile(targetPath, diffBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, digest, data); err == nil {
		t.Fatal("expected error on immutable artifact content conflict")
	}
	// Restore valid content
	_ = os.Chmod(targetPath, 0600)
	if err := os.WriteFile(targetPath, data, 0400); err != nil {
		t.Fatal(err)
	}

	// Test Open when stored artifact has invalid integrity (tampered bytes on disk)
	tamperDigest := strings.Repeat("a", 64)
	tamperPath := filepath.Join(dir, tamperDigest+".apk")
	if err := os.WriteFile(tamperPath, []byte("tampered data"), 0400); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, tamperDigest); err == nil {
		t.Fatal("expected stored artifact integrity failure on tampered content")
	}

	// Test valid snapshot Close
	reader, err := s.Open(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	// Test Put with canceled context
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Put(canceledCtx, digest, data); err == nil {
		t.Fatal("expected error on canceled context")
	}

	// Test idempotent Put with identical content
	if err := s.Put(ctx, digest, data); err != nil {
		t.Fatalf("expected successful idempotent Put, got %v", err)
	}

	// Test invalid digest in Open and Put (lines 43, 103)
	if _, err := s.Open(ctx, "invalid-digest"); err == nil {
		t.Fatal("expected error on invalid digest in Open")
	}
	if err := s.Put(ctx, "invalid-digest", data); err == nil {
		t.Fatal("expected error on invalid digest in Put")
	}

	// Test Open on canceled context
	cancCtx2, cancel2 := context.WithCancel(ctx)
	cancel2()
	if _, err := s.Open(cancCtx2, digest); err == nil {
		t.Fatal("expected error on canceled context in Open")
	}

	// Test Open with artifact larger than 64 MiB (lines 115-116)
	largeDir := t.TempDir()
	largeStore, err := filesystem.Open(largeDir)
	if err != nil {
		t.Fatal(err)
	}
	largeDigest := strings.Repeat("f", 64)
	largeFile, err := os.OpenFile(filepath.Join(largeDir, largeDigest+".apk"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_ = largeFile.Truncate(64<<20 + 2)
	_ = largeFile.Close()
	if _, err := largeStore.Open(ctx, largeDigest); err == nil {
		t.Fatal("expected error for file > 64MiB in Open")
	}

	// Test Put when target is a directory causing ReadFile error (line 76)
	targetDir := filepath.Join(dir, strings.Repeat("e", 64)+".apk")
	_ = os.Mkdir(targetDir, 0700)
	dummyData := []byte("dummy")
	dummySum := sha256.Sum256(dummyData)
	dummyDigest := hex.EncodeToString(dummySum[:])
	_ = os.Mkdir(filepath.Join(dir, dummyDigest+".apk"), 0700)
	if err := s.Put(ctx, dummyDigest, dummyData); err == nil {
		t.Fatal("expected error when target is a directory")
	}

	// Test Put with context canceled between temp file sync and link (lines 66-68)
	mockCalled := false
	mockCtx := &mockCancelContext{Context: ctx, called: &mockCalled}
	if err := s.Put(mockCtx, digest, data); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// Test Open on nonexistent digest file (lines 106-107)
	nonexistentDigest := strings.Repeat("a", 64)
	if _, err := s.Open(ctx, nonexistentDigest); err == nil {
		t.Fatal("expected error on nonexistent digest file in Open")
	}
}

type mockCancelContext struct {
	context.Context
	called *bool
}

func (c *mockCancelContext) Err() error {
	if *c.called {
		return context.Canceled
	}
	*c.called = true
	return nil
}
