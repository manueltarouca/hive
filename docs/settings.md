---
title: Settings
section: Use hive
order: 16
description: The settings file, what each setting does, and how the app applies changes.
---

# Settings

hive's settings live in one file: `~/.config/hive/config`, or
`$XDG_CONFIG_HOME/hive/config`. The Mac app and the server read it, and the
app applies it as soon as you save it. hive for Android has its own
settings, on the phone (see [hive on your phone](./android#settings)).

The hosts you add are in `hosts.json`, next to the file.

You don't need the file: everything has a default. To create it with every
setting listed and commented out, run:

```sh
hive config path --create
```

## The file

One setting per line, `key = value`. A line starting with `#` is a comment.
A true-or-false setting alone on its line (`confirm-paste`) means true.
Quotes around a value are optional. An empty value (`font-size =`) resets a
setting to its default.

```sh
# ~/.config/hive/config
theme = night
font-size = 14
bell = flash+sound
```

The format is Ghostty's, so a `#` after a value is part of the value, not a
comment.

## Settings

| Setting | Default | What it does |
| --- | --- | --- |
| `theme` | `system` | `system`, `day` (Hive Day) or `night` (Hive Night) |
| `font-family` | empty | A monospaced font installed on this Mac; empty is the bundled JetBrains Mono. A font that isn't installed or isn't monospaced falls back to JetBrains Mono, and the title bar says why. Nerd Font icons draw either way |
| `font-size` | `13` | Points, 8 to 32 |
| `bell` | `flash` | When a program rings the bell: `flash`, `sound`, `flash+sound` or `none`. The Dock bounces once if hive is in the background; `none` turns both off |
| `notify` | `background` | Agent notifications: `background` (when hive isn't in front), `always` or `never` |
| `confirm-paste` | `true` | Ask before pasting lines into a program that would run each one |
| `motion` | `snappy` | How tabs move: `snappy` settles quickly, `natural` springs with a slight overshoot, `off` moves them at once. Reduce Motion in macOS always moves them at once |
| `confirm-close` | `running` | Ask before closing a tab, or its last pane: `running` (only when a program other than the shell runs in it), `always` or `never` |
| `dim-panes` | `light` | How much the panes you're not typing in step back: `light`, `more` or `off` |
| `balance-splits` | `true` | Give every pane in a tab the same room when the app opens or closes one. This replaces sizes set by dragging or `--ratio` |
| `focus-follows-pointer` | `false` | Focus a pane when the pointer moves over it, without a click |
| `cursor-style-blink` | empty | Whether the cursor blinks, with Ghostty's name: empty or `true` blinks (Ghostty's default), `false` keeps it steady. It blinks only in the focused pane, and typing or output shows it again at once. Programs can still choose with `CSI Ps SP q` or DEC mode 12 |
| `scrollback-limit` | `16m` | History kept per terminal, in bytes (`k`, `m`, `g`), at least `64k`. Read by the server; lowering it drops old lines at once |
| `resume-agents` | `true` | Resume agents' sessions when the server starts again. Read when the server starts |
| `listen` | empty | Listeners the server adds when it starts, separated by spaces, such as `tailnet://:4483`. Only `tailnet://` and `tailnet-serve://`. One that can't open is tried again every 15 seconds. Listener options to `hive server start` replace it. See [Remote hosts](./remote) |
| `keybind` | none | Change a shortcut: `KEY=ACTION`, one per line, such as `keybind = cmd+shift+t=client.new_tab`, or `KEY=unbind` to remove one. See [Keys](#keys) |
| `scripts` | `true` | Run the [scripts](./scripts#scripts-that-keep-running) in the scripts folder (beside this file). Read when the server starts |
| `script-remote-run` | `false` | Let callers on other machines (over Tailscale) run [scripts](./scripts) on this server |

## Changing settings

- In the Mac app: **Hive → Settings…** (⌘,).
- Edit the file in any editor. The app picks up the change when you save.
- Choose a theme from **View** or the command palette: hive writes it into
  the file and keeps the rest of the file as it was.
- From a terminal: `hive config set theme night`.

If the file is a link (for example, into a dotfiles folder managed with GNU
Stow), hive writes to the file it points to and leaves the link in place.

## Problems

A setting with a bad value keeps its default, and the title bar shows the
first problem with its line number. Click it to open the file. To see every
problem:

```sh
hive config check
```

It exits with 1 if there are errors (warnings, such as an unknown setting,
don't count).

## Commands

| Command | Does |
| --- | --- |
| `hive config path [--create]` | Prints the file's path; `--create` makes it from the template if it's missing |
| `hive config default` | Prints the template |
| `hive config check` | Lists problems by line |
| `hive config show [--json]` | Every setting's value and where it came from |
| `hive config set KEY VALUE` | Changes one setting, keeping the rest of the file |
| `hive config reload` | Makes the server read the file now (it also checks every 2 seconds) |

`HIVE_CONFIG` names a different file, for tests. `HIVE_STYLE=dark` or
`light` overrides the theme.

## Keys

Every shortcut in the Mac app is a binding you can change. `hive keymap`
lists them, with where each came from. Add a `keybind` line per change:

```
keybind = cmd+shift+t=client.new_tab
keybind = cmd+k=unbind
keybind = ctrl+b>c=client.new_tab
keybind = resize/left=client.resize:{"direction":"left"}
```

A key is written with `ctrl`, `alt`, `shift` and `cmd`; a sequence as
`ctrl+b>c`; a key in a mode as `resize/left`. Arguments, when an action
takes them, follow a colon, as JSON. Your lines win over the app's
defaults and over scripts'. `hive config set keybind VALUE` adds a line,
and `hive config set keybind ""` removes them all.
