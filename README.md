# hive

A terminal multiplexer for agent-heavy work. It tracks the public surface of
[the reference's the reference app](https://www.the reference.com/ref/docs), so it can be
swapped for the reference app later. On top of the reference app it adds:

- sessions that survive a server restart or a machine reboot
- coding agents (Claude Code first) that are resumed mid-conversation
- agent status driven by Claude Code's hooks

See [ARCHITECTURE.md](ARCHITECTURE.md) for the design and milestones.

## Build

You need Go 1.26 and Zig **0.16** (`brew install zig@0.16`; Ghostty refuses
other minor versions).

```sh
make vt      # once: builds libghostty-vt from the pinned Ghostty commit
make build   # ./bin/hive
make test
```

`make` exports `PKG_CONFIG_PATH`. For plain `go` commands, source `.envrc`.

## Use

```sh
hive new work --cwd ~/src/app          # a session (starts the server if needed)
hive -s work split --split below -- htop
hive -s work run --label tests -- go test ./...
hive -b tests capture --trim
hive -b tests wait && echo passed
hive -s work send 'make build'; hive -s work send-key enter
hive events -s work --json | jq .
hive api list; hive api describe session.new_split
```

Targets are an ID, a label, a 1-based position, or an ID suffix. Inside a
hive terminal, `HIVE_SESSION` and `HIVE_BLOCK` are the defaults. Exit
statuses match the reference app: 2 usage, 3 target not found or ambiguous, 4 server
unreachable, and `wait`/`run --wait` pass through the program's status.

### Agents

```sh
hive agent install claude     # SessionStart/SessionEnd/Notification/Stop/UserPromptSubmit hooks
hive agents                   # every agent, blocked ones first
```

Claude Code sessions started inside hive report their session ID. After a
restart they come back with `claude --resume <id>`. Codex, OpenCode, Gemini
and Grok have built-in resume commands. Override any harness with
`HIVE_RESUME_<HARNESS>='["cmd","--flag","{id}"]'`.

### Restarts

`hive server stop`, a crash or a reboot all end the programs, but not the
sessions. The next `hive` command starts the server, which rebuilds every
session:
- the same layout, labels and IDs
- each block's screen and scrollback, followed by a `── restored ──` marker
- a shell in the block's last directory, or the agent's resume command

Other programs are not re-run. State lives in `~/.local/state/hive`.

## Protocol

- `POST /v1/call` with `{"method","params"}` on the Unix socket.
- `GET /v1/events` streams NDJSON (`?session=`, `?all=1`, `?since=N`).
- `GET /v1/attach` is a WebSocket for clients. It carries snapshots plus
  seq-numbered output frames, input, resize with size authority, RPC and
  events. The frame format is documented in `internal/server/attach.go`.
