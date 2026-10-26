#!/bin/sh
# SPDX-License-Identifier: LGPL-3.0-only
set -eu
cd "$(dirname "$0")/.."
comparison=$(mktemp -d "${TMPDIR:-/tmp}/kylok-generation.XXXXXX")
trap 'rm -rf "$comparison"' EXIT HUP INT TERM
cp -R go/gen "$comparison/gen"
./scripts/generate.sh
diff -ru "$comparison/gen" go/gen
