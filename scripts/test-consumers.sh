#!/bin/sh
# SPDX-License-Identifier: LGPL-3.0-only
set -eu
umask 077
cd "$(dirname "$0")/.."
task_root=$PWD
: "${JAVA_HOME:?Set JAVA_HOME to JDK 17}"
: "${ANDROID_HOME:?Set ANDROID_HOME to the Android SDK}"
case "${KYLOK_REMOTE_SDK:-}" in
    ''|go|android|all) ;;
    *) printf '%s\n' 'KYLOK_REMOTE_SDK must be empty, go, android or all' >&2; exit 1;;
esac
export PATH="$ANDROID_HOME/build-tools/37.0.0:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
fixture=$(mktemp -d "${TMPDIR:-/tmp}/kylok-consumers.XXXXXX")
cleanup() {
    # Go module caches are read-only by default. Only this fresh fixture is changed.
    chmod -R u+w "$fixture"
    rm -rf "$fixture"
}
trap cleanup EXIT HUP INT TERM
python3 scripts/check-distribution.py out/release
export KYLOK_CONSUMER_APK="$fixture/android/build/outputs/apk/release/fresh-kylok-host-release.apk"
export KYLOK_GO_VERSION=$(cat go/VERSION)
export KYLOK_ANDROID_VERSION=$(cat android/VERSION)
export KYLOK_MAVEN_REPO="$task_root/out/release/maven"
case "${KYLOK_REMOTE_SDK:-}" in android|all) export KYLOK_MAVEN_REPO=https://repo.maven.apache.org/maven2;; esac
KYLOK_AAR_SHA256=$(python3 - <<'PY'
import json
from pathlib import Path
m=json.loads(Path('out/release/kylok-release.json').read_text())
print(next(value['sha256'] for name,value in m['files'].items() if name.endswith('.aar')))
PY
)
export KYLOK_AAR_SHA256
export KYLOK_PROXY_CACHE="$(go env GOMODCACHE)/cache/download"
cp -R tests/consumers/go "$fixture/go"
cp -R tests/consumers/android "$fixture/android"
printf '%s\n' 'android.useAndroidX=true' 'org.gradle.jvmargs=-Xmx2g -Dfile.encoding=UTF-8' > "$fixture/android/gradle.properties"
python3 - "$fixture" "$task_root" <<'PY'
import os,sys
from pathlib import Path
fixture,root=map(Path,sys.argv[1:])
v=os.environ['KYLOK_GO_VERSION']
(fixture/'go/go.mod').write_text(f'module consumer.invalid/kylok-host\n\ngo 1.27.1\n\nrequire github.com/thienvu18/kylok/go v{v}\n')
proxy=(root/'out/release/go-proxy').as_uri()
cache=Path(os.environ['KYLOK_PROXY_CACHE']).as_uri()
(fixture/'proxy').write_text(f'{proxy},{cache},https://proxy.golang.org')
PY
# Build from only Maven coordinates, including minification and merged DPC resources.
android/gradlew -p "$fixture/android" verifySdkArtifact assembleDebug assembleRelease lintDebug
tools/apk-metadata -apk "$fixture/android/build/outputs/apk/release/fresh-kylok-host-release.apk" > "$fixture/host.json"
python3 - "$fixture" <<'PY'
import json,sys,zipfile
from pathlib import Path
p=Path(sys.argv[1]);m=json.loads((p/'host.json').read_text())
assert m['package_name']=='dev.kylok.consumer' and not m['debuggable'],m
assert m['receiver']=='io.github.thienvu18.kylok.dpc.KylokDeviceAdminReceiver',m
apk=p/'android/build/outputs/apk/release/fresh-kylok-host-release.apk'
with zipfile.ZipFile(apk) as z:
    dex=b''.join(z.read(n) for n in z.namelist() if n.endswith('.dex'))
    for name in (b'AgentDatabase_Impl',b'EnrollmentWorker'):
        assert name in dex, f'Reflective/persisted component stripped: {name}'
print('Fresh Maven host metadata and minification verified.')
PY
(
    cd "$fixture/go"
    export GOWORK=off GOMODCACHE="$fixture/modules" GOPRIVATE= GONOPROXY=
    export GONOSUMDB=github.com/thienvu18/kylok/go
    export GOPROXY=$(cat "$fixture/proxy")
    case "${KYLOK_REMOTE_SDK:-}" in go|all) export GONOPROXY=github.com/thienvu18/kylok/go GONOSUMDB=;; esac
    go mod tidy
    go list -m -json github.com/thienvu18/kylok/go > "$fixture/module.json"
    python3 - "$fixture/module.json" "$fixture/modules" "$task_root/out/release" <<'PY'
import json,sys,zipfile
from pathlib import Path
m=json.loads(Path(sys.argv[1]).read_text())
assert not m.get('Replace'),m
assert Path(m['Dir']).is_relative_to(Path(sys.argv[2])),m
assert m['Version']=='v'+__import__('os').environ['KYLOK_GO_VERSION'],m
prefix=m['Path']+'@'+m['Version']+'/'
archive=Path(sys.argv[3])/'go-proxy'/m['Path']/'@v'/(m['Version']+'.zip')
with zipfile.ZipFile(archive) as z:
    for name in z.namelist():
        assert (Path(m['Dir'])/name.removeprefix(prefix)).read_bytes()==z.read(name),name
PY
    go test -race -v ./...
)
printf '%s\n' 'Fresh Go host uses the new minified Android host APK; both SDKs resolve from packages without workspace replacements or SDK source inclusion. No physical device execution.'
