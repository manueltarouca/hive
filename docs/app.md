---
title: The app
section: Start
order: 3
description: The macOS app, its shortcuts, and how it draws your terminals.
---

# The app

The app shows one session at a time. Each tab is a window. Each pane in a tab
is a block, drawn as a card with its program and directory on top.

It draws terminals with Ghostty's terminal engine, so programs behave the same
as they do in Ghostty. Scrolling, scrollback and selection are native. There
is no copy mode.

## Shortcuts

Press ⌘/ (or **Help → Keyboard Shortcuts**) to see them all in the app, with
the editing keys and mouse gestures. The window reads the menu bar, so it
shows the keys as your keyboard layout types them: on some layouts ⇧⌘[ and
⇧⌘] are other keys.

| Keys | Does |
| --- | --- |
| ⌘T | New tab |
| ⌘D / ⇧⌘D | Split right / down |
| ⌘W / ⇧⌘W | Close pane / tab |
| ⇧⌘↩ | Zoom the pane |
| ⌥⌘ arrows | Focus the pane in that direction |
| ⌃⌘ arrows | Move the border in that direction |
| ⌘1…⌘9, ⇧⌘[ ⇧⌘] | Switch tabs |
| ⌘K | Change session |
| ⇧⌘N | New session |
| ⇧⌘P | Command palette |
| ⇧⌘S | Agents sidebar |
| ⌘/ | Keyboard shortcuts |

## Icons

Tabs and the sidebar show each program as a honeycomb cell: its own icon
from its vendor (Claude Code, Codex, Gemini CLI, Grok, OpenCode, Ollama,
Python, Bun, Go, Rust, Docker), else its logo on its brand colour (Node,
Vim, Ruby and others), else a terminal. A tab with several panes shows the
other panes' cells peeking out behind. Sources and licences are in the app's
third-party notices.

## Sessions

Click the session name in the title bar, or press ⌘K. Type to filter. Type
a name that doesn't exist and press return to create it.

The app reopens the session you were last in, on whichever machine it is.

## Remote hosts

The app shows sessions on other machines too: hive servers on your
tailnet. Choose **Add Remote Host…** (in the Hive menu, the session list or
the command palette) and give it a label and an address, such as
`https://box.example.ts.net` or a machine name. The server there must let
you in (see `hive server start --tailscale`).

Each host gets its own section in the session list. A saved host is
connected on demand: it's listed with **Connect**, and connects when you pick
it (or at launch, if the session you were last in is on it). Once connected,
its sessions show under it, with **New Session on …**. In the command
palette, **Connect to Remote Host**, **Disconnect from Remote Host** and
**Remove Remote Host** each list the hosts they apply to. A host saved with `hive hosts add
--always` connects at launch and stays connected, so its agents' status and
notifications are live while you're elsewhere. A session on another machine works as a local one: panes, splits,
agents and their status, and notifications, which name the host. The title
bar shows a globe and the host's name. While the host is away, the title
bar says so in a word, and the panes dim under a card that says what's
happening and why; the app keeps trying, and the card goes the moment the
host is back. The app reconnects on its own.

**Share Hosts with Phone…** (in the Hive menu) shows a code that brings your
hosts to hive on your phone, and this Mac too when its server listens on the
tailnet. See [Remote servers](./remote) and [hive on your phone](./android).

Hosts are the same list as `hive hosts`: one added with the CLI shows up in
the app, and the other way round. Remove one from the command palette.

## The sidebar

Press ⇧⌘S to show the session's tabs as a list. A tab with a coding agent
shows the agent's task and what it is doing:

- **Working:** a honey hexagon.
- **Blocked:** a Blocked badge, and the reason it is waiting.
- **Done:** a green hexagon, until you look at the tab.

Right-click a tab for **Pin Tab**: pinned tabs come first, in the title bar,
the sidebar and on the phone, and ⌘1…⌘9 count them first. Drag a tab among
the pinned ones to pin it, or out of them to unpin it.

Drag a tab to move it. When an agent in another session needs you, the
session name in the title bar shows a count, and the session picker (⌘K)
says which session.

See [Agents](./agents) for how agents report their state.

## Theme

Choose **Hive Day**, **Hive Night** or **Match System** in the View menu, or
type "theme" in the command palette. Your choice is saved in the settings
file (`theme`); see [Settings](./config).

## Mouse

Some programs use the mouse themselves, such as Claude Code. In those
programs, clicks, drags and the scroll wheel go to the program. That's how
you scroll Claude Code and copy from it. When a program copies text, it goes
on your clipboard, and the pane says **Copied**. Like anything on the Mac
clipboard, it can reach your other devices through Universal Clipboard.

Hold **Shift** to select and scroll in hive instead.

## Links

Hold ⌘ over a link to see it underlined, with the pointing hand, and
⌘-click to open it: a
link a program set (OSC 8), or a web address or file path in the text
(`/abs`, `~/`, `./`, `../`, with a `:line:col` after it). A web link opens
in your browser, a file in its app (Preview for an image), a folder in
Finder. A link to anything that could run, such as an app, a script, an
installer or an executable, is shown in Finder instead; another kind of
link (`ssh://`) asks first. On a remote host, only web links open: its
paths aren't on this Mac.

hive tells programs it shows links (`FORCE_HYPERLINK=1` in every pane).
Claude Code then makes its file paths and links clickable with a plain
click, as in Ghostty. Some command-line tools also write link codes into
piped or redirected output when it's set; to turn it off, set
`FORCE_HYPERLINK=0` in the environment hive starts with.

## Selection

Drag to select. Double-click selects a word, triple-click a line. ⌘C copies.
Typing clears the selection. In programs that use the mouse, hold Shift
while you drag.

## Size

A program's terminal has one size, even when several clients show it: the
size of the device you use. The app takes it when you bring its window to
the front, resize it, or type, click or scroll in a pane. It doesn't take it
back on its own while another device, such as your phone, has the pane
fitted to it. Another client showing the same pane sees it at that size.
