---
name: hive-setup
description: Install hive, the terminal multiplexer for agent-heavy work, from a release or from source; set up the macOS app; connect Claude Code to it with hooks; check it works; or remove it. Use it when the user wants to try, install, set up, update, or uninstall hive, when `hive` is not found, or when they paste a hive setup prompt.
---

# Setting up hive

hive is one program, `hive` (server and CLI), plus an optional native macOS
app. Releases have binaries for macOS on Apple Silicon and Linux x86_64. On
any other platform, or to follow `main`, build from source.

Do the steps yourself and report what you did. Before each step that changes
something outside the clone, say what it changes.

## 1. From a release (macOS arm64, Linux x86_64)

Use `curl`, not a browser: macOS blocks command-line programs that a browser
downloaded. Take the latest version from
https://github.com/manueltarouca/hive/releases, and always check the
checksum:

```sh
V=0.1.0 OS=macos-arm64    # or linux-amd64 (needs glibc 2.28+)
cd "$(mktemp -d)"
curl -fsSLO https://github.com/manueltarouca/hive/releases/download/v$V/hive-$V-$OS.tar.gz
curl -fsSLO https://github.com/manueltarouca/hive/releases/download/v$V/SHA256SUMS
shasum -a 256 --ignore-missing -c SHA256SUMS   # Linux: sha256sum --ignore-missing -c SHA256SUMS
tar -xzf hive-$V-$OS.tar.gz
mkdir -p ~/.local/bin && mv hive-$V-$OS/hive ~/.local/bin/
```

- **The app:** download `Hive-$V-macos-arm64.zip` the same way, then
  `ditto -x -k Hive-$V-macos-arm64.zip /Applications`. Use `ditto`: it keeps
  the app's signature intact.
- **First launch:** a `curl` download carries no quarantine, so the app
  opens normally. If the user downloaded it in a browser instead, macOS blocks
  the first launch:
  - macOS 15+: **Open Anyway** in System Settings → Privacy & Security.
  - macOS 14: Control-click → **Open**.
- **PATH:** if `~/.local/bin` isn't on `PATH`, tell the user which line to
  add to their shell profile.

Then skip to step 3 (Check it works). Steps 1b and 2 below build from source
instead.

## 1b. Check the tools

| Tool | Version | Notes |
| --- | --- | --- |
| git | any | |
| Go | 1.26+ | `go version` |
| Zig | **0.16.x exactly** | Ghostty's build refuses other minor versions. The default `brew install zig` is newer, so use `zig@0.16`. |
| Xcode | 27+ | Only to build the macOS app. |

On macOS with Homebrew: `brew install go zig@0.16`. Ask before installing
anything the user doesn't have.

## 2. Build

Clone it somewhere permanent. Claude's hooks call this exact binary, so
moving the clone later breaks them.

```sh
git clone https://github.com/manueltarouca/hive ~/src/hive
cd ~/src/hive
make vt       # once: builds Ghostty's terminal library (about a minute)
make build    # ./bin/hive
```

- **Zig not found** (on Linux, or without Homebrew): run
  `make vt ZIG=/path/to/zig` with a 0.16 Zig.
- **`invalid flag in pkg-config --libs`**: a broken earlier build. Delete
  `third_party/ghostty-vt` and run `make vt` again.

Put it on `PATH` with a link, so that rebuilding updates it:

```sh
mkdir -p ~/.local/bin && ln -sf "$PWD/bin/hive" ~/.local/bin/hive
```

If `~/.local/bin` isn't on `PATH`, tell the user which line to add to their
shell profile.

## 3. Check it works

Use a throwaway server, so the check doesn't touch the user's real state:

```sh
D=$(mktemp -d)
HIVE_SERVER=$D/s.sock HIVE_STATE_DIR=$D/state hive new check --cwd /tmp
HIVE_SERVER=$D/s.sock HIVE_STATE_DIR=$D/state hive -s check run --wait -- echo ok
HIVE_SERVER=$D/s.sock HIVE_STATE_DIR=$D/state hive server stop
```

Set both variables on each command. With `HIVE_SERVER` alone, the server
writes to the real state directory.

## 4. The macOS app (optional, macOS 14+)

```sh
apps/macos/script/bundle.sh          # builds apps/macos/build/Hive.app
open apps/macos/build/Hive.app
```

The app carries its own `hive` and starts a server if none is running. The
user can drag it to `/Applications`.

## 5. Connect Claude Code

The hooks let hive show each Claude session's state (working, blocked,
done) and resume it after a restart. Show the planned change first:

```sh
hive agent install claude --dry-run
hive agent install claude
```

- **What it changes:** six hook entries in `~/.claude/settings.json`, or in
  `$CLAUDE_CONFIG_DIR`. Nothing else in the file changes, and running it
  again changes nothing.
- **Where the hooks act:** only in Claude sessions started inside a hive
  terminal. Elsewhere they exit at once.
- **Folder trust:** Claude Code runs hooks only in folders the user trusts.

Other harnesses (Codex, OpenCode, Gemini, Grok) have no hooks. hive resumes
them when they report a session through `agent.report`.

## 6. Tell the user

Report it like this:

```
hive is set up
  ✓ hive    <path>  (on PATH: yes/no)
  ✓ app     <path or "skipped">
  ✓ hooks   ~/.claude/settings.json (or "skipped")

Start:  hive new work --cwd ~/src/project
Docs:   https://manueltarouca.github.io/hive/docs
```

If they use tmux, mention that the `hive-from-tmux` skill can move their
windows over, one at a time.

## Updating

```sh
cd ~/src/hive && git pull && make build
```

Run `make vt` again only if `GHOSTTY_REV` in the Makefile changed. Restart the
server so it uses the new binary: `hive server stop`. The next command starts
it again, and sessions are restored.

## Uninstalling

Confirm each step with the user. The state directory holds their saved
sessions.

```sh
hive agent uninstall claude        # remove the hooks first, while hive still exists
hive server stop
rm ~/.local/bin/hive               # the link
rm -r ~/src/hive                   # the clone
rm -r ~/.local/state/hive          # saved sessions and scrollback, only if they agree
```
