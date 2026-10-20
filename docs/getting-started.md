---
title: Getting started
section: Start
order: 2
description: Your first ten minutes with hive: a session, a restart, and an agent you can see.
---

# Getting started

This takes about ten minutes. You need `hive` and Hive.app from
[Installation](./installation).

## A session

Open Hive.app. It starts the server and opens a session in your home
folder. Split it with ⌘D, open a tab with ⌘T, and run something that takes a
while, such as a build or `top`.

The same from a terminal:

```sh
hive new work --cwd ~/src/app
hive -s work split --split below -- top
hive ls
```

## Close it, and come back

Quit Hive.app with ⌘Q, then open it again. Your session is where you left
it, still running: the server kept it while no app showed it.

Now restart the server, as a reboot would:

```sh
hive server stop
hive ls
```

`hive ls` starts the server again. Each pane comes back with its screen,
its scrollback and a `── restored ──` line, then a shell in the same folder.
Programs such as `top` don't run again; agents do. See
[Restarts](./restarts).

## An agent you can see

Connect Claude Code once:

```sh
hive agent install claude
```

Start `claude` in a hive pane and give it a task. The tab shows what it is
doing. When it needs you, the tab, the sidebar (⇧⌘S) and the menu bar say
so, and a notification shows when hive isn't in front. `hive agents` lists
every agent, the waiting ones first. See [Agents](./agents).

## Next

- [The Mac app](./app): shortcuts, the sidebar, the menu bar.
- [Remote hosts](./remote): your sessions on another machine.
- [hive on your phone](./android): the same sessions on Android.
- [The CLI](./cli) and [the server API](./api): drive hive from scripts and
  agents.
