// SPDX-License-Identifier: LGPL-3.0-only
package kylok_test

import (
	"testing"

	"github.com/thienvu18/kylok/go"
)

// TestVersion confirms that the Version function reports the embedded non-empty SemVer string.
func TestVersion(t *testing.T) {
	v := kylok.Version()
	if v == "" {
		t.Fatal("expected non-empty version")
	}
}
