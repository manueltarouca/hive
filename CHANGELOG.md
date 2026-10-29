# Changelog

All notable changes to hive. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and hive uses
[semantic versioning](https://semver.org/).

**Compatibility.** Command names, flags, exit codes, API methods and event
names are a contract. They break only in a new major version. While hive is
0.x, a breaking change can come in a minor version (0.1 → 0.2), never in a
patch, and this file lists it under "Changed" or "Removed".

## [Unreleased]

### Added

- Tap a path a program printed, on the phone or the tablet, to see the file
  without leaving hive: images (pinch to zoom), text with line numbers
  (`:40` opens at that line; the last 2 MB of a long log), folders, and
  anything else through Open in…. A sheet over the pane on a phone, a panel
  beside the panes on a wide screen. Paths are read where the pane is, also
  across wrapped rows. The server serves them at `GET /v1/file`.
- Seeing what scripts do, and turning them off: every change a script
  makes is listed (`hive script show`, `hive script audit -f`, method
  `audit.list`, event `script_acted`) and shows for a few seconds on the
  pane it touched. A script can be put in watch mode (`hive script watch`,
  `script.set_mode`): it may look, mark panes and notify, and every change
  it tries is refused and listed. Scripts declare switches (`options` in
  the manifest, `h.options`, `hive script set`, `script.set_option`), and a
  notification can offer `hive.set_option` and `hive.set_mode` buttons,
  which the server runs for whoever presses them. Only people change
  modes, switches and grants. The Mac app has a Scripts window (Hive →
  Scripts…) and the phone a Scripts page, with all of this.
- hive for Android on tablets: splits side by side when each pane gets 60
  columns, every tab in the strip, pages at a reading width, and on wide
  screens a sessions column beside the panes. One layout adapts to the
  width; phones keep one pane at a time.
- hive for Android: long press selects text, with Copy, Select all and
  Paste. A two-row key bar on phones with all four arrows (held, they
  repeat), **alt**, **⇧tab**, **↵**, **home** and **end**, and **⋯** for
  control keys, symbols, page keys and F1 to F12. **ctrl** and **alt**
  apply to the next key, or stay on with a second tap.
- Hive.app: drag tabs to reorder them in the title bar too, with the others
  making room where the tab will land; Escape puts it back. **Move Tab
  Left** and **Move Tab Right** in the Window menu and the palette.
- Starter scripts: `hive script add` lists and installs `limit-resume`,
  `needs-you`, `stall-watch`, `digest`, `lane-keeper`, `team`,
  `keep-awake` and `dev-panes`.
- Agents stopped by an error, such as Claude Code at its usage limit, are
  `done` with `kind: "error"` and the reason, and the apps show them as
  Stopped. `agent.act` takes `expect_state`, to nudge an agent that is
  idle or done. Mac client action `client.reveal` brings a pane forward;
  `action.run` from the Mac carries the app's `client_id`.


- Scripts: TypeScript or JavaScript the server runs, with the same methods
  and events as the CLI. `hive script run FILE` runs one once, with its log
  on stderr and its result on stdout; `hive script stop`, `hive script ls`,
  `hive script check` and `hive script types` (a `hive.d.ts` for editors)
  go with it. A script can only read unless allowed
  more (`--allow control,spawn`, or a `hive.script` manifest), and what it
  changes goes to an audit log. Methods `script.run`, `script.stop`,
  `script.list` and `script.result`; scoped events `script_started`,
  `script_log` and `script_ended`; setting `script-remote-run`. See
  [Scripts](https://manueltarouca.github.io/hive/docs/scripts).
- Scripts that keep running: the server runs the scripts in
  `~/.config/hive/scripts` once you grant them (`hive script grant`),
  reloads one when you save it, keeps the old version when the new one has
  an error, and quarantines one whose handlers keep running too long.
  `h.store` keeps a script's values across restarts. Commands `hive script
  grant`, `revoke`, `enable`, `disable`, `reload`, `logs` and `audit`;
  methods to match; events `server_started`, `restore_finished` and
  `config_changed`; setting `scripts`.
- Every method says which permission a script needs to call it: `read`,
  `screen`, `control`, `spawn`, `input`, `ui`, `config` or `server`.
  `api list` and `api describe` show it, with a note for methods whose
  arguments decide it (a layout that runs a command needs `spawn`).
- `GET /v1/events?types=a,b` streams only events of those types. Some
  events, such as a script's log lines, reach only a stream that names them,
  so they never crowd the apps' stream or its history.
- Keys as data: `keymap.get` gives an app its shortcuts (its defaults,
  then scripts' `h.bind`, then the settings file's `keybind` lines), with
  sequences (`ctrl+b>c`) and modes (`resize/left`); event
  `keymap_changed`. `client.perform` asks a connected app to do one of its
  own actions. Commands `hive keymap`, `hive actions` and `hive do`.
- Notifications and badges: `notify.send` reaches every app, and the server
  keeps notifications for a day (`notification.list`, `notification.ack`),
  so an app that was closed shows what it missed. `status.set`,
  `status.clear` and `status.list` put badges on panes; event
  `status_changed`.
- Scripts can watch agents (`h.agents.on_state`), offer one-tap answers to
  blocked agents (`h.agents.rule`, through the new `agent.act`, which acts
  only if the agent is still blocked on what was seen), react to what
  terminals print (`h.trigger`, `h.wait_output`), and register actions
  (`action.list`, `action.run`). `hive.terminal.search` finds lines in a
  terminal's screen and scrollback.
- `agent_changed` and `agent.list` carry `kind` (what a blocked agent's
  notification is about: `permission_prompt`, `elicitation_dialog`),
  `revision` (moves on whenever state, message or kind change),
  `updated_at` and `cwd`. `agent.report` takes `kind`; the hook sends it,
  and leaves it out for a server too old to know it.
- Error codes `permission_denied`, `timeout` and `script_unloaded`, for
  scripts.

### Fixed

- A method that crashed while called over the attach stream (the apps'
  connection) stopped the whole server. It now fails that one call with
  `internal`, as it already did over `/v1/call`.
- On macOS, programs in panes could be denied the local network ("No route
  to host"), screen recording and accessibility, with no prompt. macOS held
  whatever had started the server responsible for them: a shell, or a
  Hive.app that had since quit. The server is now responsible for its own
  panes, whoever starts it, and has its own name (`xyz.matm.hive.server`)
  and usage text, so macOS asks for it and you grant it once in System
  Settings; grants to it and to Hive.app now survive rebuilds and updates.
  Hive.app also declares its local network and Apple Events use.
- On macOS, closing the terminal app that ran the first `hive` command
  stopped the server and every terminal in it: macOS stops what an app
  started when it closes. Hive.app now adds the server as its background
  item (Login Items & Extensions), and `hive` asks macOS to start it, so it
  keeps running when you close Hive or a terminal app. It starts on demand,
  not at login, and macOS restarts it after a crash. Terminals started by it
  get a UTF-8 `LANG` from your macOS language when none is set, and commands
  find what your login shell's `PATH` finds. Over SSH, `hive` finds the same
  server as Hive.app.

### Changed

- `client.perform` needs `control` for app actions that change your layout
  (new tabs, splits, closing, resizing); `ui` still covers showing things.
  A script can mark only its own notifications seen.
- Hive.app takes its shortcuts from the server's keymap, with sequences and
  modes, and lists scripts' actions in the command palette. Every default
  has a key a Portuguese keyboard types without Option (⌃⇥ and ⌃⇧⇥ for
  tabs, ⌘' for the shortcuts), and the menus show the keys your keyboard
  types instead of letting macOS move them to other keys. Both apps show
  scripts' badges in pane headers and their notifications, with buttons
  that run the script's action, and show what they missed while closed.
- hive for Android previews what an agent did on the tab sheet's cards, not
  its input box and status bar, and draws symbols such as ⏵ and ⏸ that its
  font lacks. Scripts' actions are in the switcher.
- hive for Android: a tap on the pane no longer brings up the keyboard; the
  keyboard key and a two-finger tap do. A pane nobody else is using opens
  fitted to the device. A hardware keyboard types without the on-screen one.
- Day theme text reads at 4.5:1: muted is `#6F675C`, honey, coral and sage
  words use darker text colours, and words on honey are ink. Scripts' state
  colours follow the brand: honey running, coral waiting for your OK.

- `session.new_split`, `session.move_block` and split layouts refuse a
  `ratio` outside 0 to 1 with `bad_request`. It used to become 0.5 without a
  word, so a percentage such as 30 split the pane in half. Leaving `ratio` out
  still means half.

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
