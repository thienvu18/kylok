// SPDX-License-Identifier: LGPL-3.0-only
// Package filesystem provides atomic content-addressed APK persistence.
package filesystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/thienvu18/kylok/go/ports"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Store struct{ dir string }

// Open prepares a durable content-addressed directory; it never creates a temporary fallback.
// Usage: store, err := filesystem.Open("/var/lib/kylok/artifacts").
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("artifact directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Put verifies the digest and atomically publishes immutable bytes without replacing concurrent content.
// Usage: err := store.Put(ctx, digest, verifiedBytes).
func (s *Store) Put(ctx context.Context, digest string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if !digestPattern.MatchString(digest) || hex.EncodeToString(sum[:]) != digest {
		return errors.New("artifact digest mismatch")
	}
	// Link rather than rename: concurrent publication cannot overwrite existing bytes.
	f, err := os.CreateTemp(s.dir, ".publish-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(0400); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	target := filepath.Join(s.dir, digest+".apk")
	if err = os.Link(f.Name(), target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		existing, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, data) {
			return errors.New("immutable artifact content conflict")
		}
	}
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

type snapshot struct{ *bytes.Reader }

// Close satisfies the reader lifecycle contract; this verified snapshot owns only memory.
// Usage: defer reader.Close() after store.Open(ctx, digest).
func (snapshot) Close() error { return nil }

// Open verifies stored bytes and returns a seekable immutable snapshot, detecting on-disk corruption.
// Usage: reader, err := store.Open(ctx, digest); close reader after use.
func (s *Store) Open(ctx context.Context, digest string) (ports.ReadSeekCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !digestPattern.MatchString(digest) {
		return nil, errors.New("invalid artifact digest")
	}
	f, err := os.Open(filepath.Join(s.dir, digest+".apk"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<20+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest || len(data) > 64<<20 {
		return nil, errors.New("stored artifact integrity failure")
	}
	return snapshot{bytes.NewReader(data)}, nil
}
