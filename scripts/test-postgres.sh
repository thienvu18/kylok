#!/bin/sh
# SPDX-License-Identifier: LGPL-3.0-only
# Own one disposable real PostgreSQL fixture; never connect these destructive failure tests to a user database.
set -eu
umask 077
cd "$(dirname "$0")/.."
image='postgres:18.6-alpine@sha256:10a5c69a83035ab5239f5620d708520e8b28f5aa1f2b20143e6024532edadfb5'
fixture=$(mktemp -d "${TMPDIR:-/tmp}/kylok-postgres.XXXXXX")
container=
cleanup() {
    if [ -n "$container" ]; then docker rm -fv "$container" >/dev/null; fi
    rm -rf "$fixture"
}
trap cleanup EXIT HUP INT TERM
# The password is disposable and never appears in argv or test output.
python3 - "$fixture/env" <<'PY'
import secrets,sys
from pathlib import Path
Path(sys.argv[1]).write_text('POSTGRES_PASSWORD='+secrets.token_hex(24)+'\nPOSTGRES_USER=kylok\nPOSTGRES_DB=kylok\n')
PY
port=$(python3 - <<'PORT'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    print(sock.getsockname()[1])
PORT
)
container=$(docker run -d --env-file "$fixture/env" -p "127.0.0.1:$port:5432" "$image")
export KYLOK_POSTGRES_CONTAINER="$container"
attempt=0
until docker exec "$container" pg_isready -U kylok -d kylok >/dev/null 2>&1; do
    attempt=$((attempt+1))
    if [ "$attempt" -ge 60 ]; then docker logs "$container" >&2; exit 1; fi
    sleep 1
done
port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$container")
KYLOK_POSTGRES_DSN=$(python3 - "$fixture/env" "$port" <<'PY'
from pathlib import Path
import sys
password=Path(sys.argv[1]).read_text().splitlines()[0].split('=',1)[1]
print('postgres://kylok:'+password+'@127.0.0.1:'+sys.argv[2]+'/kylok?sslmode=disable')
PY
)
export KYLOK_POSTGRES_DSN
GOWORK=off go -C go test -race -count=1 -v ./adapters/postgres
