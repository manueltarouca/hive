---
title: hive on your phone
section: Start
order: 3.8
description: hive for Android: your sessions and agents on the phone, over your tailnet.
---

# hive on your phone

hive for Android is the Mac app, small: the same servers, sessions, tabs and
agents, live. Answer an agent that needs you from the lock screen, read what
it did, or type into any pane.

The phone reaches your hosts through the Tailscale app, as your Mac does.
Sign in to Tailscale on the phone with the same account.

## Adding hosts

The first screen asks for a host: type its address, as in the Mac's **Add
Remote Host…** (`box.example.ts.net`, `box`, or a URL). hive tries it and
saves the address that answered.

Or bring every host across at once: on the Mac choose **Share Hosts with
Phone…** in the Hive menu, then on the phone **Scan the code** (or copy the
code on the Mac and **Paste it**). The phone lists each host and its address
and adds the ones you keep checked. A host that isn't on a tailnet, or that
would change the address of one you saved, starts unchecked. The code carries
labels and addresses only: who may connect comes from your tailnet.

Your Mac is in the code when its server listens on the tailnet (see
[Remote servers](./remote)).

## The session

The app opens the session you were last in. Its title is at the top, with
its host; tap it for the switcher: every host's sessions, **Connect** for a
host that isn't connected yet, **New session**, and **Add remote host**.
Long press a host for **Stay connected**, **Disconnect** and **Forget**.

Below the title are the session's tabs, each with its agents' state, then
one pane at a time. The dots under it are the panes of a split: tap them, or
swipe sideways. **+** opens a new tab.

Tap the pane to type (or the keyboard key, in a program that takes taps as
clicks). The key bar has what a phone keyboard lacks: **esc**,
**ctrl** (for the next letter), **tab**, **⌃C**, **paste**, the arrows and a
few symbols. Text with several lines asks before pasting when the program
didn't ask for pasted text, as on the Mac. Drag to scroll. A tap clicks in
programs that use the mouse and opens links.

## Size

A pane keeps your Mac's size, zoomed to fit the phone's width, so the Mac
doesn't reflow. Pinch to read closer. **Fit to phone** gives the pane the
phone's size instead (the Mac shows the rest of the pane empty). **Mac size**
stops asking for it, and the Mac takes its size back as soon as you use it
there.

## Agents

When an agent in the pane on screen asks something, its choices show under
the pane as buttons, with **Yes** first; tap one to answer. Or write what it
should do instead: hive leaves the question (escape) and sends your words.

In the background, hive notifies you when an agent needs you or is done,
naming the session and host. On a question, **Allow**, **Deny** and
**Reply** answer from the notification, if the agent is still waiting.

To notify while the phone is in your pocket, hive keeps its connected hosts
open with a quiet ongoing notification ("Connected to 2 hosts"). When the
phone sleeps deeply, the system cuts the network: hive reconnects when it
wakes and tells you what changed meanwhile.

## When a host is away

A card over the pane says what's happening, as on the Mac:
**Reconnecting to box** while the network is gone, or why the host can't be
used. hive keeps trying; the session keeps running on the server.
