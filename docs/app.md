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

## Sessions

Click the session name in the title bar, or press ⌘K. Type to filter. Type
a name that doesn't exist and press return to create it.

The app reopens the session you were last in.

## The sidebar

Press ⇧⌘S to show the session's tabs as a list. A tab with a coding agent
shows the agent's task and what it is doing:

- **Working:** a honey hexagon.
- **Blocked:** a Blocked badge, and the reason it is waiting.
- **Done:** a green hexagon, until you look at the tab.

Drag a tab to move it. When an agent in another session needs you, the
session name in the title bar shows a count, and the session picker (⌘K)
says which session.

See [Agents](./agents) for how agents report their state.

## Theme

Choose **Hive Day**, **Hive Night** or **Match System** in the View menu, or
type "theme" in the command palette. hive remembers your choice.

## Mouse

Some programs use the mouse themselves, such as Claude Code. In those
programs, clicks, drags and the scroll wheel go to the program. That's how
you scroll Claude Code and copy from it. When a program copies text, it goes
on your clipboard, and the pane says **Copied**. Like anything on the Mac
clipboard, it can reach your other devices through Universal Clipboard.

Hold **Shift** to select and scroll in hive instead.

## Selection

Drag to select. Double-click selects a word, triple-click a line. ⌘C copies.
Typing clears the selection. In programs that use the mouse, hold Shift
while you drag.

## Size

A program's terminal has one size, even when several clients show it. The app
takes the size when you focus its window or click into a pane. Another
client showing the same pane sees it at that size.
