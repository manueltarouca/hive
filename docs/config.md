---
title: Settings
section: Start
order: 3.5
description: The settings file, what each setting does, and how the app applies changes.
---

# Settings

hive's settings live in one file: `~/.config/hive/config`, or
`$XDG_CONFIG_HOME/hive/config`. The app and the server read it, and the
app applies it as soon as you save it.

You don't need the file: everything has a default. To create it with every
setting listed and commented out, run:

```sh
hive config path --create
```

## The file

One setting per line, `key = value`. A line starting with `#` is a comment.
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
| `font-family` | empty | A monospaced font; empty is the bundled JetBrains Mono |
| `font-size` | `13` | Points, 8 to 32 |
| `bell` | `flash` | When a program rings the bell: `flash`, `sound`, `flash+sound` or `none` |
| `notify` | `background` | Agent notifications: `background` (when hive isn't in front), `always` or `never` |
| `confirm-paste` | `true` | Ask before pasting lines into a program that would run each one |
| `scrollback-limit` | `16m` | History kept per terminal, in bytes (`k`, `m`, `g`), at least `64k`. Read by the server; lowering it drops old lines at once |
| `resume-agents` | `true` | Resume agents' sessions when the server starts again. Read when the server starts |

## Changing settings

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
