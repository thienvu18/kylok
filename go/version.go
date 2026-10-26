// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var sdkVersion string

// Version identifies this SDK's source package, independently of its host.
// Usage: version := kylok.Version().
func Version() string { return strings.TrimSpace(sdkVersion) }

// ProtocolVersion is the current wire version. Development versions must be
// paired from the same source revision; the number alone is not compatibility.
const ProtocolVersion uint32 = 1
