# Changelog

All notable changes to hive. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and hive uses
[semantic versioning](https://semver.org/).

**Compatibility.** Command names, flags, exit codes, API methods and event
names are a contract. They break only in a new major version. While hive is
0.x, a breaking change can come in a minor version (0.1 → 0.2), never in a
patch, and this file lists it under "Changed" or "Removed".

## [Unreleased]

## [0.1.0] - 2026-10-17

The first release.

### Downloads

- `hive` for macOS on Apple Silicon, and for Linux x86_64 with glibc 2.28 or
  later: the server and the CLI in one program.
- `Hive.app` for macOS 14 or later on Apple Silicon. It isn't signed with an
  Apple Developer ID yet. If you download it with a browser, macOS blocks the
  first launch until you approve it in System Settings; with `curl`, it opens
  normally. See
  [Installation](https://manueltarouca.github.io/hive/docs/installation).
- `SHA256SUMS`, and a signed build provenance attestation for every file.

### Added

- **Sessions, windows and splits** that keep running in a server and come
  back after a restart: layouts, labels, scrollback, and a shell in each
  pane's last directory.
- **Agents:** Claude Code hooks report each agent's state (working,
  blocked, done), and `hive agents` lists them. After a restart, agents
  resume their conversation (Claude Code, Codex, OpenCode, Gemini, Grok).
- **One API for everything:** JSON over a Unix socket that describes
  itself (`hive api list`, `hive api describe`). There is an event stream,
  and an attach protocol for clients.
- **The CLI:** `new`, `run`, `split`, `send`, `send-key`, `capture`,
  `wait`, `events`, `agents`, and more, with `--json` everywhere and stable
  exit codes.
- **Hive.app:** a native macOS client drawn with Metal on Ghostty's
  terminal engine. It has an agents sidebar, a command palette, a session
  picker, native selection and scrollback, and a Hive Day / Hive Night /
  Match System theme setting. The sidebar shows each tab's agent, its task
  and state; tabs reorder by dragging. Tabs show each program as an app
  icon, and the server names programs as people do (Claude Code, not its
  version number; npm tools, not node). Grok, which runs Claude Code's
  hooks, is told apart from Claude, and an agent killed without ending its
  session ends when its shell returns, so it isn't resumed. ⌘/ shows every shortcut. Accents typed
  with dead keys (~, ´, ^) reach programs as typed, Claude Code included.
- **The mouse in terminal programs:** the scroll wheel, clicks and drags
  reach programs that use the mouse (Claude Code's scrolling and copy), and a
  program's copy (OSC 52) lands on the clipboard from the focused pane.
- **Agent skills** for Claude Code and other agents: `hive`, `hive-setup`
  and `hive-from-tmux`.

[Unreleased]: https://github.com/manueltarouca/hive/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/manueltarouca/hive/releases/tag/v0.1.0
