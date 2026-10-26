#!/bin/sh
# SPDX-License-Identifier: LGPL-3.0-only
set -eu
umask 077
cd "$(dirname "$0")/.."
task_root=$PWD
test_host=${1:?Usage: scripts/prepare-local-server.sh IP_OR_HOSTNAME [http|https]}
test_scheme=${2:-http}
case "$test_scheme" in http|https) ;; *) printf '%s\n' 'Expected http or https' >&2; exit 1;; esac
python3 - "$test_host" <<'PY'
import re,sys
from urllib.parse import urlsplit
host=sys.argv[1]
assert re.fullmatch(r'[A-Za-z0-9.-]+',host) and urlsplit('http://'+host).hostname==host.lower(), 'Use an IP or DNS hostname, without a scheme, port or path'
PY
: "${JAVA_HOME:?Set JAVA_HOME to JDK 17}"
: "${ANDROID_HOME:?Set ANDROID_HOME to the Android SDK}"
export PATH="$ANDROID_HOME/build-tools/37.0.0:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
test_dir="$HOME/.local/share/kylok-local-server/$test_host"
mkdir -p "$test_dir/apk" "$test_dir/data"
chmod 700 "$test_dir"
if [ "$test_scheme" = https ]; then
if [ ! -f "$test_dir/ca.pem" ]; then
    openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 30 \
        -subj '/CN=Kylok local CA' -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
        -addext 'keyUsage=critical,keyCertSign,cRLSign' \
        -keyout "$test_dir/ca.key" -out "$test_dir/ca.pem" 2>"$test_dir/ca-generation.log"
fi
openssl x509 -checkend 3600 -noout -in "$test_dir/ca.pem"
if [ ! -f "$test_dir/server.pem" ]; then
    openssl req -new -newkey rsa:3072 -nodes -sha256 -subj "/CN=$test_host" \
        -keyout "$test_dir/server.key" -out "$test_dir/server.csr" 2>"$test_dir/server-generation.log"
    san_type=$(python3 - "$test_host" <<'PY'
import ipaddress,sys
try: ipaddress.ip_address(sys.argv[1]);print('IP')
except ValueError: print('DNS')
PY
)
    printf 'subjectAltName=%s:%s\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n' "$san_type" "$test_host" > "$test_dir/server.ext"
    openssl x509 -req -in "$test_dir/server.csr" -CA "$test_dir/ca.pem" -CAkey "$test_dir/ca.key" \
        -CAcreateserial -days 14 -sha256 -extfile "$test_dir/server.ext" \
        -out "$test_dir/server.pem" 2>>"$test_dir/server-generation.log"
fi
openssl verify -CAfile "$test_dir/ca.pem" "$test_dir/server.pem"
fi
export KYLOK_KEYSTORE="$test_dir/host.p12"
export KYLOK_KEY_ALIAS=kylok-host
if [ ! -f "$test_dir/signing-password" ]; then openssl rand -base64 32 > "$test_dir/signing-password"; fi
IFS= read -r KYLOK_STORE_PASSWORD < "$test_dir/signing-password"
export KYLOK_STORE_PASSWORD
export KYLOK_KEY_PASSWORD="$KYLOK_STORE_PASSWORD"
if [ ! -f "$KYLOK_KEYSTORE" ]; then
    "$JAVA_HOME/bin/keytool" -genkeypair -keystore "$KYLOK_KEYSTORE" -storetype PKCS12 \
        -alias "$KYLOK_KEY_ALIAS" -keyalg RSA -keysize 3072 -validity 3650 \
        -dname 'CN=Kylok local host' -storepass:env KYLOK_STORE_PASSWORD -keypass:env KYLOK_KEY_PASSWORD
fi
if [ "$test_scheme" = https ]; then
    export KYLOK_CA_CERT="$test_dir/ca.pem"
    export KYLOK_TRUST_HOST="$test_host"
    android/gradlew -p android :minimal-dpc:assembleCustomCA "-PhostVersionCode=${KYLOK_HOST_VERSION_CODE:-1}"
    test_apk="$task_root/examples/minimal-dpc/build/outputs/apk/customCA/minimal-dpc-customCA.apk"
else
    android/gradlew -p android :minimal-dpc:assembleRelease "-PhostVersionCode=${KYLOK_HOST_VERSION_CODE:-1}"
    test_apk="$task_root/examples/minimal-dpc/build/outputs/apk/release/minimal-dpc-release.apk"
fi
unset KYLOK_STORE_PASSWORD KYLOK_KEY_PASSWORD
make build-go
tools/apk-metadata -apk "$test_apk" > "$test_dir/apk-metadata.json"
digest=$(python3 - "$test_dir/apk-metadata.json" <<'PY'
import json,sys
print(json.load(open(sys.argv[1]))['sha256'])
PY
)
cp "$test_apk" "$test_dir/apk/$digest.apk"
python3 - "$test_dir" "$test_host" "$digest" "$test_scheme" <<'PY'
from pathlib import Path
import ipaddress,shlex,sys
directory,host,digest,scheme=sys.argv[1:]
port=8443 if scheme=='https' else 8080
try: ipaddress.ip_address(host);bind_host=host
except ValueError: bind_host='0.0.0.0'
values={'KYLOK_LOCAL_DIR':directory,'KYLOK_LOCAL_HOST':host,
 'KYLOK_PUBLIC_URL':f'{scheme}://{host}:{port}','KYLOK_LISTEN':f'{bind_host}:{port}',
 'KYLOK_APK':f'{directory}/apk/{digest}.apk','KYLOK_DATA':f'{directory}/data',
 'KYLOK_TLS_CERT':f'{directory}/server.pem','KYLOK_TLS_KEY':f'{directory}/server.key',
 'KYLOK_CA_CERT':f'{directory}/ca.pem'}
Path(directory,f'{scheme}.env').write_text(''.join(f'export {k}={shlex.quote(v)}\n' for k,v in values.items()))
print('Prepared signed host APK and configuration in',directory)
print('Run: scripts/run-local-server.sh',host,scheme)
PY
