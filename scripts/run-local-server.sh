#!/bin/sh
# SPDX-License-Identifier: LGPL-3.0-only
set -eu
cd "$(dirname "$0")/.."
test_host=${1:?Usage: scripts/run-local-server.sh IP_OR_HOSTNAME [http|https]}
test_scheme=${2:-http}
case "$test_host" in *[!A-Za-z0-9.-]*|'') printf '%s\n' 'Expected an IP address or DNS hostname' >&2; exit 1;; esac
case "$test_scheme" in http|https) ;; *) printf '%s\n' 'Expected http or https' >&2; exit 1;; esac
test_dir="$HOME/.local/share/kylok-local-server/$test_host"
# This private file is created locally, not fetched from the network.
. "$test_dir/$test_scheme.env"
: "${ANDROID_HOME:?Set ANDROID_HOME to the Android SDK}"
export PATH="$ANDROID_HOME/build-tools/37.0.0:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
# Keep administrator access local by default; an empty override disables the console.
test_ui=${KYLOK_UI_LISTEN-127.0.0.1:8081}
test_listen=${KYLOK_AGENT_LISTEN:-$KYLOK_LISTEN}
if [ "$test_scheme" = https ]; then
    exec ./out/minimal-server serve -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
        -data "$KYLOK_DATA" -listen "$test_listen" -ui-listen "$test_ui" -tls-cert "$KYLOK_TLS_CERT" -tls-key "$KYLOK_TLS_KEY"
else
    exec ./out/minimal-server serve -public-url "$KYLOK_PUBLIC_URL" -apk "$KYLOK_APK" \
        -data "$KYLOK_DATA" -listen "$test_listen" -ui-listen "$test_ui"
fi
