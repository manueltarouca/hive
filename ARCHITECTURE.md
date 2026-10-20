# How hive is built

hive is one server that owns every terminal, and clients that show them: the
`hive` CLI, the Mac app and the Android app. Everything goes through the
server's API. This page explains the parts and the decisions behind them.
The [docs](https://manueltarouca.github.io/hive/docs/installation) explain
how to use hive.

## Principles

- **Stable names.** Commands, flags, method names and arguments, event names
  and exit codes are a contract. Scripts and agents rely on them, so they
  break only in a new major version (during 0.x, a minor one), and
  `CHANGELOG.md` lists it.
- **One API.** The apps, the CLI and scripts all use the server's API. Nothing
  is only possible in an app.
- **Agents are part of the design.** The server knows which panes run an
  agent, what state each is in, and how to resume it.
- **Local and remote are the same.** A server on another machine speaks the
  same API over a different listener. Only who gets in differs.

## The parts

```
cmd/hive          the CLI, and `hive server run`
internal/         the server and what it's built from (below)
apps/macos        Hive.app: Swift, AppKit, Metal
apps/android      hive for Android: Kotlin, Compose
site/             the landing page and the docs site (SvelteKit)
docs/             the docs, in Markdown; the site renders them
skills/           agent skills, also a Claude Code plugin
bench/            benchmarks and their recorded baselines
```

| Package | Does |
| --- | --- |
| `internal/vt` | One terminal: libghostty-vt (Ghostty's terminal engine, through cgo) behind a lock. Feeding output, resizing, formatting text, snapshots, key encoding |
| `internal/block` | A pane: a PTY and a `vt` terminal. Reads the program's output, feeds it to the terminal and to subscribers, watches the program in front, and reports when it exits |
| `internal/session` | Sessions, tabs and their layout trees: splits, moves, zoom, focus, labels, pins. Resolves targets |
| `internal/api` | The method registry: each method's name, description, and arguments and result as JSON Schema, with strict decoding |
| `internal/server` | Owns all state. The HTTP routes, the event bus, the attach stream, clients, size ownership, restore |
| `internal/persist` | Saves `state.json` and each terminal's snapshot, safely |
| `internal/agents` | What each agent harness reports, how to resume it, and the Claude Code hook installer |
| `internal/remote` | Listeners beyond the Unix socket, who each caller is, and who gets in. Built-in Tailscale |
| `internal/config` | The settings file: parsing, defaults, problems by line, editing in place |
| `internal/events` | The event bus: numbered events, a kept history, per-subscriber buffers |
| `internal/client` | The Go client the CLI uses, local or remote |
| `internal/hosts` | Saved remote hosts (`hosts.json`) |
| `internal/id`, `names`, `paths` | IDs, generated session names, where files go |

## The model

```
server → session → window (tab) → layout tree (split | block) → block (pane: PTY + terminal)
```

IDs look like `block:<uuidv7>`. They never change and are never reused.
Labels are optional. A target is resolved by full ID, then label, then
position (pinned tabs first), then the end of an ID. Every change to a layout
bumps the session's `revision`.

## The wire

All of it is HTTP, on the Unix socket and on every other listener:

| Route | Carries |
| --- | --- |
| `POST /v1/call` | One method call, JSON in and out |
| `GET /v1/events` | Numbered events, one JSON object per line, with a heartbeat every 15 seconds |
| `GET /v1/attach` | A WebSocket per client: calls, events and terminal streams on one connection |

Requests from browsers are refused (any `Origin` or `Sec-Fetch-*` header,
or a `Host` that isn't the server's), so a web page can't drive your
terminals.

### Mirroring a terminal

A client subscribes to a pane and gets a snapshot of the server's terminal
and a `seq`: how many bytes the program has printed. Then it gets every byte
after that, tagged with its `seq`, so it can check nothing is missing.
Resizes come in the same stream, at the exact byte where they happened,
because text reflows differently depending on where a resize lands. The
client feeds all of it into its own libghostty-vt terminal and draws that.
Its terminal must never answer the program: the server's already did.

A client that falls behind gets `resync` instead of missing bytes, and
subscribes again. Nothing is dropped silently.

### Size

A pane has one size. The client you use holds it: typing, clicking, or
bringing the window to the front takes it, and the server resizes the PTY.
Other clients show the pane at that size, zoomed or with empty space.
Showing a pane without using it never takes it. A client can give the size
back (`release`), and a client that leaves gives up what it held.

## Restore

The server saves `state.json` a second after a change, every 30 seconds,
and when it stops, by writing a temporary file and renaming it, with the
data flushed first. A power cut can lose the latest save, never tear the
file. An agent's identity is saved at once, because resuming depends on it.
Each terminal's snapshot (screen, scrollback, modes) is saved beside it.

At start, the server rebuilds every session with the same IDs, restores each
terminal from its snapshot, writes a `── restored ──` line, and starts:

- for an agent that was running, its resume command, inside your login shell;
- otherwise your shell, in the pane's last folder (from the agent's hooks,
  then OSC 7, then the program's working directory).

Other programs aren't run again. One server holds a state folder at a time
(`server.lock`).

## Agents

Claude Code's hooks call `hive agent hook claude`, which reports to the
server: the session ID, the folder, and the state (working on a prompt,
blocked on a notification, done on stop). Agents can also report their own
state through OSC 7501, the Program Status protocol. The server keeps the
latest per pane, sends `agent_changed`, and ends an agent when its shell
comes back to the front. On restore it runs the harness's resume command
with the latest session ID.

## Remote hosts

A listener is a Unix socket, a tailnet address, a socket behind Tailscale
Serve, built-in Tailscale (tsnet), or plain TCP for tests. Each request is
identified before it reaches a method: a local user by the socket's peer, a
tailnet caller by Tailscale. A server lets in its owner, and others only
when told to. A tagged device has no owner. Built-in Tailscale keeps its
state in the state folder, and `--tailscale-control-url` points it at
another coordination server.

## The Mac app

| Module | Does |
| --- | --- |
| `CGhosttyVT` | libghostty-vt, linked statically |
| `HiveKit` | The protocol client: its own WebSocket over a Unix socket or TCP, calls, events, reconnecting |
| `HiveCore` | Logic with tests: mirroring, key and mouse encoding, link detection, tab order, size claims, what the menu bar shows |
| `HiveRender` | Metal drawing: a glyph atlas, and instanced quads for backgrounds, text, decorations, the cursor and selection |
| `HiveApp` | The windows, the tabs and sidebar, the palette, settings, notifications, the menu bar |

Keys are encoded in the app by libghostty's key encoder, with the modes the
mirror is in (kitty keyboard, cursor keys), and sent as bytes. A pane only
draws while its window can be seen.

## The Android app

The same protocol, from Kotlin: its own WebSocket client, a mirror per pane
on libghostty-vt through JNI (`apps/android/app/src/main/cpp`), and a Canvas
renderer with the Mac app's cell metrics and box drawing. Program icons are
generated at build time from the Mac app's sources.

## Building

`make vt` builds libghostty-vt from a pinned Ghostty commit (Zig 0.16).
`make build` builds `bin/hive`. `apps/macos/script/bundle.sh` builds
`Hive.app` with its own copy of `hive`. `make vt-android` builds the Android
library; Gradle builds the app. The release workflow builds and signs
everything from a tag; see `.github/RELEASING.md`.
