#!/bin/sh
# Push the Phase 0 daemon, (re)start it detached on the phone, forward the
# port and print the `claude mcp add` command. Extra args go to droidmcpd,
# e.g. `scripts/run-phase0.sh -enable-shell`. From Git Bash, export
# MSYS_NO_PATHCONV=1 first or /data/... arguments get rewritten to Windows paths.
set -eu
export MSYS_NO_PATHCONV=1 # stop Git Bash rewriting /data/... into Windows paths

PORT=${PORT:-8765}
BIN=/data/local/tmp/droidmcpd
TOKEN_FILE=/data/local/tmp/droidmcp.token
LOG=/data/local/tmp/droidmcpd.log
cd "$(dirname "$0")/.."

adb get-state >/dev/null
adb push daemon/bin/droidmcpd-arm64 "$BIN" >/dev/null
adb shell chmod 755 "$BIN"
# Stop any previous daemon and wait until it has released the port.
adb shell "pkill droidmcpd; for i in 1 2 3 4 5 6 7 8 9 10; do pgrep droidmcpd >/dev/null || break; sleep 0.3; done; rm -f $TOKEN_FILE" || true
adb shell "setsid $BIN -addr 127.0.0.1:$PORT $* </dev/null >$LOG 2>&1 &"

TOKEN=
for _ in 1 2 3 4 5 6 7 8 9 10; do
	TOKEN=$(adb shell cat "$TOKEN_FILE" 2>/dev/null | tr -d '\r\n') || true
	[ -n "$TOKEN" ] && break
	sleep 0.5
done
if [ -z "$TOKEN" ]; then
	echo "daemon did not start; log:" >&2
	adb shell cat "$LOG" >&2
	exit 1
fi

adb forward "tcp:$PORT" "tcp:$PORT" >/dev/null
echo "daemon running. Add it to Claude Code with:"
echo
echo "  claude mcp add --transport http android http://127.0.0.1:$PORT/mcp --header \"Authorization: Bearer $TOKEN\""
