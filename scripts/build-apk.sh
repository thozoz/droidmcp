#!/bin/sh
# Build the APK with the Go daemon packaged as lib/arm64-v8a/libdroidmcpd.so.
# Usage: scripts/build-apk.sh [debug|release]   (default: debug)
set -eu
cd "$(dirname "$0")/.."
VARIANT=${1:-debug}
JNI=android/app/src/main/jniLibs/arm64-v8a
mkdir -p "$JNI"

VERSION=$(git describe --always --dirty 2>/dev/null || echo dev)
(cd daemon && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
	go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "../$JNI/libdroidmcpd.so" ./cmd/droidmcpd)

TASK=assembleDebug
[ "$VARIANT" = release ] && TASK=assembleRelease
(cd android && ./gradlew ":app:$TASK" --console=plain -q)
ls android/app/build/outputs/apk/"$VARIANT"/*.apk
