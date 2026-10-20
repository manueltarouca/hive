---
title: Restarts
section: Use hive
order: 13
description: What hive keeps on disk, and what comes back after a restart or a reboot.
---

# Restarts

Quit the app and your programs keep running in the server. Stop the server,
or restart the machine, and your sessions come back the next time it starts.

## What comes back

- Every session, tab and layout, with the same IDs and labels.
- Each terminal's screen and scrollback, followed by a `── restored ──` line.
- A shell in each terminal's last working directory.
- Agents, resumed with their latest session, in your shell: quit one and the
  pane is at a prompt in the same place. With nu, csh or tcsh, the agent runs
  alone. See [Agents](./agents).

Other programs are not run again. A pane that ran `htop` comes back with a
shell, and the restored line says what it was running.

## Where it lives

| Path | Holds |
| --- | --- |
| `~/.local/state/hive/state.json` | Sessions, layouts, and what each pane was doing |
| `~/.local/state/hive/blocks/ID.snap` | A snapshot of each terminal: screen, scrollback, modes |
| `~/.local/state/hive/server.log` | What the server did, and why something failed |
| `~/.local/state/hive/tailscale/` | With `--tailscale`: the server's Tailscale login and device key. Keep it private |
| `~/.local/state/hive/server.lock` | Held by the running server: a second server on the same directory won't start |
| `~/.local/state/hive/server-id` | This state folder's ID, for clients telling servers apart |

`HIVE_STATE_DIR` or `XDG_STATE_HOME` moves it.

hive writes a second after changes, every 30 seconds, and when it stops. An
agent's identity is written at once, because restore depends on it. Every
write goes to a temporary file that replaces the old one, so a crash leaves
the previous state whole.

## The working directory

A restored shell starts in the first of these that exists:

1. The agent's directory, from its hooks.
2. The directory the shell last reported (OSC 7).
3. The foreground program's directory.
4. The directory the block started in.

Shells report their directory with OSC 7. zsh and fish need shell integration
for it. Without it, hive falls back to the foreground program's directory.

## Closing for good

`hive kill <session>` and `hive close` remove a session or block and its
snapshot. Those don't come back.
