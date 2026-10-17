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

## The agents sidebar

Press ⇧⌘S. The sidebar lists every coding agent in every session. Agents
waiting for you come first, with the reason they are waiting. Click one to
jump to it. Below them are the current session's tabs.

See [Agents](./agents) for how agents report their state.

## Selection

Drag to select. Double-click selects a word, triple-click a line. ⌘C copies.
Typing clears the selection.

## Size

A program's terminal has one size, even when several clients show it. The app
takes the size when you focus its window or click into a pane. Another
client showing the same pane sees it at that size.
