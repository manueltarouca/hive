---
title: Questions
section: Help
order: 31
description: Does hive replace tmux? Why Tailscale? Which agents? Short answers.
---

# Questions

## Does hive replace tmux?

For most work, yes: sessions that keep running, splits, and a CLI to drive
them. hive's sessions also come back after a reboot, scroll like any Mac
app, and know about your agents. hive has no prefix key and no status line.
`hive-from-tmux`, one of hive's [agent skills](./agent-setup), moves tmux
sessions over one window at a time.

## Does it work over ssh?

On another machine you run hive's server there, and use it from your Mac
over Tailscale: see [Remote hosts](./remote). Inside an ssh session, the
`hive` CLI works as on any Linux machine.

## Why Tailscale?

A server runs programs as you, so it has to know who connects. Tailscale
tells it, without passwords or keys for you to manage. hive doesn't have
accounts of its own.

## Is the traffic to a remote server encrypted?

Yes. A remote server listens only on your tailnet, so everything between
it and your Mac or phone goes through WireGuard, encrypted from one device
to the other. Tailscale's relays, when they are used, carry only encrypted
packets. By default the server lets in only you.

Two things stay true. The server runs your terminals, so whoever controls
that machine can read them, as with any remote shell. And a plain TCP
listener has no encryption and no identity: the server starts one only with
`--allow-anonymous`, for tests on one machine. See [Remote hosts](./remote).

## Which agents does it know?

Claude Code reports its state through hooks, and Grok runs the same hooks.
Codex, OpenCode and Gemini resume after a restart. See [Agents](./agents).

## Which systems does it run on?

The server and the CLI: macOS on Apple Silicon, and Linux on x86_64. The app:
macOS 14 or later. hive for Android: Android 10 or later.

## Why is the Mac app unsigned?

Signing needs an Apple Developer ID, which hive doesn't have yet. Each
release has checksums and a signed record of where it was built; see
[Installation](./installation).

## Does it send anything anywhere?

No. There is no telemetry and no account. See [Privacy](./privacy).
