# droidmcp

An MCP server that runs **on** an Android phone and lets an AI agent drive it:
screenshot, read the UI tree, tap/swipe/type, press system keys, open apps.
Browser automation, but for Android, with no PC in the control loop.

The daemon is written in Go and runs as the shell user (uid 2000), the same
privilege level `adb shell` has. It speaks MCP over Streamable HTTP and every
request needs a bearer token.

> **Status: Phase 0.** A Go binary pushed to `/data/local/tmp`, backed by stock
> tools (`screencap`, `uiautomator`, `input`, `monkey`). Phase 1 replaces the
> backend with a UiAutomation helper inside an APK; the tool interface stays the
> same. See [ARCHITECTURE.md](ARCHITECTURE.md).

## Quick start (Phase 0)

Requirements: Go 1.26+, `adb`, a phone with USB debugging on (on MIUI, also
"USB debugging (Security settings)").

```sh
sh scripts/build-phase0.sh   # -> daemon/bin/droidmcpd-arm64
sh scripts/run-phase0.sh     # push, start detached, adb forward, print the next command
```

`run-phase0.sh` ends by printing something like:

```sh
claude mcp add --transport http android http://127.0.0.1:8765/mcp --header "Authorization: Bearer <token>"
```

Run that, start Claude Code, and ask it to take a screenshot.

Manual steps, if you'd rather not use the script:

```sh
adb push daemon/bin/droidmcpd-arm64 /data/local/tmp/droidmcpd
adb shell chmod 755 /data/local/tmp/droidmcpd
adb shell "setsid /data/local/tmp/droidmcpd </dev/null >/data/local/tmp/droidmcpd.log 2>&1 &"
adb shell cat /data/local/tmp/droidmcp.token
adb forward tcp:8765 tcp:8765
```

Stop it:

```sh
curl -X POST -H "Authorization: Bearer <token>" http://127.0.0.1:8765/shutdown
```

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
| `type_text` | `text`, optional `ref` to tap first |
| `key` | home, back, recents, enter, wake, power, volume_up, volume_down |
| `open_app` | `package` or `label` |
| `list_apps`, `current_app` | none |
| `wait_for` | `text` or `ref`, `timeout_ms` |
| `shell` | `cmd`; only registered with `-enable-shell` |

A ref belongs to one snapshot. Any screen-changing tool (tap, swipe, key, ...)
makes every ref stale, and using one returns `stale ref, call ui_tree`.

### Phase 0 limitations

- `webp` returns an error (the Go standard library has no WebP encoder).
- `type_text` handles printable ASCII only.
- `open_app` by label matches package-name substrings, not real app labels.
- Display 0 only.

## Daemon flags

| flag | default | |
|---|---|---|
| `-addr` | `127.0.0.1:8765` | Keep loopback unless you want LAN access |
| `-token` | random | Bearer token |
| `-token-file` | `/data/local/tmp/droidmcp.token` | Written with mode 0600 |
| `-max-edge` | `1080` | Defines screenshot space |
| `-enable-shell` | `false` | Exposes the `shell` tool |

## Security

- Binds to loopback by default; reach it through `adb forward`.
- A random 256-bit bearer token is required on every endpoint, compared in constant time.
- The `shell` tool is off unless explicitly enabled.
- Anyone holding the token can fully control the phone. Treat it like a password.

## Development

```sh
cd daemon
go vet ./...
go test ./...
```
