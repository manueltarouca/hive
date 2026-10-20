---
title: hive on your phone
section: Use hive
order: 15
description: hive for Android: your sessions and agents on the phone, over your tailnet.
---

# hive on your phone

hive for Android shows the same hosts, sessions, tabs and agents as the Mac.
Answer an agent that needs you from a notification, read what it did, or type
into any pane. Install it from the release's APK: see
[Installation](./installation#hive-for-android).

The phone reaches your hosts through the Tailscale app, as your computer does.
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
[Remote hosts](./remote)).

## The session

The app opens the session you were last in. Its title is at the top, with
its host; tap it for the switcher: every host's sessions, **Connect** for a
host that isn't connected yet, **New session**, and **Add remote host**.
Long press a host for **Stay connected**, **Disconnect** and **Forget**, or
open **Hosts** from the switcher.

Below the title are the session's tabs, each with its agents' state, then
one pane at a time. The dots under it are the panes of a split: tap them, or
swipe sideways. **+** opens a new tab.

Pinned tabs come first, here and on the Mac. With more than four tabs, the
strip keeps the pinned ones, the ones that need you and the one you're in,
after a count of them all. Tap the count, or swipe up on the strip, for
every tab: pinned, needs you, working, then the quiet ones, each with its
last two lines. Filter them by name. Swipe a card right to pin it or unpin
it. Long press any tab for **Pin tab**, **Rename** and **Close tab**.

Tap the pane to type (or the keyboard key, in a program that takes taps as
clicks). The key bar has what a phone keyboard lacks: **esc**,
**ctrl** (for the next letter), **tab**, **⌃C**, **paste**, the arrows and a
few symbols. Text with several lines asks before pasting when the program
didn't ask for pasted text, as on the Mac. Drag to scroll. A tap clicks in
programs that use the mouse and opens links.

## Size

A pane has one size, set by the device you use. Under the pane the phone
says who that is: "Sized by Hive.app on studio", "Sized by this phone", or
"Nobody holds this pane's size." A pane sized by another device is zoomed to fit the phone's width,
so it doesn't reflow there; pinch to read closer.

**Fit to phone** gives the pane the phone's size (the other device shows
the rest of the pane empty). **Back to studio** gives it back to whoever
had it, or **Give back** when nobody did. Using the other device takes it back too: typing or clicking in the
pane, or bringing its window to the front. A device showing the pane
without using it never takes it.

## Agents

When an agent in the pane on screen asks something, its choices show under
the pane as buttons, with **Yes** first; tap one to answer. Or write what it
should do instead: hive leaves the question (escape) and sends your words.

In the background, hive notifies you when an agent needs you or is done,
naming the session and host. On a question, **Allow**, **Deny** and
**Reply** answer from the notification, if the agent is still waiting.

By default a host is used on demand: 30 seconds after you leave hive, it
lets go, and nothing runs in the background. To be notified with the phone
in your pocket, turn on **Stay connected** for a host (on **Hosts**). hive
then keeps it open, with a quiet ongoing notification ("Connected to 1
host") whose **Disconnect** lets go of everything. When the phone sleeps
deeply, the system cuts the network: hive reconnects when it wakes and tells
you what changed meanwhile.

## Hosts

**Hosts**, from the switcher, lists every host with its state. For each:

- **Connect** or **Disconnect**. A disconnected host stays saved, and its
  sessions keep running on the server.
- **Stay connected**: keep it open in the background, for notifications.
- **Rename**: the label the phone uses for it.
- **Forget**: remove it from the phone, after asking.

**Add a host** and **Scan the code** are at the bottom.

## Settings

**Settings**, from the switcher, has what's about the phone itself:

| Setting | Does |
| --- | --- |
| Theme | System, Night or Day |
| Text size | The size of a pane fitted to the phone |
| Ask before pasting several lines | As on the Mac, when the program didn't ask for pasted text |
| Open panes fitted to the phone | A pane opens at the phone's size, instead of zoomed out to another device's |
| Notifications | When an agent needs you or is done: when hive is in the background, always, or never |

## When a host is away

A card over the pane says what's happening, as on the Mac:
**Reconnecting to box** while the network is gone, or why the host can't be
used. hive keeps trying; the session keeps running on the server.
