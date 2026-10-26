#!/bin/sh
# SPDX-License-Identifier: LGPL-3.0-only
set -eu
cd "$(dirname "$0")/.."
.tools/bin/buf lint
.tools/bin/buf format --diff --exit-code
.tools/bin/buf build -o /dev/null
