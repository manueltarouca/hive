# Changelog

All notable changes to hive. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and hive uses
[semantic versioning](https://semver.org/).

**Compatibility.** Command names, flags, exit codes, API methods and event
names are a contract. They break only in a new major version. While hive is
0.x, a breaking change can come in a minor version (0.1 → 0.2), never in a
patch, and this file lists it under "Changed" or "Removed".

## [Unreleased]

## [0.1.0] - 2026-10-20

The first release.

### Downloads

- `hive` for macOS on Apple Silicon, and for Linux x86_64 with glibc 2.28 or
  later: the server and the CLI in one program.
- `Hive.app` for macOS 14 or later on Apple Silicon. It isn't signed with an
  Apple Developer ID yet. If you download it with a browser, macOS blocks the
  first launch until you approve it in System Settings; with `curl`, it opens
  normally. See
  [Installation](https://manueltarouca.github.io/hive/docs/installation).
- `hive-0.1.0-android.apk`, hive for Android 10 or later. Install it from
  the file; it isn't on Google Play.
- `SHA256SUMS`, and a signed build provenance attestation for every file.

### Added

- **Sessions, windows and splits** that keep running in a server and come
  back after a restart: layouts, labels, scrollback, and a shell in each
  pane's last directory.
- **Agents:** hooks report each agent's state (working, blocked, done), and
  `hive agents` lists them. After a restart, agents resume their
  conversation (Claude Code, Codex, OpenCode, Gemini, Grok) in your login
  shell, so quitting one leaves a prompt.
- **One API for everything:** JSON over a Unix socket and your tailnet,
  that describes itself (`hive api list`, `hive api describe`). There is an
  event stream, and an attach protocol for clients.
- **The CLI:** `new`, `run`, `split`, `send`, `send-key`, `capture`
  (with `--tail`), `wait`, `events`, `agents`, `client ls`, and more, with
  `--json` everywhere and stable exit codes.
- **Settings** in one file, `~/.config/hive/config`, with `hive config` to
  show, check and change them, and a Settings window in the Mac app (⌘,).
  `listen` keeps a server reachable on your tailnet however it starts.
- **Program status:** programs that report their state with OSC 7501 show
  it, as agents do. Links and paths in a terminal open with ⌘-click.
- **Remote hosts:** a server on another machine, reached over Tailscale.
  Listeners for Tailscale built in (`--tailscale`), Tailscale Serve, a
  tailnet address, or plain HTTP with `--allow-anonymous`. The server knows
  who each caller is from Tailscale and lets in only its owner unless told
  otherwise. `hive hosts` saves them, `-S NAME` runs any command on one, and
  `hive whoami` says who the server thinks you are. Hosts connect when you
pick them, or with `--always` at launch. See
  [Remote hosts](https://manueltarouca.github.io/hive/docs/remote).
- **Hive.app**, a native macOS client drawn with Metal on Ghostty's terminal
  engine:
  - an agents sidebar with each tab's agent, its task and its state;
  - a command palette, a session picker across all your hosts, and ⌘/ for
    every shortcut;
  - tabs that show each program as an app icon, and pinned tabs that come
    first;
  - native selection and scrollback, the mouse in terminal programs, and a
    program's copy (OSC 52) landing on the clipboard;
  - Hive Day, Hive Night, or the system's appearance;
  - hive in the menu bar: the agents that need you, your hosts, and a count
    when something is waiting. Closing the window keeps hive there.
- **hive for Android:** your sessions on the phone, over Tailscale. Tabs
  with each program's icon, pins, and a sheet of every tab grouped by what
  it's doing. A key bar with what a phone keyboard lacks, answers to an
  agent's question as buttons, and notifications you can answer from. Add
  your hosts by scanning a code from the Mac (**Share Hosts with Phone**).
- **Size follows the device you use.** A pane takes the size of the client
  you type in. The phone says who holds a pane's size, can fit it to the
  phone, and gives it back.
- **Agent skills** for Claude Code and other agents: `hive`, `hive-setup`
  and `hive-from-tmux`.

[Unreleased]: https://github.com/manueltarouca/hive/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/manueltarouca/hive/releases/tag/v0.1.0
