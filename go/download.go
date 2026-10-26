// SPDX-License-Identifier: LGPL-3.0-only
package kylok

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/thienvu18/kylok/go/provisioning"
)

// Serve immutable managed APK ranges only after device, operation and lease authorization.
func (s *SDK) applicationHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(405)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/artifacts/"), "/")
		if len(parts) != 2 || !devicePattern.MatchString(parts[0]) || !strings.HasSuffix(parts[1], ".apk") {
			http.NotFound(w, r)
			return
		}
		digest := strings.TrimSuffix(parts[1], ".apk")
		credential, ok := bearer(r.Header.Get("Authorization"))
		id := r.Header.Get("X-Kylok-Device-ID")
		if !ok || !devicePattern.MatchString(id) || !digestPattern.MatchString(digest) {
			http.Error(w, "Unauthorized", 401)
			return
		}
		a, err := s.store.AuthorizeDownload(r.Context(), id, hash(credential), parts[0], digest, r.Header.Get("X-Kylok-Lease-ID"), s.clock().UTC())
		if err != nil {
			http.Error(w, "Unauthorized", 403)
			return
		}
		reader, err := s.artifacts.Open(r.Context(), digest)
		if err != nil {
			http.Error(w, "Artifact unavailable", 503)
			return
		}
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, provisioning.MaxAPKBytes+1))
		sum := sha256.Sum256(data)
		if err != nil || int64(len(data)) != a.SizeBytes || len(data) > provisioning.MaxAPKBytes || hex.EncodeToString(sum[:]) != digest {
			http.Error(w, "Artifact integrity failure", 500)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("ETag", `"`+digest+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, a.PackageName+".apk", time.Time{}, bytes.NewReader(data))
	})
}
