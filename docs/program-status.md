---
title: Program status
section: Reference
order: 9
description: How programs tell hive what they are doing, with OSC 7501.
---

# Program status

A program can tell the terminal what it is doing: working, waiting for you,
done, or failed. hive reads the Program Status Protocol, OSC 7501, which
the reference published for the reference app. Programs that speak it work in both.

```sh
printf '\e]7501;state=working:app=deploy:msg=%s\e\\' "$(printf 'Deploying' | base64)"
```

| State | Means |
| --- | --- |
| `idle` | Waiting for its next instruction |
| `working` | Running |
| `blocked` | Waiting for you. `kind` says why: `permission`, `question` or `auth` |
| `done` | Finished, and you haven't looked yet |
| `error` | Failed |
| `clear` | Remove the record |

`working` and `blocked` end when the program exits or a new shell prompt
starts. `done` and `error` stay until you look.

hive keeps the records per terminal. `hive.terminal.program_status` returns
them, and `program_status_changed` reports each change.

The full specification is in
[the reference app's documentation](https://www.the reference.com/ref/docs/build/program-status).
