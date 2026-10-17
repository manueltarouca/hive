# hive: architecture

A terminal multiplexer for agent-heavy work. It replaces tmux.

## Principles

- **Stable names.** CLI verbs and flags, method names and arguments, event
  names and exit codes are a contract. Scripts and agents rely on them, so
  they don't change.
- **One API.** The app, the CLI and scripts all use the server's API. There
  is nothing only the app can do.
- **Agents are first-class.** Persistence, agent hooks and resume live in
  their own packages and methods (`agent.*`).

## Decisions (2026-10-17)

| Topic | Choice | Why |
|---|---|---|
| Server + CLI | Go, one binary `hive` | goroutines suit PTY fan-out; tsnet gives built-in Tailscale later; gopher-lua is Lua 5.1 |
| VT core | libghostty-vt via `go.mitchellh.com/libghostty` (cgo, static) | Ghostty's own parser and state: formatter, snapshots, OSC 7501, OSC 133, key encoder |
| First client | native macOS app: Swift + AppKit + Metal (milestone 2) | no double emulation; each pane renders a client-side libghostty-vt fed by the server stream |
| Wire | JSON over HTTP on a Unix socket (`POST /v1/call`), NDJSON event stream (`GET /v1/events`), WebSocket per attached block (M2) | curl-able, the same transport local and remote |
| API | self-describing: every method has a name, a description and JSON Schema in/out; `hive api list/describe/call` | lets CLI, Lua and agents bind to it |
| Persistence | state journal plus a libghostty snapshot per block, restored at server start | survives a machine reboot: layout, labels and scrollback come back, shells restart in their cwd |
| Agents | harness hooks report session IDs; restore relaunches `claude --resume <id>`, `codex resume <id>`… | panes of Claude Code / Codex / OpenCode / Gemini / Grok come back mid-conversation |
| Status | OSC 7501 (Program Status Protocol) records per block, exposed as events and `program_status` | an agent dashboard that shows who's blocked or done |

## Model

server → session → window (tab) → layout tree (`split{dir,ratio,a,b}` | `block`) → block (terminal: PTY + headless VT)

IDs are `kind:<uuidv7>`, immutable and never reused. Labels are optional.
Targets resolve in this order: full ID, label (case-insensitive), 1-based
position, then an ID suffix of 4 or more characters. Every layout mutation
bumps the session's `revision`.

## Layout on disk

- socket: `$XDG_RUNTIME_DIR/hive/server.sock`, or `$TMPDIR/hive-$UID/server.sock` on macOS
- state: `$XDG_STATE_HOME/hive` (default `~/.local/state/hive`): `state.json`, `blocks/<id>.snap`, `server.log`
- config: `~/.config/hive/init.lua` (milestone 3)

## Milestones

1. **Headless core.** Server, CLI, sessions/windows/splits, send/send-key/capture/wait, events, api introspection, persistence + restore, agent hooks + resume.
2. **Mac app.** Attach stream, Metal grid renderer from libghostty-vt render state, tabs/splits, a sidebar with agent status.
3. **Lua.** `init.lua` keymap/actions/modes, `hive do` scripts, stable API names.
4. **Remote.** tsnet listener, `hive -S host`, multi-host sidebar.

## Milestone 1 plan (detail)

**Packages**

