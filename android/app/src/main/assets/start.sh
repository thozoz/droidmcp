#!/system/bin/sh
# Starts the droidmcp daemon as the shell user. Run it from a computer:
#
#   adb shell sh /sdcard/Android/data/com.efe.droidmcp/files/start.sh [daemon flags]
#
# e.g. -enable-shell, -addr 0.0.0.0:8765 (LAN). The app rewrites this file on
# every launch, so it always matches the installed version.

PKG=com.efe.droidmcp
LOG=/data/local/tmp/droidmcpd.log
TOKEN_FILE=/data/local/tmp/droidmcp.token

if [ "$(id -u)" != 2000 ]; then
    echo "run this through 'adb shell' (needs the shell user, uid 2000)" >&2
    exit 1
fi

APK=$(pm path "$PKG" | grep -m1 'base.apk' | sed 's/^package://')
if [ -z "$APK" ]; then
    echo "$PKG is not installed" >&2
    exit 1
fi
BIN="$(dirname "$APK")/lib/arm64/libdroidmcpd.so"
if [ ! -x "$BIN" ]; then
    echo "daemon binary missing at $BIN" >&2
    exit 1
fi

# Replace a running daemon (this or the Phase 0 one) and wait for its port.
pkill -x libdroidmcpd.so
pkill -x droidmcpd
for _ in 1 2 3 4 5 6 7 8 9 10; do
    pgrep -x libdroidmcpd.so >/dev/null || pgrep -x droidmcpd >/dev/null || break
    sleep 0.3
done
rm -f "$TOKEN_FILE"

# setsid: keep running after the adb session ends (USB unplugged, wireless debugging off).
setsid "$BIN" -apk "$PKG" -app "$PKG" -token-file "$TOKEN_FILE" "$@" </dev/null >"$LOG" 2>&1 &

for _ in 1 2 3 4 5 6 7 8 9 10; do
    [ -s "$TOKEN_FILE" ] && break
    sleep 0.5
done
if [ ! -s "$TOKEN_FILE" ]; then
    echo "daemon failed to start; log:" >&2
    cat "$LOG" >&2
    exit 1
fi
echo "droidmcp daemon running (log: $LOG)."
echo "On the computer: adb forward tcp:8765 tcp:8765, then use the bridge or the token in $TOKEN_FILE."
