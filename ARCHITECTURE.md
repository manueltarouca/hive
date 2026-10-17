# hive: architecture

A terminal multiplexer for agent-heavy work, modelled on the public surface of
the reference's the reference app (see `~/code/research/ref/SURFACE.md`). It replaces tmux.

## Goal: a stepping stone to the reference app

the maintainer will replace hive with the reference app once he has beta access. Therefore:
- **the reference app's documented names win.** CLI verbs and flags, `session.*` method
  names and arguments, event names and their nesting, `block.method{block_id,
  method, args}` for terminal methods, layout tables and exit codes all match
  `SURFACE.md`. Only the prefixes differ: `hive` for `ref`, `HIVE_` for
  `REX_`, and the creator `hive.terminal` for `com.the reference.terminal`.
- **Additions are isolated.** Persistence, agent hooks and resume live in
  their own packages and methods (`agent.*`), so they can later be rebuilt as
  a `ref do` Lua script or a companion process against the reference app's API.
- **Planned:** `hive export --ref` emits a the reference app Lua script that recreates
  sessions and layouts and resumes agents.

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
3. **Lua.** `init.lua` keymap/actions/modes, `hive do` scripts, `ref`-compatible API names where sensible.
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
| `cmd/hive` | CLI: auto-starts the server; global `-s -w -b -S --json`; exit codes 0/1/2/3/4/130 as the reference app |

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

**Events (NDJSON):** the same names and nesting as the reference app (`session_event` →
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
- Server crash = all PTYs die (same as the reference app/tmux). Restore covers it, as long
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
  - the reference app drift: `process_changed{generation, ancestors}`,
    `program_status_changed.reason`, `program_status_removed`.
  - One `LayoutSpec` ↔ the reference app layout mapping, for `hive export --ref`.
  - The hook installer embeds an absolute binary path, so moving `bin/hive`
    breaks the hooks.
  - Tests for: shutdown with a WebSocket attached; a child ignoring SIGHUP
    with a grandchild holding the pty.

## Building

`make vt` builds libghostty-vt from the pinned Ghostty commit (needs `zig@0.16`).
`make` builds `./bin/hive`.