| Package | Responsibility |
|---|---|
| `internal/id` | typed UUIDv7 IDs, short suffixes |
| `internal/vt` | wraps libghostty Terminal behind a mutex: feed, resize, format, snapshot/restore, key encoding. Effects (title, pwd, bell, OSC 7501, OSC 133, notifications, clipboard) are turned into Go hooks, queued and fired after the write returns, so a hook never re-enters the lock |
| `internal/block` | a terminal block: PTY (creack/pty) plus a vt instance, a read loop feeding the vt and broadcasting bytes to attach subscribers, child wait → exit event, foreground-process poll (`TIOCGPGRP` + sysctl) → `process_changed` |
| `internal/session` | session/window/layout tree ops: new_window, new_split, close (tree collapse), move/swap, zoom, focus, resize ratio, labels, revision |
| `internal/api` | method registry: name, description, Go in/out types → JSON Schema by reflection, strict decoding (unknown field → `bad_request`), errors `{status, code, message}` |
| `internal/server` | owns the state; HTTP mux on the unix socket: `POST /v1/call`, `GET /v1/events?session=&all=` (NDJSON), `GET /v1/blocks/{id}/attach` (WebSocket, M2); an event bus with per-subscriber buffers (slow consumers dropped, never blocking PTY reads) |
| `internal/persist` | debounced (1s) `state.json` plus `blocks/<id>.snap`; atomic write by temp file + rename; periodic flush every 30s, and on SIGTERM |
| `internal/agents` | harness adapters: hook payload → `{harness, session_id, cwd}` on the block; resume argv templates; installers for Claude Code (`settings.json` SessionStart hook) and Codex (`hooks.json`) |
| `cmd/hive` | CLI: auto-starts the server; global `-s -w -b -S --json`; exit codes 0/1/2/3/4/130 |

**API methods (M1)**

| Group | Methods |
|---|---|
| server | `server.version`, `server.shutdown`, `api.list`, `api.describe` |
| sessions | `session.list`, `session.create{label,cwd,command}`, `session.view`, `session.set_label`, `session.close` |
| windows | `session.new_window{layout\|command,cwd,label,focus}`, `session.close_window`, `session.focus_window`, `session.set_window_label`, `session.move_window` |
| blocks/layout | `session.new_split{block_id,direction: right\|left\|above\|below,ratio,command,cwd,label,focus,keep_open}`, `session.focus_block`, `session.zoom_block`, `session.swap_blocks`, `session.resize_pane`, `session.set_block_label`, `block.close`, `block.inspect` |
| terminal | `terminal.write{data}`, `terminal.send_key{key}`, `terminal.format{format: text\|vt\|html,trim,unwrap}`, `terminal.resize{cols,rows}`, `terminal.process`, `terminal.program_status`, `terminal.wait{timeout}` (long-poll until child exits) |
| targets | `target.resolve{kind,target,session_id}` → id, or a `not_found`/`ambiguous` error (CLI exit 3) |
| agents | `agent.report{block_id,harness,session_id,cwd,event}` (called from hooks), `agent.list` (every agent block across sessions, with its status) |

**Events (NDJSON):** session events wrap block events (`session_event` →
`block_event`). Additionally `agent_changed{block_id, harness, session_id}`.

**Panes' environment:**
- `HIVE_SERVER` (socket path), `HIVE_SESSION`, `HIVE_BLOCK`
- `TERM=xterm-256color`, `COLORTERM=truecolor`, `TERM_PROGRAM=hive`
- login-shell environment (`$SHELL -l`)

**Restore flow at server start:**
1. Read `state.json` and rebuild sessions, windows and the layout with the same IDs.
2. For each block, decode `<id>.snap` into a new vt.
3. Write the reset preamble `ESC[?1049l ESC[!p ESC[0m`, then a dim
   `── restored <timestamp> ──` line.
4. Spawn either the agent resume command (if `agent` is set and the harness
   has a template) or the user's shell in the saved cwd (`pwd` from OSC 7,
   else the foreground process cwd, else the original cwd).
5. Programs that weren't agents or shells aren't re-run (decision:
   "layout + scrollback + resume agents").

**Agent identity flow (Claude Code):**
1. `hive agent install claude` adds a SessionStart hook to the Claude config:
   `hive agent hook claude`.
2. Claude runs the hook inside a hive pane (so `HIVE_BLOCK` is set) and passes
   `{session_id, cwd, ...}` as JSON on stdin.
3. The hook calls `agent.report`, which persists `agent{harness:"claude", session_id}` on the block.
4. On restore, the pane runs `claude --resume <session_id>` in its cwd.

Codex follows the same flow with `codex resume <id>`. OpenCode, Gemini and Grok
get user-configurable templates until their resume flags are verified.

**Testing:** red-green, per package. The server is tested in-process over a
temp unix socket. One end-to-end test runs the binary: start the server,
split, send, capture, kill the server, restart it and assert the scrollback
and layout are restored.

