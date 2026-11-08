#!/bin/sh
# Cross-compile the Phase 0 daemon. Static Linux/arm64 binary: no NDK needed.
set -eu
cd "$(dirname "$0")/../daemon"
mkdir -p bin
VERSION=$(git describe --always --dirty 2>/dev/null || echo dev)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
	go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o bin/droidmcpd-arm64 ./cmd/droidmcpd
echo "built daemon/bin/droidmcpd-arm64 ($VERSION)"
