# droidmcp

An MCP server that runs **on** an Android phone and lets an AI agent drive it:
screenshot, read the UI tree of every window, tap/swipe/type (any language),
press system keys, open apps. Browser automation, but for Android.

- The **daemon** (Go) runs as the shell user (uid 2000), the privilege level
  `adb shell` has, and speaks MCP over Streamable HTTP with a bearer token.
- A **helper** (Kotlin, started by the daemon with `app_process`) does the
  work through `UiAutomation`: screenshots, accessibility tree, input injection.
- The **app** shows the start command, token, options and a Stop button, and
  posts a persistent notification with a kill switch while the daemon runs.
- The **bridge** (Go, on your computer) is what your MCP client launches. It
  owns the adb plumbing so the client's connection never drops.

See [ARCHITECTURE.md](ARCHITECTURE.md) for how the pieces fit.

## Quick start

Requirements: Go 1.26+, JDK 17+, Android SDK (platform 36), `adb`, and an
arm64 phone on Android 11+ with USB debugging enabled (on MIUI/HyperOS also
"USB debugging (Security settings)").

```sh
sh scripts/build-apk.sh                            # Go daemon -> APK
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

Open **droidmcp** on the phone once (it writes `start.sh`), then:

```sh
adb shell sh /sdcard/Android/data/com.thozoz.droidmcp/files/start.sh
```

The easiest way to connect: tap **Copy setup prompt** in the app and paste it
into your agent (e.g. Claude Code). It does the steps below itself.

The bridge ships inside the APK for Windows, macOS and Linux (amd64/arm64).
It is a single executable with nothing to install; copy the one for your
computer from the phone and register it by absolute path:

```sh
mkdir -p ~/.droidmcp
adb pull /sdcard/Android/data/com.thozoz.droidmcp/files/bridge/droidmcp-bridge-darwin-arm64 ~/.droidmcp/droidmcp-bridge
chmod +x ~/.droidmcp/droidmcp-bridge     # not on Windows (keep the .exe name there)
claude mcp add android -- ~/.droidmcp/droidmcp-bridge
```

Because it comes from the phone, the bridge always matches the daemon's
version. (From source: `cd daemon && go install ./cmd/droidmcp-bridge`.)

The bridge waits for the phone, runs `adb forward`, and reads the daemon's
token over adb, refetching it after a daemon restart or token rotation.
Unplugging the cable is fine: the daemon keeps running, and the bridge
reconnects when the phone is back. Several phones: `droidmcp-bridge -serial <id>`.

## Tools

All coordinates (inputs and `ui_tree` bounds) are in **screenshot space**: the
screen scaled so its long edge is `-max-edge` px (default 1080). The server
converts them to device pixels.

| tool | arguments |
|---|---|
| `screenshot` | `format` (jpeg, webp), `quality` (default 70) |
| `ui_tree` | `interactive_only` (default true). Nodes get refs like `s3.e12` |
| `tap`, `long_press` | `ref` or `x`,`y`; `duration_ms` for long_press |
| `swipe` | `x1`,`y1`,`x2`,`y2`, `duration_ms` |
| `scroll` | `direction` (up/down/left/right), optional `ref` |
| `type_text` | `text`, optional `ref`; appends to the field's text |
| `key` | home, back, recents, enter, wake, power, volume_up, volume_down |
| `open_app` | `package` or `label` |
| `list_apps`, `current_app` | none |
| `wait_for` | `text` or `ref`, `timeout_ms` |
| `shell` | `cmd`; only with the shell option enabled |

A ref belongs to one snapshot. Any screen-changing tool makes every ref stale;
using one returns `stale ref, call ui_tree`. `tap` and `type_text` with a ref
use the accessibility action (`ACTION_CLICK`, `ACTION_SET_TEXT`) and fall back
to touching the node's bounds.

Known gaps: display 0 only.

## Stopping

- The **Stop** button in the app or in the notification.
- `adb shell pkill -x libdroidmcpd.so`
- **Reboot.** The daemon never starts on its own.

Stop *asks* the daemon to exit (an app cannot kill a shell-user process); the
daemon exits within 3 seconds even if a helper call is stuck. Turning off USB
or wireless debugging does **not** stop it: it is detached with `setsid` by
design.

## Security

- Binds to `127.0.0.1`; reach it through `adb forward` (the bridge does this).
- A random 256-bit bearer token guards every endpoint, compared in constant
  time. A new token is made on each start; the app can rotate it.
- The token reaches the app only through a broadcast that only the shell can
  send (the receiver requires `DUMP`), and the bridge reads it over adb. It is
  never written to shared storage.
- The helper's socket is an abstract unix socket; the daemon accepts only its
  own child (SO_PEERCRED: uid 2000 and the spawned pid).
- The `shell` tool is off unless enabled in the app.
- **LAN access** (off by default, confirmation required) serves plain HTTP:
  anyone on the same network can read the token and fully control the phone.
  Use it only on networks you trust. TLS with certificate pinning in the
  bridge is a possible next step.

Anyone holding the token controls the phone. Treat it like a password.

## Development

```sh
cd daemon && go vet ./... && go test ./...      # unit tests (auth, ui tree, ipc, helper restarts, bridge)
cd android && ./gradlew :app:assembleDebug
```

`scripts/build-phase0.sh` and `scripts/run-phase0.sh` still build and run the
APK-less Phase 0 daemon (stock `screencap`/`uiautomator`/`input` backend).
