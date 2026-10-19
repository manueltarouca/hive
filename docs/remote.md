---
title: Remote servers
section: Start
order: 3.7
description: Run hive on other machines and use them from your Mac or phone over your tailnet.
---

# Remote servers

A hive server can run on any machine you reach over
[Tailscale](https://tailscale.com): a Linux box, a lab VM, another Mac. Your
Mac's app, the CLI and the phone use it as they use your own: sessions,
panes, agents and their notifications. The terminals keep running there
while you're away.

## Listening on your tailnet

A server always listens on its Unix socket. To be reachable from other
machines it needs one more listener:

| Option | Listens | Use it when |
| --- | --- | --- |
| `--tailscale` | As its own Tailscale node (built in), at `https://NAME.ts.net` and on port 4483 | The machine has no Tailscale, or you want hive to be its own device |
| `--extra-listen tailnet://:4483` | On the machine's Tailscale addresses, port 4483 | The machine runs Tailscale already (Linux `tailscaled`, or the Mac app) |
| `--extra-listen tailnet-serve:///PATH.sock` | On a socket that `tailscale serve` forwards to | You publish it with Tailscale Serve, with its HTTPS name |
| `--extra-listen HOST:PORT --allow-anonymous` | Plain TCP, no identity | Tests on one machine only |

```sh
hive server start --extra-listen tailnet://:4483
hive server start --tailscale --tailscale-hostname box
```

The first time, `--tailscale` prints a link to add the server to your
tailnet. For a machine without a browser, pass an auth key:
`--tailscale-auth-key-file /path/to/key` (or `-` to read it from stdin).

## Who can connect

Every request is identified before it reaches the server's API, and `hive
whoami` says how. By default a server lets in only the Tailscale user who
owns its machine: you, from any of your devices.

| Option | Also lets in |
| --- | --- |
| `--tailnet-allow-identified` | Anyone Tailscale identifies, on `tailnet://` and `tailnet-serve://` listeners |
| `--tailscale-allow-tailnet-members` | Anyone your tailnet's ACLs let reach the built-in node |
| `--allow-anonymous` | Anyone who reaches a plain TCP listener |

A tagged device (`tag:ci`) is never its owner, whatever its user is: it
gets in only with the widening options. A server refused you when `hive -S
box whoami` exits with status 5; the app and the phone say "refused".

A hive server runs your programs as you. Anyone it lets in can type into
every terminal, read every screen and start new programs. Keep the default
unless you mean to share the machine, and never put a plain listener on a
network other people use: it can't tell callers apart.

## Adding a host

Name a server once, and every client uses it by its label:

```sh
hive hosts add box https://box.example.ts.net
hive -S box ls
```

In the Mac app, choose **Add Remote Host…** in the Hive menu. A bare name
(`box`) is tried at `https://box`, then `https://box:4483`, then
`http://box:4483`, and saved as the address that answered.

Hosts are connected on demand: when you pick one or one of its sessions.
`hive hosts add box URL --always` connects it at launch and keeps it
connected, so its agents' status and notifications stay live.

## On your phone

hive for Android reaches the same hosts through the Tailscale app on the
phone. See [hive on your phone](./android). To use your Mac's own server
from the phone, start it with a tailnet listener:

```sh
hive server stop && hive server start --extra-listen tailnet://:4483
```

Stopping the server restarts your terminals; agents resume where they were
(see [Agents](./agents)).

## When a host is away

The clients keep trying on their own, backing off up to 10 seconds between
tries, and come back the moment the host does. Meanwhile the app and the
phone say why:

| They say | Means |
| --- | --- |
| No hive server running | The machine answers, but nothing listens on hive's port |
| Not answering | Nothing came back: the machine is asleep, off, or off the tailnet |
| Refused this Mac (or phone) | The server doesn't let your Tailscale login in |
| Reconnecting | The connection dropped; the session keeps running on the server |
