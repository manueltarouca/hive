---
title: Sessions, windows, blocks
section: Start
order: 2
description: How hive organizes work, and how commands find what to act on.
---

# Sessions, windows, blocks

hive organizes work into four kinds of things.

- A **session** is a group of related work, usually one project. It keeps
  running whether or not anything is looking at it.
- A **window** is a tab in a session. It holds a layout of blocks.
- A **block** is one program in a window. Today that is always a terminal.
- A **client** is a program connected to the server, such as the app.

Each has an ID that never changes and is never reused, such as
`block:01a149bd-5371-7000-8bcc-af6cb8b70503`. Sessions, windows and blocks
can also have a label you choose. A session without one gets a name like
`drifting-cedar`.

## Targets

With no options, a command acts where you run it. Inside a hive terminal,
that is the terminal's own block and session. hive sets three variables in
every terminal:

| Variable | Holds |
| --- | --- |
| `HIVE_SESSION` | The terminal's session |
| `HIVE_BLOCK` | The terminal's block |
| `HIVE_SERVER` | The server's socket |

To act elsewhere, name a target:

| Option | Chooses |
| --- | --- |
| `-s`, `--session` | A session |
| `-w`, `--window` | A window in the session |
| `-b`, `--block` | A block |
| `-S`, `--server` | A server, by socket path |

A target is matched in this order, and the first kind of match wins:

1. A full ID.
2. A label, ignoring case.
3. A position, counting from one. Windows count in tab order.
4. The end of an ID, at least four characters long, such as `5d8b70`.

If a target matches more than one thing, the command changes nothing, lists
the matches, and exits with status 3.

```sh
hive -s api split --split below -- htop
hive -b logs capture --trim
hive -s 2 session inspect
```

Labels are easy to type but can change. In scripts, keep the ID a command
prints with `--json`.
