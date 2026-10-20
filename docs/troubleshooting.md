---
title: Troubleshooting
section: Help
order: 30
description: What to do when hive doesn't start, an agent doesn't show, or a host doesn't connect.
---

# Troubleshooting

The server writes what it does to `~/.local/state/hive/server.log`. Most
answers start there.

## macOS says Hive.app can't be opened

The app isn't signed with an Apple Developer ID yet, so macOS asks you to
approve it once. [Installation](./installation#the-app) shows the steps for
your macOS version.

## "another hive server uses this state folder"

Only one server can use a state folder. Another one runs already: use it, or
stop it with `hive server stop`. A test or an experiment needs its own
folder: set `HIVE_SERVER`, `HIVE_STATE_DIR` and `HIVE_CONFIG` together.

## The build says the Zig version is wrong

Ghostty's build needs Zig 0.16 exactly. On macOS: `brew install zig@0.16`,
and make sure `zig version` prints `0.16`.

## An agent doesn't show its state

1. Check that hive's hooks are in Claude Code: `hive agent install claude`
   adds them, and does nothing if they're there.
2. Start the agent again, in a hive pane. Agents started outside hive, or
   before the hooks, don't report.
3. `hive agents` lists what the server knows. If the agent is there, the app
   shows it too.

Other agents need hooks of their own; see [Agents](./agents).

## Sessions didn't come back

- Look for errors in `~/.local/state/hive/server.log`, near "restored".
- With `resume-agents = false` in the settings file, agents come back as
  shells.
- A server started with `--fresh` ignores what was saved.

## A remote host doesn't connect

The app and the phone say why:

| They say | Try |
| --- | --- |
| No hive server running | Start the server on that machine, with a listener: see [Remote hosts](./remote#listening-on-your-tailnet). |
| Not answering | The machine is asleep, off, or off the tailnet. Check `tailscale status`. |
| Refused this Mac (or phone) | The server lets in only its owner. See [Who can connect](./remote#who-can-connect). `hive -S NAME whoami` exits with status 5 when refused. |

## The phone can't reach my Mac

Your Mac's server needs a tailnet listener, and one given at the command line
is gone after a reboot. Put it in the settings file:

```sh
hive config set listen tailnet://:4483
hive server stop && hive server start
```

## Still stuck

Open an [issue](https://github.com/manueltarouca/hive/issues/new/choose)
with `hive version`, what you did, and the end of the server log, without
anything private in it.
