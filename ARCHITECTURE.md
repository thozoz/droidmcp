# Architecture

```
 computer                              phone
 ─────────────────────────────         ──────────────────────────────────────────────
 MCP client (Claude Code)
   │ stdio
 droidmcp-bridge ── adb forward ──────▶ libdroidmcpd.so  (Go daemon, shell uid 2000)
   reads token over adb                  │ HTTP :8765, Bearer token
                                         │ internal/mcp    tools, refs, screenshot space
                                         │ internal/device Device interface
                                         │   ├─ Helper ── abstract unix socket ──▶ app_process
                                         │   │                                     com.efe.droidmcp.helper.Main
                                         │   │                                     (UiAutomation, shell uid)
                                         │   └─ Exec   (Phase 0: screencap/uiautomator/input)
                                         └─ am broadcast ──▶ app (notification, Stop, UI)
```

## Daemon (`daemon/`)

**`internal/device`** works in device pixels and knows nothing about MCP.
`Device` is the backend contract; `NodeActor` is the optional extra for
backends that can act on nodes directly.

- `Helper` spawns the Kotlin helper with `app_process -Djava.class.path=<apk>`.
  The APK path is resolved with `pm path` on every spawn, so app updates are
  picked up. It listens on an abstract unix socket `@droidmcp-<random>` and
  accepts only a peer whose SO_PEERCRED uid is 2000 and whose pid is the child
  it spawned. Launcher app queries (`open_app`, `list_apps`) query
  `PackageManager` through the helper (`ActivityThread.systemMain().getSystemContext()`);
  `current_app` and `shell` stay on `Exec`. Field typing uses `ACTION_SET_TEXT`
  with a clipboard + paste fallback (`ACTION_PASTE` or `KEYCODE_PASTE` via the hidden
  `IClipboard` service) when refused.
- **Helper crashes.** A request whose connection dies returns an error and is
  **not** retried, since a tap may already have happened. The next request
  starts a fresh helper. After 3 restarts within a minute it stops trying
  until the window passes. A helper-side error (`ipc.RemoteError`, e.g. "node
  is gone") keeps the connection. Covered by `helper_test.go` and verified on
  a device by killing the helper.
- `Exec` (Phase 0) runs stock tools as argv vectors; only `Shell` uses `sh -c`.

**`internal/ipc`** frames: 4-byte big-endian length, 1-byte type (`J` JSON,
`B` binary), payload. A request is `{"id","op","args"}`; a response is
`{"id","ok","error","result","binary"}`, followed by a binary frame (the
screenshot) when `binary` is set. One request is in flight at a time.

**`internal/mcp`** turns the device into tools.

- *Screenshot space.* `scale = min(1, maxEdge / longEdge)`. It depends only on
  `-max-edge` and the display's long edge, so it is the same in every
  orientation; input injection uses the same rotated space as screenshots and
  the tree. The display is read once at startup and refreshed by every
  screenshot and `ui_tree`, so x,y tools never query the device.
- *Refs.* `ui_tree` stores one global snapshot `sN`, whose nodes are `sN.eM`.
  The phone has one screen, so this state is global, not per client. A
  generation counter goes up on every screen-changing tool. A ref resolves only
  if it belongs to the current snapshot and that snapshot was taken at the
  current generation. The generation is captured before the dump, so an action
  racing a dump makes the dump stale. The helper keeps the matching
  `AccessibilityNodeInfo`s for click and set-text.

**HTTP.** The MCP handler is **stateless** (nothing depends on
`Mcp-Session-Id`). With go-sdk v1.8.0 this serves both session-based clients
(protocol 2025-11-25) and sessionless 2026-07-28 clients; the SDK accepts the
latter only on stateless handlers. `/health`, `/shutdown` and `/token`
(rotate) sit behind the same token. After a shutdown request the process
exits within 3 seconds even if a helper call is stuck holding the helper lock.

**Process.** `start.sh` launches the daemon with `setsid`, and the daemon
ignores SIGHUP, so it outlives the adb session. With `-app` it grants the app
`POST_NOTIFICATIONS`, whitelists it from doze, and sends `DAEMON_STARTED`
(port, token) / `DAEMON_STOPPED` broadcasts.

## Helper (`android/.../helper`)

`Main serve <socket>` prepares a main looper (needed by
`AccessibilityInteractionClient`), connects a `UiAutomation` through its hidden
constructor (as the `uiautomator` command does) with interactive-window
retrieval enabled, and serves requests until the daemon closes the socket. It
never outlives the daemon. Touch MOVE events are injected asynchronously;
DOWN, UP and keys are synchronous, so an action is complete when it returns.

## App (`android/.../app`)

- `StatusReceiver` (exported, but the sender must hold `DUMP`, which the shell
  has and ordinary apps do not) stores the port and token in private
  preferences and shows the "running" notification with **Stop**.
- `StopReceiver` / the Stop button POST `/shutdown`.
- `DroidMcpApp` writes `assets/start.sh` to the app's external files dir.
  The write is atomic and skipped when unchanged, because the STARTED
  broadcast launches the app while `sh` is still reading that script.
- LAN and shell options are written to a `flags` file that `start.sh` reads on
  the next start.

## Bridge (`daemon/cmd/droidmcp-bridge`)

A stdio MCP server that mirrors the daemon's tools. On any connection failure
it runs `adb wait-for-device` and `adb forward`, and on 401 it refetches the
token over adb. A failed call is retried once only when that cannot repeat an
action: the dial was refused, the token was rejected, or the tool is
read-only.
