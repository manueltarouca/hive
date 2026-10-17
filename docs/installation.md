---
title: Installation
section: Start
order: 1
description: hive builds from source on macOS. The server also runs on Linux.
---

# Installation

hive builds from source. There are no binaries yet.

The server and the command-line interface are one Go program, `hive`. It runs
on macOS and Linux. The app is native macOS and needs macOS 14 or later.

## Requirements

| Tool | Version | Why |
| --- | --- | --- |
| Go | 1.26 | The server and CLI. |
| Zig | **0.16** exactly | Builds libghostty-vt, Ghostty's terminal engine. Ghostty refuses other minor versions. |
| Xcode | 16 or later | The macOS app. The Command Line Tools alone can build the server. |

On macOS, Homebrew has the pinned Zig:

```sh
brew install go zig@0.16
```

## Build

```sh
git clone https://github.com/manueltarouca/hive
cd hive
make vt        # once: builds libghostty-vt from the pinned Ghostty commit
make build     # ./bin/hive
```

`make vt` takes a minute the first time. It fetches one pinned commit of
Ghostty and builds only its terminal library.

## The app

```sh
apps/macos/script/bundle.sh
open apps/macos/build/Hive.app
```

`Hive.app` carries its own copy of `hive`. When it starts and no server is
running, it starts one.

## Check it works

```sh
./bin/hive new demo
./bin/hive ls
```

Any `hive` command starts a server if none is running. `hive server stop`
stops it. Your sessions come back the next time it starts.
