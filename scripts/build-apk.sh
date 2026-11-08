#!/bin/sh
# Build the APK. It carries:
#   lib/arm64-v8a/libdroidmcpd.so   the daemon (runs on the phone)
#   assets/bridge/droidmcp-bridge-* the computer-side bridge for each OS/CPU,
#                                   which an agent fetches from the phone with adb pull
# Usage: scripts/build-apk.sh [debug|release]   (default: debug)
set -eu
cd "$(dirname "$0")/.."
VARIANT=${1:-debug}
JNI=android/app/src/main/jniLibs/arm64-v8a
BRIDGE=android/app/src/main/assets/bridge
mkdir -p "$JNI" "$BRIDGE"

VERSION=$(git describe --always --dirty 2>/dev/null || echo dev)
LDFLAGS="-s -w -X main.version=$VERSION"
(cd daemon && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
	go build -trimpath -ldflags "$LDFLAGS" -o "../$JNI/libdroidmcpd.so" ./cmd/droidmcpd)

rm -f "$BRIDGE"/droidmcp-bridge-*
for target in windows/amd64 darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do
	os=${target%/*}
	arch=${target#*/}
	ext=
	[ "$os" = windows ] && ext=.exe
	(cd daemon && CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
		go build -trimpath -ldflags "$LDFLAGS" -o "../$BRIDGE/droidmcp-bridge-$os-$arch$ext" ./cmd/droidmcp-bridge)
done

TASK=assembleDebug
[ "$VARIANT" = release ] && TASK=assembleRelease
(cd android && ./gradlew ":app:$TASK" --console=plain -q)
ls -l android/app/build/outputs/apk/"$VARIANT"/*.apk
