---
title: Installation
section: Start
order: 1
description: Download hive for macOS or Linux, or build it from source.
---

# Installation

The server and the command-line interface are one program, `hive`. It runs
on macOS and Linux. The app is native macOS and needs macOS 14 or later on
Apple Silicon.

## Download

Each [release](https://github.com/manueltarouca/hive/releases) has `hive`
for macOS (Apple Silicon) and Linux (x86_64, glibc 2.28 or later),
`Hive.app`, and a `SHA256SUMS` file. Download with `curl` rather than a
browser: macOS blocks programs that a browser downloaded until you approve
them.

```sh
V=0.1.0 OS=macos-arm64    # or linux-amd64
curl -fsSLO https://github.com/manueltarouca/hive/releases/download/v$V/hive-$V-$OS.tar.gz
curl -fsSLO https://github.com/manueltarouca/hive/releases/download/v$V/SHA256SUMS
shasum -a 256 --ignore-missing -c SHA256SUMS   # on Linux: sha256sum --ignore-missing -c SHA256SUMS
tar -xzf hive-$V-$OS.tar.gz
mkdir -p ~/.local/bin && mv hive-$V-$OS/hive ~/.local/bin/
```

If `hive` isn't found afterwards, add `~/.local/bin` to your `PATH`, for
example with `export PATH="$HOME/.local/bin:$PATH"` in `~/.zshrc`.

Each download also has a signed record of where it was built: the release
workflow, from the tagged commit. With the
[GitHub CLI](https://cli.github.com):
`gh attestation verify hive-$V-$OS.tar.gz --owner manueltarouca`.

### The app

```sh
curl -fsSLO https://github.com/manueltarouca/hive/releases/download/v$V/Hive-$V-macos-arm64.zip
ditto -x -k Hive-$V-macos-arm64.zip /Applications
```

`Hive.app` carries its own copy of `hive`.

The app isn't signed with an Apple Developer ID yet. Downloaded with `curl`,
it opens normally. Downloaded with a browser, macOS blocks it the first
time:

- **macOS 15 and later:**
  1. Open `Hive.app`. macOS says it can't verify it. Click **Done**.
  2. Open **System Settings → Privacy & Security**, scroll down, and click
     **Open Anyway** next to the message about Hive.
  3. Confirm with **Open Anyway** and your password.
- **macOS 14:** Control-click `Hive.app` in Finder, choose **Open**, then
  **Open** again.

macOS remembers your choice. Or, from a terminal:
`xattr -dr com.apple.quarantine /Applications/Hive.app`.

## Build from source

### Requirements

| Tool | Version | Why |
| --- | --- | --- |
| Go | 1.27 | The server and CLI. |
| Zig | **0.16** exactly | Builds libghostty-vt, Ghostty's terminal engine. Ghostty refuses other minor versions. |
| Xcode | 27 or later | The macOS app. Releases are built with Xcode 27.1. The Command Line Tools alone can build the server. |

On macOS, Homebrew has the pinned Zig:

```sh
brew install go zig@0.16
```

### Build

```sh
git clone https://github.com/manueltarouca/hive
cd hive
make vt        # once: builds libghostty-vt from the pinned Ghostty commit
make build     # ./bin/hive
```

`make vt` takes a minute the first time. It fetches one pinned commit of
Ghostty and builds only its terminal library.

### The app

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
