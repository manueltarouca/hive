---
title: The CLI
section: Reference
order: 6
description: Every hive command, its options, and its exit codes.
---

# The CLI

`hive` is the server and the command-line interface in one program. Inside a
hive terminal it is already on `PATH`.

Commands, flags and exit codes are stable. They break only in a new major
version, and the
[changelog](https://github.com/manueltarouca/hive/blob/main/CHANGELOG.md)
lists every change.

## Sessions

| Command | Does |
| --- | --- |
| `hive ls` | List sessions |
| `hive new [LABEL] [--cwd DIR] [-- CMD…]` | Create a session |
| `hive session inspect` | Show a session's windows, layout and blocks |
| `hive kill SESSION` | Close a session and everything in it |

## Blocks

| Command | Does |
| --- | --- |
| `hive run [opts] [-- CMD…]` | Open a terminal in a new window |
| `hive split [opts] [-- CMD…]` | Open a terminal beside the current one |
| `hive move [--split SIDE] TARGET` | Move the block beside another |
| `hive block inspect` | Show a block, its methods, and its program |
| `hive label TEXT` | Label the block. `--session` or `--window` label those. |
| `hive zoom [--on\|--off]` | Zoom the block |
| `hive focus [left\|right\|up\|down]` | Focus the block, or its neighbor |
| `hive close [--window\|--session]` | Close the block, its window, or its session |

`run` and `split` take:

| Option | Does |
| --- | --- |
| `-- CMD…` | Runs a command instead of a shell. No shell parses it. |
| `--cwd DIR` | Sets the working directory. Without it, the CLI's own; a block created through the API without one starts in the server account's home. |
| `--label TEXT` | Labels the new block. |
| `--split right\|left\|above\|below` | Where the new block goes. |
| `--ratio PCT` | The new block's share of the split. |
| `--focus=false` | Leaves focus where it was. |
| `--keep-open` | Keeps the terminal after its command exits. |
| `--wait` | Waits for the command and exits with its status. |

## Input and output

| Command | Does |
| --- | --- |
| `hive send TEXT` | Types text. Reads stdin with `-` or no text. |
| `hive send-key KEY…` | Presses keys: `enter`, `ctrl+c`, `up`, `shift+tab`… |
| `hive capture [--trim] [--unwrap] [--format text\|vt]` | Prints the screen and scrollback |
| `hive wait [--for DUR] [TARGET]` | Waits for a program to exit, or a window or session to close |
| `hive events [--all] [--since N]` | Streams events |

## Agents

| Command | Does |
| --- | --- |
| `hive agents` | List agents, blocked first |
| `hive agent install claude` | Add hive's hooks to Claude Code |
| `hive agent uninstall claude` | Remove them |
| `hive agent hook HARNESS` | Run by an agent's hooks |

## API and server

| Command | Does |
| --- | --- |
| `hive api list [-q]` | Every server method |
| `hive api describe METHOD` | A method's arguments and result, as JSON Schema |
| `hive api call METHOD [JSON\|@FILE\|-]` | Call a method |
| `hive server run [--fresh]` | Run the server in the foreground |
| `hive server start` / `stop` / `status` | Manage a background server |
| `hive version` | Print the version. It doesn't start a server. |
| `hive help` | List the commands |

Every command takes `--json` for output a script can parse.

## Remote servers

Any command runs against another machine's server with `-S`. The server
there must listen on your tailnet (`hive server start --tailscale`, or
`--extra-listen tailnet://:4483`), and let you in: by default only the
Tailscale user who owns that machine.

```sh
hive -S https://box.example.ts.net ls
hive hosts add box https://box.example.ts.net
hive -S box new build -- make
hive -S box whoami
```

| Command | Does |
| --- | --- |
| `hive hosts [ls]` | The servers you've named |
| `hive hosts add LABEL URL` | Name one. The app shows the same list. |
| `hive hosts rm LABEL` | Forget it |
| `hive whoami` | Who the server thinks you are, and how you reached it |
| `hive client ls` | The apps connected to a server, and as whom |

`-S` takes:

| Value | Means |
| --- | --- |
| `/PATH`, `./PATH`, `~/PATH` | A Unix socket on this machine |
| `https://HOST`, `http://HOST:PORT` | A remote server |
| `LABEL` | A host saved with `hive hosts add` |
| `HOST` | A remote server, tried at `https://HOST`, then `https://HOST:4483`, then `http://HOST:4483` |

`HIVE_SERVER` is always a socket path: hive sets it in every terminal.

Hosts are kept in `hosts.json`, next to the settings file. A bare host is
saved as the URL it answered at. A command never starts a server on another
machine. A block created there without `--cwd` starts in that account's
home; `--cwd` is an absolute or `~` path on that machine. `config` edits this
machine's settings; for a remote server's, use `hive -S HOST api call
config.set`.

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | It worked |
| 1 | It failed |
| 2 | The command line was wrong |
| 3 | A target was not found or matched more than one thing |
| 4 | The server could not be reached |
| 5 | The server refused you |
| 130 | It was interrupted |

`wait` and `run --wait` exit with the program's own status.
