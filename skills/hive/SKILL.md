---
name: hive
description: Drive the hive terminal multiplexer from an agent. Use it to run commands in terminals you can watch and wait on, start dev servers or test runs, read terminal output, type into a running program, follow events, check which coding agents are blocked, or call hive's API. Use it whenever the `hive` command is available or `HIVE_BLOCK` is set (you are inside hive), and whenever the user mentions hive sessions, panes, splits, blocks, or wants something running "in another pane", even if they don't say "hive".
---

# Driving hive

hive is a terminal multiplexer with an API. Everything the app can do, you
can do with the `hive` command or the API it wraps. Sessions keep running
after the app closes, and come back after a restart.

## Where you are

- `HIVE_BLOCK` set: you are inside a hive terminal. `HIVE_SESSION` is your
  session, and commands default to it, so you rarely need `-s`.
- `HIVE_BLOCK` unset: pass `-s SESSION` to say which session you mean.
  `hive ls` lists them.
- `hive` not found: hive is not installed. The `hive-setup` skill installs it.

## The model

server → **session** (a project) → **window** (a tab) → layout → **block**
(one terminal).

Every target is an ID that never changes, such as `block:01a1…`. Sessions,
windows and blocks can also have labels. `-s`, `-w` and `-b` accept, in this
order:
1. a full ID
2. a label, ignoring case
3. a position from 1
4. the last 4+ characters of an ID

A target that matches more than one thing changes nothing and exits 3.
Labels are for people and can change. In scripts, keep the ID that `--json`
prints.

## Recipes

**Run something and get its result.** `--wait` keeps the terminal after the
program exits and returns its exit status, so you can read the output:

```sh
hive run --label tests --focus=false --wait -- go test ./...
echo "exit $?"
hive -b tests capture --tail 50          # the last 50 lines that show something
hive -b tests close
```

Without `--wait` or `--keep-open`, a block closes as soon as its program
exits. Its output goes with it, but `hive wait` still reports the status.

**Start something long-running** (dev server, watcher):

```sh
hive run --label web --keep-open --focus=false -- pnpm dev
hive -b web capture --tail 20            # check it came up
```

**Put it beside the user's pane** instead of in a new tab:

```sh
hive split --split below --ratio 30 --label logs --focus=false -- tail -f app.log
```

`--split` takes `right`, `left`, `above` or `below`. Without `-- CMD`, you
get a shell.

**Type into a program** (a REPL, a prompt, a shell):

```sh
hive -b repl send 'print(1 + 1)'      # types text; does not press enter
hive -b repl send-key enter           # keys: enter, ctrl+c, up, shift+tab, esc...
hive -b repl capture --tail 5
```

`send -` reads the text from stdin. Programs take time to react, so sleep
briefly before you capture.

**Wait for things:**

```sh
hive wait --for 10m tests            # a block's program; exits with its status
hive wait session:…                  # until a window or session closes
```

**Watch events**: blocks created and closed, titles, exits, agent changes:

```sh
hive events -s SESSION --json        # NDJSON, one event per line
```

**Check on coding agents** in every session, blocked ones first:

```sh
hive agents
hive agents --json
```

**Another machine.** `-S` runs any command on a saved host, or on a server
by URL. The server must let you in; `whoami` says who it thinks you are:

```sh
hive hosts                           # the saved hosts
hive -S box ls
hive -S box whoami
```

`HIVE_SERVER` is always this machine's socket; use `-S` for others.

**Pin a tab** (pinned tabs come first in every app):

```sh
hive api call session.pin_window '{"session_id":"session:…","window_id":"window:…","pinned":true}'
```

**Look around:**

```sh
hive ls                              # sessions
hive -s SESSION session inspect      # windows, layouts, every block (JSON)
hive -b TARGET block inspect         # one block: command, cwd, process, agent
```

## The API

The CLI wraps a JSON API, and the server describes it:

```sh
hive api list                        # every method
hive api describe session.new_split  # arguments and result, as JSON Schema
hive api call session.list
hive api call hive.terminal.format '{"block_id":"block:…","args":{"trim":true}}'
```

Unknown arguments are errors, so `describe` before you guess. For a method
the CLI doesn't cover, use `hive api call`. The socket also speaks HTTP, and
accepts only JSON:

```sh
curl --unix-socket "$HIVE_SERVER" http://hive/v1/call \
  -H 'content-type: application/json' -d '{"method":"session.list"}'
```

## Exit status

| Code | Means |
| --- | --- |
| 0 | worked |
| 1 | failed |
| 2 | bad command line |
| 3 | target not found or ambiguous |
| 4 | server unreachable |
| 5 | the server refused you |
| 130 | interrupted |

`wait` and `run --wait` pass through the program's own status.

## Working alongside a person

The user is often looking at the same session, so act like a polite
colleague at a shared desk:

- **Use `--focus=false`** for anything you start, unless the user asked to
  see it. Taking focus moves their view and their keyboard.
- **Only type into blocks you created**, unless asked. Another block may be
  the user mid-sentence, or another agent mid-task. `send` there corrupts
  their work.
- **Clean up what you opened** with `hive -b ID close` when you no longer
  need it. Leave other blocks alone.
- **Ask before anything wide:** `hive kill`, `hive close --session` or
  `--window`, and `hive server stop`. These end programs the user cares
  about. After `server stop`, sessions come back, but their programs don't.
- **`hive close` with no target closes your own block** when you are inside
  hive, and that ends you. Always pass `-b`.
