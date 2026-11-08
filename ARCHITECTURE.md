# Architecture

```
 MCP client (Claude Code)
      │  Streamable HTTP + Bearer token        (adb forward tcp:8765)
      ▼
 droidmcpd (Go, shell uid 2000)
   internal/auth    bearer middleware
   internal/mcp     tools, ref state, screenshot-space scaling
   internal/device  Device interface
        ├─ Exec    (Phase 0)  screencap / uiautomator / input / monkey
        └─ Helper  (Phase 1)  Kotlin UiAutomation helper over stdin/stdout
```

## Layers

**`internal/device`** works only in device pixels and knows nothing about MCP.
`Device` is the backend contract. `Exec` implements it by running stock tools
as argv vectors (`exec.Command`), and never builds a shell string. Only the
`Shell` method uses `sh -c`.

- Screenshots come from raw `screencap` output: a 12- or 16-byte header, then
  RGBA_8888 or RGBX_8888 pixels. Any other pixel format is an error. There is no
  PNG encode/decode round trip.
- UI trees come from `uiautomator dump`. It is retried once on "could not get
  idle state".
- Input-injection refusals (`SecurityException` / `INJECT_EVENTS`, e.g. MIUI)
  return `ErrInjectionBlocked`, which tells the user how to fix it.
- An all-black screenshot carries a FLAG_SECURE warning instead of failing.

**`internal/mcp`** turns the device into tools.

- *Screenshot space.* `scale = min(1, maxEdge / longEdge)`. It depends only on
  `-max-edge` and the display's long edge, so it is the same in every
  orientation. `input tap` uses the same rotated space that `screencap` and
  `uiautomator` report. Every tool coordinate is divided by `scale` on the way
  in, and every bound is multiplied by it on the way out.
- *Display cache.* The display is read once at startup. After that, every
  `screenshot` (raw header size) and `ui_tree` refreshes it. Tools that take
  x,y read only the cache and never run a command, which keeps taps fast.
  Rotation (`dumpsys input`) is read only for screenshot/ui_tree headers.
- *Refs.* `ui_tree` stores one global snapshot with id `sN`, and its nodes get
  refs `sN.eM`. The phone has a single screen, so the state is global rather
  than per client. A generation counter goes up on every screen-changing tool.
  A ref resolves only if it belongs to the current snapshot *and* that
  snapshot was taken at the current generation; otherwise the result is
  `stale ref, call ui_tree`. The generation is captured before the dump starts,
  so a tap that races a dump makes the dump stale. `wait_for(ref)` matches the
  old node by signature (class, id, text, desc) and returns a fresh ref.

**HTTP.** The MCP handler runs in **stateless** mode, so nothing depends on
`Mcp-Session-Id`. With go-sdk v1.8.0 this serves both session-based clients
(protocol 2025-11-25: initialize, then calls) and sessionless 2026-07-28
clients. The SDK accepts 2026-07-28 only on stateless handlers. Both are
covered by `server_test.go`. `/health` and `/shutdown` sit behind the same
token as `/mcp`.

**Process.** The daemon ignores SIGHUP and is started with `setsid`, so it
survives the adb session ending. SIGTERM or `POST /shutdown` shuts it down
gracefully.

## Phase 1 (planned)

A Gradle app packages the daemon as `jniLibs/arm64-v8a/libdroidmcpd.so`
(`useLegacyPackaging = true`). `start.sh` launches it, and the daemon spawns
`app_process … com.efe.droidmcp.helper.Main` as shell uid. The helper uses
UiAutomation for screenshots, input injection and the window tree, and talks to
Go over length-prefixed JSON frames. `helperDevice` implements the same
`Device` interface, which brings WebP, Unicode text, `ACTION_CLICK` and label
lookup. The UI app holds the token, the LAN and shell toggles, and a
notification with a kill switch.
