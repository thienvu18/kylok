#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p .tools/bin
GOBIN="$PWD/.tools/bin" GOWORK=off go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
GOBIN="$PWD/.tools/bin" GOWORK=off go install connectrpc.com/connect/cmd/protoc-gen-connect-go@v1.21.1
GOBIN="$PWD/.tools/bin" GOWORK=off go install github.com/bufbuild/buf/cmd/buf@v1.70.0
.tools/bin/buf lint
.tools/bin/buf generate
