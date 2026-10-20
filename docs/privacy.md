---
title: Privacy
section: Reference
order: 10
description: What hive collects (nothing), and what it connects to.
---

# Privacy

hive collects no telemetry. It has no analytics, no crash reporting and no
account.

## What connects to what

| Part | Connects to |
| --- | --- |
| The server | Nothing, by default. It listens only on a Unix socket that only you can read. |
| The server, with a listener | It also accepts connections on your tailnet, or on the address you give it. See [Remote hosts](./remote). |
| The server, with `--tailscale` | Tailscale's coordination and relay servers, to join your tailnet as its own device. `--tailscale-control-url` points it at your own server, such as Headscale, instead. |
| Hive.app | This Mac's server, and the hive servers you add. |
| hive for Android | The hive servers you add, and nothing else. |

The programs you run in hive make their own connections. hive doesn't see or
control them.

## What stays on disk

- Your sessions and each terminal's screen and scrollback, in the state
  directory (`~/.local/state/hive`). See [Persistence and restore](./restore).
- With `--tailscale`, the server's Tailscale node key, in
  `~/.local/state/hive/tailscale`. Anyone who can read it can act as that
  device on your tailnet.
- The hosts you added, in `~/.config/hive/hosts.json` on the Mac, and in the
  app's own storage on the phone.

## What others can see

- Notifications show what an agent asked, such as "Claude needs your
  permission to use Bash". On a phone they can show on the lock screen, and
  you can answer them there. Your phone's notification settings decide.
- The code from **Share Hosts with Phone** holds your hosts' names and
  addresses. Show it only to your own phone.
- Anyone a server lets in can do anything your account on that machine can.
  By default that is only you. See
  [Who can connect](./remote#who-can-connect).
