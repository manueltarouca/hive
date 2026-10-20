---
title: Sessions, tabs and panes
section: Start
order: 2
description: What a session, a tab and a pane are, and how commands choose which one to act on.
---

# Sessions, tabs and panes

You work in **sessions**: one per project, usually. A session has **tabs**,
and a tab is split into **panes**, each a terminal. A session keeps running
in the server when no app shows it.

The CLI and the API use other names for two of these. A tab is a
**window** (`-w`), and a pane is a **block** (`-b`): a block is one program
in a window, which today is always a terminal. An app or a phone connected
to the server is a **client**.

Every session, window and block has an ID that never changes and is never
used again, such as `block:01a149bd-5371-7000-8bcc-af6cb8b70503`. You can
also give each a label. A session without a label gets a name such as
`drifting-cedar`.

## Choosing what a command acts on

A command acts where you run it. In a hive terminal, that's the terminal's
own pane and session: hive sets these variables in every terminal it starts.

| Variable | Holds |
| --- | --- |
| `HIVE_SESSION` | The session's ID |
| `HIVE_BLOCK` | The pane's ID |
| `HIVE_SERVER` | The server's socket |

To act somewhere else, give a target:

| Option | Chooses |
| --- | --- |
| `-s`, `--session` | A session |
| `-w`, `--window` | A tab in the session |
| `-b`, `--block` | A pane |
| `-S`, `--server` | Another server: a saved host's label, a host name, `https://HOST`, or a socket path. See [Remote hosts](./remote) |

```sh
hive -s api split --split below -- htop
hive -b logs capture --trim
hive -s 2 session inspect
```

hive matches a target in this order, and stops at the first kind that
matches:

1. A full ID.
2. A label, ignoring case.
3. A position, from 1. Tabs count as the apps show them, pinned tabs
   first, so `-w 1` is the tab ⌘1 picks.
4. The end of an ID, at least four characters, such as `5d8b70`.

When a target matches more than one thing, the command changes nothing,
lists what matched, and exits with status 3.

Labels are easy to type, and you can change them. In a script, keep the ID
that a command prints with `--json`.
