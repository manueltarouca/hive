---
title: The Mac app
section: Use hive
order: 11
description: The macOS app, its shortcuts, and how it draws your terminals.
---

# The Mac app

The app shows one session at a time, as tabs. Each tab holds panes, drawn as
cards with the program and its folder on top.

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
| ⌘1…⌘9, ⇧⌘[ ⇧⌘] | Switch tabs (pinned tabs count first) |
| ⌘K | Change session |
| ⇧⌘N | New session |
| ⇧⌘P | Command palette |
| ⇧⌘S | Agents sidebar |
| ⌘/ | Keyboard shortcuts |
| ⌘, | Settings |

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

The app shows sessions on other machines too: hive servers on your tailnet.
Choose **Add Remote Host…** (in the Hive menu, the session list or the
command palette) and give it a label and an address, such as
`https://box.example.ts.net` or a machine name. The server there must let
you in: see [Who can connect](./remote#who-can-connect).

Each host has its own section in the session list. A saved host connects on
demand: it's listed with **Connect**, and connects when you pick it, or at
launch if the session you were last in is on it. Once connected, its
sessions show under it, with **New Session on …**. A host saved with
`hive hosts add --always` connects at launch and stays connected, so its
agents' status and notifications stay live.

A session on another machine works as one on this Mac: panes, splits,
agents, and notifications that name the host. The title bar shows a globe
and the host's name.

When a host is away, the title bar says why in a few words: "Reconnecting",
"Not answering", "No hive server running" or "Refused this Mac". The panes
dim under a card that explains. The app keeps trying, and the card goes the
moment the host is back.

In the command palette, **Connect to Remote Host**, **Disconnect from Remote
Host** and **Remove Remote Host** each list the hosts they apply to.

**Share Hosts with Phone…** (in the Hive menu) shows a code that brings your
hosts to hive on your phone, and this Mac too when its server listens on the
tailnet. See [Remote hosts](./remote) and [hive on your phone](./android).

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

## The menu bar

hive's three cells sit in the menu bar. The top cell fills while agents are
working, and a count says how many need you. The menu lists them, blocked
first, then finished ones you haven't looked at, then the ones working:
pick one to open its pane. It also has your hosts, to connect or
disconnect, **This Mac**, which shows how your phone and other Macs reach
this one, **New Session**, **Open Hive** and **Settings…**.

Closing the window keeps hive in the menu bar, and your sessions keep
running. Open it again from the menu or the Dock; ⌘Q quits.

## Settings

**Hive → Settings…** (⌘,) has the app's settings:

| Section | Settings |
| --- | --- |
| Appearance | Theme, font, font size |
| Terminal | Bell, blinking cursor, ask before pasting several lines, scrollback |
| Agents | Notifications (when hive is in the background, always, or never), resume agents when the server restarts |

Everything there is saved in the settings file, and editing the file updates
the window. **Open Config File** opens it. See [Settings](./settings).

## Theme

Choose **Hive Day**, **Hive Night** or **Match System** in the View menu, or
type "theme" in the command palette. Your choice is saved in the settings
file (`theme`); see [Settings](./settings).

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