**Known risks / open questions:**
- cgo plus libghostty thread-safety: one goroutine touches a Terminal at a time
  (mutex). Hooks are deferred.
- Snapshot cost on large scrollback: snapshot only blocks dirtied since the
  last flush; cap scrollback (default 10k lines).
- Server crash = all PTYs die (as in tmux). Restore covers it, as long
  as the journal is fresh.
- Is a long-poll for `terminal.wait` acceptable vs a streaming RPC?
- Claude/Codex hook payload fields are assumed (`session_id`); verify against
  real runs.

## Adopted from the design review (2026-10-17)

These amendments win over the M1 detail above where they conflict.

1. **VT write-back.**
   - `WritePty` bytes are queued during `VTWrite` and flushed to the PTY in
     order, right after it returns.
   - Reply callbacks (enquiry, size, DA, color scheme, clipboard read) answer
     synchronously from cached state.
   - Notification callbacks are queued and fired after the lock is released.
   - No hook may take the block lock (`RenderHold` also fires from
     `Reset`/`Resize`).
2. **Attach stream.**
   - One WebSocket per client, with frames tagged by block.
   - A subscription opens with a libghostty snapshot plus `seq` (a byte
     offset), followed by `seq`-tagged byte frames.
   - Each subscriber gets a ring buffer. On overflow the server sends
     `resync_required` and the client re-requests a snapshot. Bytes are never
     silently dropped.
3. **Clients are objects.**
   - `client.register` / `client.list` / `client_connected`.
   - Each block has a `size_owner`: the most recent attaching client with
     size authority sets the PTY size, and the others letterbox.
4. **Durability.** `agent.report` flushes `state.json` synchronously, and a
   block is snapshotted on `child_exited`.
