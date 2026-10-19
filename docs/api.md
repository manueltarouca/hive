---
title: Server API
section: Reference
order: 7
description: The methods the server offers, how to call them, and the events it sends.
---

# Server API

Everything hive does goes through the server's API: the app, the CLI and
your scripts all use it. The server describes its own API, so what you read
from it always matches the server you are talking to.

```sh
hive api list
hive api describe session.new_split
hive api call server.version
```

Each method has a name, a description, and its arguments and result as JSON
Schema. A misspelled argument is an error, not ignored.

## Calling it directly

The server listens on a Unix socket. `hive server status` prints its path.

```sh
curl --unix-socket "$HIVE_SERVER" http://hive/v1/call \
  -H 'content-type: application/json' -d '{"method":"session.list"}'
```

The server accepts only JSON, and refuses requests from browsers (any
`Origin` or `Sec-Fetch-*` header), so a web page can't drive your terminals.

A server can also listen on your tailnet.
The API is the same on every listener; only who gets in differs. Every
request is identified before it reaches a method, and `whoami` says how:

| Principal | Who |
| --- | --- |
| `peer:UID` | A local account, on the Unix socket |
| `tailscale:alice@github` | A person, by their Tailscale login |
| `tailscale:tag:ci` | A tagged device, which never counts as its owner |
| `anonymous` | A caller on a plain TCP listener |

A call answers with `{"result": …}`, or with `{"error": {"status", "code",
"message"}}` and an HTTP status:

| Code | Status | Means |
| --- | --- | --- |
| `bad_request` | 400 | The arguments were wrong |
| `not_found` | 404 | No such session, window or block |
| `method_not_found` | 404 | No such method |
| `ambiguous` | 409 | A target matched more than one thing |
| `conflict` | 409 | Another client holds the size, or a label is taken |
| `forbidden` | 403 | This caller may not use this server |

## Methods

Method names are stable. They break only in a new major version, and the
[changelog](https://github.com/manueltarouca/hive/blob/main/CHANGELOG.md)
lists every change.

| Group | Methods |
| --- | --- |
| Sessions | `session.list`, `session.create`, `session.view`, `session.list_blocks`, `session.set_label`, `session.close` |
| Windows | `session.new_window`, `session.close_window`, `session.focus_window`, `session.focus_next_window`, `session.focus_previous_window`, `session.move_window`, `session.set_window_label`, `session.pin_window` |
| Blocks | `session.new_split`, `session.move_block`, `session.describe_block`, `session.focus_block`, `session.focus_direction`, `session.focus_next_pane`, `session.focus_previous_pane`, `session.zoom_block`, `session.resize_pane`, `session.set_block_label`, `block.close`, `block.method` |
| Terminals, as `hive.terminal.NAME` | `format`, `write`, `send_key`, `resize`, `size`, `title`, `pwd`, `process`, `program_status`, `list_dir`, `wait` |
| Agents | `agent.report`, `agent.list` |
| Settings | `config.get`, `config.set`, `config.reload` (the server's own settings file; see [Settings](./config)) |
| Server | `server.version`, `server.flush`, `server.shutdown`, `api.list`, `api.describe`, `target.resolve`, `client.list`, `whoami` |

Terminal methods take the block and their own arguments separately:

```sh
hive api call hive.terminal.format '{"block_id":"block:…","args":{"trim":true}}'
```

## Events

`GET /v1/events` streams events, one JSON object per line.

| Query | Streams |
| --- | --- |
| *(none)* | Sessions created and destroyed |
| `?session=ID` | Everything in one session, terminal events included |
| `?all=1` | Everything, clients included |
| `?since=N` | First, the events after number N that are still kept |

Every event has a number. A client that reconnects passes the last number it
saw and misses nothing, unless the gap is older than the server keeps. Then it
gets `resync_required` and should read the state again.

A terminal event arrives inside a session event:

```json
{"seq":42,"type":"session_event","payload":{"session_id":"session:…",
 "event":{"type":"block_event","payload":{"block_id":"block:…",
  "creator_name":"hive.terminal","name":"child_exited","payload":{"exit_code":2}}}}}
```

Session events include `window_created`, `window_closed`, `window_pinned`, `block_created`,
`block_closed`, `session_view_changed` and `active_window_changed`. Terminal
events include `child_exited`, `title_changed`, `pwd_changed`, `bell`,
`process_changed`, `size_changed`, `desktop_notification`,
`program_status_changed` and `agent_changed`.

`agent_changed` carries `harness`, `session_id`, `state`, `message` and
`event`: what changed it. That is a hook's event name (`SessionStart`,
`Stop`…), or one of the server's own:

| `event` | Why |
| --- | --- |
| `program_status` | The agent reported its own state (OSC 7501), such as idle after an interrupt, which runs no hook |
| `shell_returned` | The pane's interactive shell came back in front: the agent exited |
| `agent_returned` | The same agent is in front again after `shell_returned` |

`agent.report` takes an optional `agent_pid`, the process the hook runs
for. A report from a different agent than the one holding the pane (one
another agent runs as a tool) is ignored. `hive agent hook` sends it.
