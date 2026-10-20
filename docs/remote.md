---
title: Remote hosts
section: Start
order: 3.7
description: Run hive on other machines and use them from your Mac or phone over your tailnet.
---

# Remote hosts

A hive server can run on any machine you reach over
[Tailscale](https://tailscale.com): a Linux box, a lab VM, another Mac. Your
Mac's app, the CLI and the phone use it as they use your own: sessions,
panes, agents and their notifications. The terminals keep running there
while you're away.

## Listening on your tailnet

A server always listens on its Unix socket, which only you can use. To be
reachable from other machines, it needs one more listener:

| Option | Listens | Use it when |
| --- | --- | --- |
| `--tailscale` | As its own Tailscale device (built in), at `https://NAME.TAILNET.ts.net` and on port 4483 | The machine has no Tailscale, or you want hive to be its own device |
| `--extra-listen tailnet://:4483` | On the machine's Tailscale addresses, port 4483 | The machine runs Tailscale already (Linux `tailscaled`, or the Mac app) |
| `--extra-listen tailnet-serve:///PATH.sock` | On a socket that `tailscale serve` forwards to | You publish it with Tailscale Serve, with its HTTPS name |
| `--extra-listen HOST:PORT --allow-anonymous` | Plain TCP, no identity | Tests on one machine only |

A server takes its options when it starts. If one is running, stop it first:

```sh
hive server stop
hive server start --extra-listen tailnet://:4483
```

Stopping a server ends its programs. Shells and agents come back when it
starts; other programs don't (see [Persistence and restore](./restore)).

To keep a listener whoever starts the server (the Mac app starts it at
login, without options), put it in the [settings file](./config):

```sh
hive config set listen tailnet://:4483
```

The setting takes `tailnet://` and `tailnet-serve://` listeners. If one
can't open when the server starts (Tailscale isn't up yet, at login), the
server starts anyway and tries again every 15 seconds. Options given to
`hive server start` replace the setting.

### Built-in Tailscale

With `--tailscale`, the server joins your tailnet as its own device, named
`hive` unless you choose another name:

```sh
hive server start --tailscale --tailscale-hostname box
```

The first time, the server needs to sign in. The link is in
`hive server status` and in the server's log,
`~/.local/state/hive/server.log`. On a machine without a browser, pass a
one-time auth key instead: `--tailscale-auth-key-file FILE`, or `-` to read
it from standard input. The server keeps its login in
`~/.local/state/hive/tailscale`, so it needs the key only once. That folder
holds the device's key: keep it private.

HTTPS needs MagicDNS and HTTPS certificates turned on for your tailnet.
Without them, the server answers on `http://box:4483`.

| Option | Does |
| --- | --- |
| `--tailscale-hostname NAME` | The device's name (default `hive`) |
| `--tailscale-auth-key-file FILE` | Sign in with an auth key, without a browser |
| `--tailscale-advertise-tag tag:NAME` | Sign in as a tagged device. A tagged device has no owner, so this also needs `--tailscale-allow-tailnet-members` |
| `--tailscale-control-url URL` | Use another coordination server, such as Headscale |

A `hive` built with the `notsnet` build tag has no built-in Tailscale, and
says so if you ask for it. The other listeners work without it.

### Keeping a Linux server running

On Linux, run the server as a user service so it starts at boot and keeps
running after you log out:

```ini
# ~/.config/systemd/user/hive.service
[Unit]
Description=hive

[Service]
ExecStart=%h/.local/bin/hive server run
Restart=on-failure

[Install]
WantedBy=default.target
```

```sh
systemctl --user enable --now hive
sudo loginctl enable-linger "$USER"   # keep it running when you log out
```

`hive server run` reads `listen` from the settings file, so the listener
comes with it.

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
phone. See [hive on your phone](./android). For the phone to reach your
Mac's own server, give it a tailnet listener in the settings file, then
restart it:

```sh
hive config set listen tailnet://:4483
hive server stop && hive server start
```

## Who is connected

`hive client ls` lists the apps and phones connected to a server, and who
each one is. `hive whoami` shows who the server thinks you are.

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