5. **Claude hooks.**
   - SessionStart (any `source`, overwriting `session_id`: `/clear` mints a
     new one) and SessionEnd (the agent ended, so don't resume it).
   - Notification → `blocked`, Stop → `done`, UserPromptSubmit → `working`.
     These drive agent status alongside OSC 7501.
6. **Working directory.** Sources in order: the hook's `cwd` (agents), then
   OSC 7, then the foreground process cwd via `proc_pidinfo(PROC_PIDVNODEPATHINFO)`,
   then the original cwd.
7. **Waiting and events.**
   - `wait` subscribes to `child_exited` through the events stream; the RPC
     is sugar with a server-side cap and returns `timed_out`.
   - Events carry `seq`, accept `?since=`, and send heartbeats.
8. **Splits and scrollback.**
   - `new_split` accepts a nested `layout`.
   - Scrollback is capped in bytes (`WithMaxScrollbackBytes`).
   - Snapshots are written with `SnapshotWriteTo` straight to a temp file.
9. **Event fields.** Title, pwd and status events carry `owner{pid,name}`.
10. **Harness resume templates** (no installers):
    - `codex resume <id>`
    - `opencode -s <id>`
    - `gemini --resume <id>`
    - `grok --resume <id>`
11. **Cut from M1:** `move_window`, `swap_blocks`, `html` format.

**Build order:** vt → block → api/server/events/clients → session tree + CLI
→ persist/restore + e2e → Claude agents → attach stream with a Go test client
(the exit criterion for M1).

## Status

**Milestone 1: done (2026-10-17).**
- Code review by Fable 5.1 found 8 bugs, all fixed with regression tests
  (`internal/server/review_test.go` and others).
- Should-fix items adopted: inner event shape, `since` gap →
  `resync_required`, `size_changed`, async WebSocket calls, Origin guard,
  bounded reply writer, Claude `notification_type` + `PostToolUse`, OSC 7
  host check, process-group kill.
- Deferred, from the same review:
  - Event detail: `process_changed{generation, ancestors}`,
    `program_status_changed.reason`, `program_status_removed`.
  - The hook installer embeds an absolute binary path, so moving `bin/hive`
    breaks the hooks.
  - Tests for: shutdown with a WebSocket attached; a child ignoring SIGHUP
    with a grandchild holding the pty.

## Milestone 2 plan: the Mac app (draft, under review)

A native app that attaches to the local hive server over `/v1/attach`.
Each pane mirrors its block in a client-side libghostty-vt terminal and is
drawn with Metal.

**Layout:** `apps/macos/`, a SwiftPM package, so it builds with the Command
Line Tools.
- `script/bundle.sh` assembles `Hive.app` (Info.plist, icon, ad-hoc codesign).
- An Xcode project only for signing and notarization when publishing.

| Module | Responsibility |
|---|---|
| `CGhosttyVT` | system-library/C target over `third_party/ghostty-vt` (headers + the shipped `module.modulemap`, static `libghostty-vt.a`) |
| `HiveKit` | the protocol client: a hand-rolled RFC 6455 client (masking, fragmentation, ping/pong) over a raw `NWConnection` to `NWEndpoint.unix`. Binary frame parser; `call` with ids; event stream with `seq`; reconnect with `since`. Remote hosts (M4) can use `URLSessionWebSocketTask` over TCP |
| `Mirror` | per block: a libghostty terminal restored from the snapshot, fed seq-checked frames. Its write-back replies are discarded (the server already answered). `resync` → re-subscribe |
| `Render` | one `MTKView` per window drawing every pane. A CoreText glyph atlas (JetBrains Mono embedded, then the system monospace font); instanced quads: background, glyph, decoration, cursor and selection passes. Driven by libghostty `RenderState` dirty rows; shaders compiled at runtime from source (CLT has no `metal` compiler) |
| `Input` | `NSTextInputClient` for IME. Keys are encoded **client-side** by the libghostty key encoder with options taken from the mirror (kitty keyboard, cursor-key modes), then sent as `input` bytes. Mouse via the mouse encoder; paste via `paste.h` (bracketed) |
| `UI` | window = session, tab bar = windows, split panes with draggable dividers (`session.resize_pane`), sidebar with sessions + agents (blocked first, from `agent.list` + `agent_changed`), command palette (⇧⌘P), default keys (⌘T, ⌘D/⇧⌘D, ⌘W, ⌥⌘arrows, ⇧⌘↩ zoom) |
| `Notify` | UserNotifications when an agent turns blocked/done while the app is in the background (rate-limited, names the session) |

**Size authority:**
- On focus or visibility, the app computes cols/rows from the pane's pixel
  rect and the cell size, and sends `resize` with `take` for visible blocks.
- Blocks it doesn't show are left alone.
- On `size_changed` from another owner it letterboxes until the user
  interacts with the pane.

**Attach order** (documented in attach.go):
1. `events` first, noting `seq`.
2. `session.view`.
3. Subscribe the visible blocks.

**Server additions expected:**
- `session.view` already returns blocks' cols/rows.
- Possibly a `client.set_focus` hint, so the agent list knows which pane
  the user is looking at. Deferred unless needed.

**Testing:**
- `HiveKit`/`Mirror` unit tests (`swift test`) against a real `hive server
  run` in a temp dir: attach, snapshot restore, seq continuity, resync,
  resize conflict.
- Renderer tests compare the *cell model* (text/style per cell from
  RenderState) rather than pixels.
- One visual smoke loop: launch the app against a temp server, screenshot
  with `screencapture`, downscale to webp, inspect.

**Cut from M2:**
- Kitty graphics
- ligatures beyond CoreText's default shaping
- settings UI (config is M3's `init.lua`)
- remote hosts (M4)
- iOS

**Spikes (2026-10-17, /tmp/hive-spike):**
- SwiftPM + CLT links the static `libghostty-vt.a` through a C target
  (`-I…/include -DGHOSTTY_STATIC`, the archive passed to the linker), and
  `ghostty_terminal_new`/`vt_write` run.
- `NWProtocolWebSocket` over `NWEndpoint.unix` **does not work**: it
  connects and aborts without sending a handshake (POSIX 53).
- A hand-written upgrade over a raw unix `NWConnection` **works**: 101,
  then hive's `welcome`.

**Risks:**
1. Glyph rendering quality and performance vs Ghostty's own renderer.
2. IME and dead keys.
3. Notarization later needs Xcode (installing) and a Developer ID.

## M2: adopted from the plan review (2026-10-17)

These win over the M2 draft above where they conflict.

1. **Resize is sequenced in the output stream.** Binary frame kind 3
   `[3][idlen][id][seq][cols u16][rows u16][cell_w u16][cell_h u16]` is
   emitted inside `Block.Resize`, under the output lock, before the PTY
   resize. The mirror resizes exactly at that seq.
2. **One serial queue `hive.vt` owns libghostty.** It owns every mirror,
   RenderState and render-hold callback, and the `NWConnection` runs on that
   queue. After each write batch it publishes an immutable
   `PaneFrame` (cells, cursor, selection, colors) and `InputModes` (kitty
   flags, DECCKM, keypad, bracketed paste, mouse tracking/format) to the main
   thread. The renderer and key encoder never touch libghostty.
3. **Mirror write-back.** `write_pty` drops bytes during `vt_write` and
   captures them during `paste`; the capture is sent as input. The reply
   callbacks are NULL. Clipboard comes from the `clipboard_written` event.
4. **Server protocol additions:**
   - `events_subscribed{seq}` and `events_resync` on gap
   - errors carry `block_id` and the request `type`
   - real cell pixel sizes in `resize` / `size_changed`
   - `new_split` / `new_window` accept `cols` / `rows`
   - `input.data_base64`
5. **Size authority:** taken on key focus or interaction. The owner resizes
   freely; others letterbox but still apply resize frames.
6. **WebSocket:** one hand-rolled RFC 6455 client over `NWConnection` (unix
   now, TCP/TLS in M4). It handles bytes trailing the 101, frames spanning
   reads, 64-bit lengths, ping/close, and sends no extensions.
7. **Render holds (mode 2026):** honored via the render-hold callback, with
   a 1 s timeout.
8. **Glyphs:**
   - a grayscale A8 atlas plus a BGRA color-emoji atlas, 1 px padding,
     grid-snapped positions
   - fallback fonts via `CTFontCreateForString`, cached per cluster; real
     bold faces
   - procedural box/block/Powerline sprites (U+2500–259F, E0B0–E0B3)
   - wide cells and spacer tails; the atlas keyed by grapheme cluster
   - draw on demand (no continuous 120 Hz), triple-buffered instances,
     dirty rows only
9. **Included:** client-side scrollback and selection.
10. **Cut from M2:** command palette, draggable dividers (keyboard resize
    first).

**Build order:**
1. server additions
2. HiveKit WebSocket + protocol
3. Mirror on the vt queue
4. offscreen renderer with golden images
5. a window with one pane, input/IME, then splits, tabs, letterbox, sidebar

**Exit criterion:** a full day of Claude Code in Hive.app, covering
shift+enter, multi-line paste, scroll, copy, resize reflow, splits, and an
app restart that reattaches with scrollback, with zero `resync` in the log.

## M2 status (2026-10-17)

The app is built: title bar tabs, pane cards, palette, picker, agents
sidebar, selection and copy, reconnect.

**Fable 5.1 security/maintainability review.** All findings are fixed with
tests:
- **S1** origin/content-type guard on every route
- **S2** unsafe-paste confirmation
- **S3** validated agent reports
- **S4** shot.sh hardening
- **S5** owner-only socket via umask
- **S6** debug-only hooks
- **B1** hold-timeout redraw
- **B2** key buffer retry
- **B3** reconnect + resync
- **B4** live-socket server start
- **B5** move validation
- **B6** safe JSON ints
- **B7** callback teardown
- lesser items: 64-byte graphemes, gesture bounds, parser copies, atlas
  restart, bidi stripping, deduped titles

**Found while validating the fixes:**
- Shutdown raced a starting Serve (fixed).
- Reconnect used to restart a server the user had stopped. The app now
  starts one only at launch.

**Open:**
- split AppModel
- spacing/typography tokens
- AppModel tests
- the renderer still rebuilds clean rows (dirtyRows unused)
- the day-long dogfood exit criterion

## Building

`make vt` builds libghostty-vt from the pinned Ghostty commit (needs `zig@0.16`).
`make` builds `./bin/hive`.
