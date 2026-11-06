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
the pane. On a phone a split shows one pane at a time: the dots under it
are its panes; tap them, or swipe sideways. **+** opens a new tab.

Pinned tabs come first, here and on the Mac. With more than four tabs, the
strip keeps the pinned ones, the ones that need you and the one you're in,
after a count of them all. Tap the count, or swipe up on the strip, for
every tab: pinned, needs you, working, then the quiet ones, each with its
last two lines. Filter them by name. Swipe a card right to pin it or unpin
it. Long press any tab for **Pin tab**, **Rename** and **Close tab**.

A tap on the pane never brings up the keyboard, so you can read and scroll
without it covering half the screen. The keyboard key, at the right end of
the key bar, shows it and hides it; so does a tap with two fingers. A
hardware keyboard types into the pane without it.

The key bar has what a touch keyboard lacks. On a phone it's two rows:
**esc**, **⌃C**, **⇧tab**, **home**, **end**, **paste**, **tab**, **ctrl**,
**alt**, **↵** and the arrows, with ↑ above ← ↓ →. Hold an arrow to repeat
it. **ctrl** and **alt** apply to the next key; tap twice quickly to keep
them on, once more to let go. **⋯** opens more keys: control keys (Claude
Code's own among them), symbols, page up and down, and F1 to F12. While an
agent asks something, its answers take the first keys of the bar.

Text with several lines asks before pasting when the program didn't ask for
pasted text, as on the Mac. Drag to scroll. A tap clicks in programs that
use the mouse and opens links.

Long press to select a word; drag the handles, or keep your finger down and
drag, to select more. **Copy**, **Select all** and **Paste** show above it.

### Find in a pane

**⋯** on the key bar, then **find**, searches what the pane printed,
scrollback included. The latest match is current; the arrows step through
the others, scrolling to each, and **Done** or Back closes the search.

### Files a pane printed

Tap a path a program printed (`build/shot.png`, `ui/Desk.kt:40`, a folder)
to see it without leaving hive. The path is read where the pane is, on its
host, even when it wraps across rows. On a phone the file opens in a sheet
over the pane, half height: drag it up for more, down to close. On a
tablet or a wide screen it opens beside the panes, which narrow to make
room, so you can read the file and answer the agent at once.

- Images show in the app; pinch to zoom.
- Text shows in the terminal's font with line numbers. `:40` after a path
  opens at that line. Text over 2 MB offers **Show the last 2 MB**, the end
  of a log being what you usually want, or **Open in…**.
- A folder lists its files, newest first; tap one to see it, Back to return.
- Anything else (a PDF, a video, an archive) opens in an app on the device:
  **Open in…** fetches it, with progress and **Cancel**, and hands it over
  read-only. The copy stays a day, then the app deletes it.

A word that only looks like a path (`a/b`) opens nothing when there's no such
file, so a tap to focus a pane doesn't pop up a viewer. A full path that
isn't there says so.

## Size

A pane has one size, set by the device you use. Under the pane the phone
says who that is: "Sized by Hive.app on studio", "Sized by this phone", or
"Nobody holds this pane's size." A pane sized by another device is zoomed to fit the phone's width,
so it doesn't reflow there; pinch to read closer.

A pane nobody else is using opens at the phone's size. **Fit to phone**
gives a pane the phone's size when another device holds it (the other device shows
the rest of the pane empty). **Back to studio** gives it back to whoever
had it, or **Give back** when nobody did. Using the other device takes it back too: typing or clicking in the
pane, or bringing its window to the front. A device showing the pane
without using it never takes it.

## Tablets

hive for Android uses the room it has. On a tablet a split shows its panes
side by side when each gets at least 60 columns, with the focused one in
honey; tap a pane to focus it. Every tab shows in the strip. A tablet takes
a pane's size when you first use it there, as the Mac does, and text starts
a little larger (13). Settings and the other pages keep a width you can
read. On a wide screen (1200 points or more), the switcher can stay open
as a sessions column beside the panes: tap the session's title, or the
sidebar button, to show it or hide it. With the keyboard up, only the panes
and the key bar stay.

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

**Scripts**, below them, has a row per host. Open one to see that
server's scripts and what each does, turn them on or off, grant what a
script asks for, and change its settings.

## When a host is away

A card over the pane says what's happening, as on the Mac:
**Reconnecting to box** while the network is gone, or why the host can't be
used. hive keeps trying; the session keeps running on the server.
