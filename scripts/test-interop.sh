#!/bin/sh
# SPDX-License-Identifier: LGPL-3.0-only
set -eu
cd "$(dirname "$0")/.."
task_root=$PWD
test_mode=${1:-https}
case "$test_mode" in http|https) ;; *) printf '%s\n' 'Expected http or https' >&2; exit 1;; esac
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/kylok-interop.XXXXXX")
server_pid=
cleanup() {
    if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
    # Only the fresh, task-owned fixture directory is removed.
    rm -rf "$test_dir"
}
trap cleanup EXIT HUP INT TERM
: "${ANDROID_HOME:?Set ANDROID_HOME to an SDK with platform android-37.0 and build-tools 37.0.0}"
export PATH="$ANDROID_HOME/build-tools/37.0.0:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
android/gradlew -p android :minimal-dpc:assembleDebug
test_apk="$task_root/examples/minimal-dpc/build/outputs/apk/debug/minimal-dpc-debug.apk"
KYLOK_TEST_APK="$test_apk" GOWORK=off go -C go test ./provisioning -run TestRealSignedAPK -count=1
GOWORK=off go -C examples/minimal-server build -o "$test_dir/server" .
test_port=$(python3 - <<'PY'
import socket
with socket.socket() as s:
    s.bind(('127.0.0.1',0))
    print(s.getsockname()[1])
PY
)
if [ "$test_mode" = https ]; then test_origin="https://localhost:$test_port"; else test_origin="http://127.0.0.1:$test_port"; fi
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=localhost \
    -addext 'subjectAltName=DNS:localhost' -keyout "$test_dir/tls.key" -out "$test_dir/tls.crt" 2>"$test_dir/openssl.log"
"$test_dir/server" enroll -public-url "$test_origin" -apk "$test_apk" \
    -allow-debug-apk -data "$test_dir/data" -scope interop -out "$test_dir/enrollment"
"$test_dir/server" serve -public-url "$test_origin" -apk "$test_apk" \
    -allow-debug-apk -data "$test_dir/data" -listen "127.0.0.1:$test_port" \
    -tls-cert "$test_dir/tls.crt" -tls-key "$test_dir/tls.key" >"$test_dir/server.log" 2>&1 &
server_pid=$!
attempt=0
while ! curl --silent --fail --cacert "$test_dir/tls.crt" "$test_origin/$(python3 - "$test_dir/enrollment.json" <<'PY'
import json,sys,urllib.parse
print(urllib.parse.urlparse(json.load(open(sys.argv[1]))['android.app.extra.PROVISIONING_DEVICE_ADMIN_PACKAGE_DOWNLOAD_LOCATION']).path.lstrip('/'))
PY
)" -o "$test_dir/download.apk"; do
    if ! kill -0 "$server_pid" 2>/dev/null; then cat "$test_dir/server.log"; exit 1; fi
    attempt=$((attempt+1))
    if [ "$attempt" -ge 30 ]; then cat "$test_dir/server.log"; exit 1; fi
    sleep 1
done
cmp "$test_apk" "$test_dir/download.apk"
KYLOK_INTEROP_QR="$test_dir/enrollment.json" KYLOK_INTEROP_CERT="$test_dir/tls.crt" \
    KYLOK_INTEROP_SERVER="$test_dir/server" KYLOK_INTEROP_DATA="$test_dir/data" KYLOK_INTEROP_APK="$test_apk" \
    android/gradlew -p android :kylok-dpc:testDebugUnitTest --rerun-tasks
"$test_dir/server" list -public-url "$test_origin" -apk "$test_apk" \
    -allow-debug-apk -data "$test_dir/data" -scope interop > "$test_dir/devices.json"
python3 - "$test_dir/devices.json" <<'PY'
import json,sys
devices=json.load(open(sys.argv[1]))
assert len(devices)==1 and devices[0]['state']=='MANAGED' and devices[0]['scope_id']=='interop',devices
print('Kotlin/Java Lite -> HTTP2 gRPC -> Connect-Go/SQLite: one authenticated device verified.')
PY
