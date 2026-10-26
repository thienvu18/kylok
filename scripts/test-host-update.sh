#!/bin/sh
# SPDX-License-Identifier: LGPL-3.0-only
set -eu
umask 077
cd "$(dirname "$0")/.."
: "${JAVA_HOME:?Set JAVA_HOME to JDK 17}"
: "${ANDROID_HOME:?Set ANDROID_HOME to the Android SDK}"
export PATH="$ANDROID_HOME/build-tools/37.0.0:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
fixture=$(mktemp -d "${TMPDIR:-/tmp}/kylok-host-update.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
export KYLOK_KEYSTORE="$fixture/host.p12"
export KYLOK_KEY_ALIAS=host-update-test
KYLOK_STORE_PASSWORD=$(openssl rand -hex 24)
export KYLOK_STORE_PASSWORD
export KYLOK_KEY_PASSWORD="$KYLOK_STORE_PASSWORD"
"$JAVA_HOME/bin/keytool" -genkeypair -storetype PKCS12 -keystore "$KYLOK_KEYSTORE" \
    -alias "$KYLOK_KEY_ALIAS" -keyalg RSA -keysize 2048 -validity 2 -dname 'CN=Kylok update fixture' \
    -storepass:env KYLOK_STORE_PASSWORD -keypass:env KYLOK_KEY_PASSWORD
for version in 1 2; do
    android/gradlew -p android :minimal-dpc:assembleRelease "-PhostVersionCode=$version"
    tools/apk-metadata -apk examples/minimal-dpc/build/outputs/apk/release/minimal-dpc-release.apk > "$fixture/$version.json"
done
export KYLOK_CA_CERT="$fixture/ca.pem"
export KYLOK_TRUST_HOST=192.168.101.5
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=Kylok-test-CA \
    -addext 'basicConstraints=critical,CA:TRUE' -keyout "$fixture/ca.key" -out "$KYLOK_CA_CERT" 2>"$fixture/openssl.log"
android/gradlew -p android :minimal-dpc:assembleCustomCA -PhostVersionCode=2
tools/apk-metadata -apk examples/minimal-dpc/build/outputs/apk/customCA/minimal-dpc-customCA.apk > "$fixture/custom.json"
python3 - "$fixture" <<'PY'
import json,sys,zipfile
from pathlib import Path
fixture=Path(sys.argv[1]);first=json.loads((fixture/'1.json').read_text());second=json.loads((fixture/'2.json').read_text());custom=json.loads((fixture/'custom.json').read_text())
for key in ('package_name','receiver','signing_certificates_sha256'):
    assert first[key]==second[key]==custom[key],(key,first,second,custom)
assert first['version_code']==1 and second['version_code']==custom['version_code']==2
assert first['sha256']!=second['sha256'] and not first['debuggable'] and not second['debuggable'] and not custom['debuggable']
with zipfile.ZipFile('examples/minimal-dpc/build/outputs/apk/release/minimal-dpc-release.apk') as archive:
    assert all(archive.read(entry)!=(fixture/'ca.pem').read_bytes() for entry in archive.namelist() if archive.getinfo(entry).file_size<8192)
with zipfile.ZipFile('examples/minimal-dpc/build/outputs/apk/customCA/minimal-dpc-customCA.apk') as archive:
    # Release optimization may rename resource paths. Verify actual embedded bytes.
    assert any(archive.read(entry)==(fixture/'ca.pem').read_bytes() for entry in archive.namelist() if archive.getinfo(entry).file_size<8192)
print('Signed host update fixture: same package/receiver/signer, higher version; custom CA isolated to its variant. No device installation performed.')
PY
