#!/usr/bin/env python3
"""End to end on a tablet (or any screen 600 dp wide or more): what a wide screen does that a
phone doesn't. Same throwaway server and helpers as e2e.py; coordinates come from the UI dump.

    ANDROID_SERIAL=emulator-5556 python3 script/e2e_wide.py [--night] [--landscape]

Shots go to build/e2e-wide.
"""
import os
import re
import shutil
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import e2e  # noqa: E402
from e2e import adb, wait, tap, find, dump, bounds, until, shot, open_switcher, hide_keyboard, keyboard_up, streaming  # noqa: E402

e2e.OUT = os.path.join(e2e.APP, "build", "e2e-wide")
HERE = e2e.HERE
LONG = ["python3", os.path.join(HERE, "fake-claude-long.py")]
DENSITY = 2.0  # hive_tab: 320 dpi


def rotate(landscape):
    w, h = map(int, adb("shell", "wm", "size").split(":")[-1].strip().split("x"))
    adb("shell", "settings", "put", "system", "accelerometer_rotation", "0")
    adb("shell", "settings", "put", "system", "user_rotation", "0" if landscape == (w > h) else "1")
    time.sleep(2)


def terminals(root):
    """The pane views on screen, left to right then top to bottom."""
    out = [n for n in root.iter("node") if n.get("class") == "android.view.View" and n.get("focusable") == "true" and
           (n.get("content-desc") or "").count("\n") >= 2]
    return sorted(out, key=lambda n: (bounds(n)[1] // 200, bounds(n)[0]))


def typed_line(srv, block):
    for line in srv.text(block).splitlines():
        if line.startswith("> "):
            return line[2:]
    return ""


def setup(srv):
    e2e.quiet_other_devices()
    srv.start()
    srv.hive("new", "demo", "--cwd", "/tmp", "--", "sh", "-c", "echo hello from hive; exec sh")
    srv.hive("new", "pair", "--cwd", "/tmp", "--", *LONG)
    sid = srv.sessions()["pair"]["session_id"]
    left = srv.view("pair")["windows"][0]["layout"]["block_id"]
    srv.call("session.new_split", {"session_id": sid, "block_id": left, "direction": "horizontal", "focus": False,
                                   "layout": {"cwd": "/tmp", "command": LONG}})
    srv.hive("new", "ask", "--cwd", "/tmp", "--", *LONG)
    sid = srv.sessions()["ask"]["session_id"]
    a = srv.view("ask")["windows"][0]["layout"]["block_id"]
    srv.call("session.new_split", {"session_id": sid, "block_id": a, "direction": "horizontal", "focus": False,
                                   "layout": {"cwd": "/tmp", "command": ["python3", os.path.join(HERE, "fake-agent.py")]}})
    adb("logcat", "-b", "crash", "-c")
    adb("shell", "cmd", "statusbar", "collapse")
    adb("shell", "pm", "clear", e2e.PKG)
    adb("shell", "pm", "grant", e2e.PKG, "android.permission.POST_NOTIFICATIONS", check=False)
    adb("shell", "settings", "put", "secure", "stylus_handwriting_enabled", "0")


def go(label):
    open_switcher()
    tap(label, exact=True)
    wait("Switch session")
    time.sleep(1.5)


LANDSCAPE = "--landscape" in sys.argv


def step_one_at_a_time(srv):
    """Upright, two panes would get under 60 columns each: one at a time, with the dots; no rail."""
    go("pair")
    wait("Pane 1 of 2")
    assert len(terminals(dump())) == 1, "two panes side by side upright"
    assert find(dump(), "Show sessions") is None, "a rail offered on a screen under 1200 dp"
    v = srv.view("pair")
    left = v["windows"][0]["layout"]["a"]["block_id"]
    until("fitted to the whole width", lambda: srv.view("pair")["blocks"][left]["cols"] >= 80)
    shot("one-at-a-time")


def step_side_by_side(srv):
    go("pair")
    v = srv.view("pair")
    left = v["windows"][0]["layout"]["a"]["block_id"]
    right = v["windows"][0]["layout"]["b"]["block_id"]
    panes = until("two panes side by side", lambda: len(t := terminals(dump())) == 2 and list(t))
    (l1, t1, r1, b1), (l2, t2, r2, b2) = bounds(panes[0]), bounds(panes[1])
    assert abs(t1 - t2) < 10 and r1 <= l2, f"not side by side: {bounds(panes[0])} {bounds(panes[1])}"
    assert find(dump(), "Pane 1 of 2") is None, "the pager's dots show beside a split"
    # Nobody held them: this tablet took each at its own size, about half its width each.
    until("both fitted", lambda: all(srv.view("pair")["blocks"][b]["size_owner"] for b in (left, right)))
    lc, rc = (srv.view("pair")["blocks"][b]["cols"] for b in (left, right))
    assert lc >= 40 and rc >= 40 and abs(lc - rc) <= 3, f"columns {lc} and {rc}"
    shot("side-by-side")
    # Keys go to the focused pane; a tap on the other focuses it there, and not on the server.
    focused0 = srv.view("pair")["windows"][0]["focused_block_id"]
    tap("Up", exact=True)
    until("the left pane got the key", lambda: typed_line(srv, left) == "[↑]")
    l, t, r, b = bounds(terminals(dump())[1])
    adb("shell", "input", "tap", str((l + r) // 2), str((t + b) // 2))
    time.sleep(0.6)
    assert not keyboard_up(), "a tap brought up the keyboard"
    tap("Down", exact=True)
    until("the right pane got the next key", lambda: typed_line(srv, right) == "[↓]")
    assert typed_line(srv, left) == "[↑]", "a key went to the pane that lost focus"
    assert srv.view("pair")["windows"][0]["focused_block_id"] == focused0, "a tap on the tablet moved the session's focus"
    shot("right-focused")


def step_ask_in_its_pane(srv):
    go("ask")
    v = srv.view("ask")
    agent = v["windows"][0]["layout"]["b"]["block_id"]
    srv.call("agent.report", {"block_id": agent, "harness": "claude", "state": "blocked", "message": "Bash: python backtest.py"})
    if not LANDSCAPE:
        tap("Pane 2 of 2")  # one at a time: the agent's pane
    # The card's own text (the terminal's description has the same words).
    found = until("the ask card", lambda: [n for n in dump().iter("node") if (n.get("text") or "").startswith("Do you want to proceed")], timeout=10)
    card = found[0]
    panes = terminals(dump())
    right = bounds(panes[-1])
    cl, ct, cr, cb = bounds(card)
    # Under the agent's pane, as wide as it: not across both.
    assert cl >= right[0] - 20 and cr <= right[2] + 20 and ct >= right[3] - 4, f"the card {bounds(card)} isn't under its pane {right}"
    shot("ask-in-its-pane")


def step_rail(srv):
    go("pair")
    root = dump()
    if find(root, "Filter or create") is None:
        tap("Show sessions", exact=True)
    wait("Filter or create")
    shot("rail")
    # Picking from the rail keeps it open.
    tap("demo", exact=True)
    time.sleep(1)
    wait("Filter or create")
    wait("hello from hive")
    tap("Hide sessions", exact=True)
    wait("Filter or create", gone=True)
    shot("rail-hidden")


def step_keyboard_up(srv):
    go("pair")
    tap("Keyboard", exact=True)
    until("the keyboard", keyboard_up)
    time.sleep(1)
    root = dump()
    assert find(root, "Filter or create") is None, "the rail stayed with the keyboard up"
    assert find(root, "Sized by") is None and find(root, "Nobody holds") is None, "the size line stayed with the keyboard up"
    shot("keyboard-up")
    tap("Hide keyboard", exact=True)
    until("the keyboard went", lambda: not keyboard_up())


def step_reading_width(srv):
    open_switcher()
    tap("Settings", exact=True)
    wait("Appearance")
    n = wait("Theme", exact=True)
    w = int(adb("shell", "wm", "size").split(":")[-1].strip().split("x")[0])
    if int(adb("shell", "settings", "get", "system", "user_rotation").strip() or 0) % 2:
        w = int(adb("shell", "wm", "size").split(":")[-1].strip().split("x")[1])
    # The column is centered at 640 dp: its first label starts no further left than that allows.
    left = bounds(n)[0]
    assert left >= w / 2 - 340 * DENSITY, f"Settings starts at x={left} on a {w} px wide screen: not a reading width"
    shot("settings")
    tap("Back")


def step_rotate(srv):
    go("pair")
    v = srv.view("pair")
    blocks = [v["windows"][0]["layout"]["a"]["block_id"], v["windows"][0]["layout"]["b"]["block_id"]]
    before = [srv.view("pair")["blocks"][b]["cols"] for b in blocks]
    landscape = "--landscape" in sys.argv
    rotate(not landscape)
    # Both panes follow the new room, once it settles.
    until("both panes refitted", lambda: [srv.view("pair")["blocks"][b]["cols"] for b in blocks] != before, timeout=10)
    time.sleep(1.5)
    after = [srv.view("pair")["blocks"][b]["cols"] for b in blocks]
    shot("rotated")
    rotate(landscape)
    until("back", lambda: [srv.view("pair")["blocks"][b]["cols"] for b in blocks] == before, timeout=10)
    assert all(c >= 40 for c in after) or len(terminals(dump())) == 1, f"rotated columns {after}"


STEPS = ([step_side_by_side, step_ask_in_its_pane, step_rail] if LANDSCAPE else [step_one_at_a_time, step_ask_in_its_pane]) + \
    [step_keyboard_up, step_reading_width, step_rotate]


def main():
    adb("shell", "cmd", "uimode", "night", "yes" if "--night" in sys.argv else "no")
    shutil.rmtree(e2e.OUT, ignore_errors=True)
    srv = e2e.Server()
    failed = False
    try:
        setup(srv)
        rotate("--landscape" in sys.argv)
        e2e.step_add_host(srv)
        for step in STEPS:
            name = step.__name__.removeprefix("step_")
            t0 = time.time()
            try:
                e2e.to_desk()
                hide_keyboard()
                step(srv)
                crash = adb("logcat", "-d", "-b", "crash", check=False)
                if e2e.PKG in crash:
                    raise AssertionError("the app crashed:\n" + "\n".join(crash.strip().splitlines()[:12]))
                print(f"ok    {name} ({time.time() - t0:.1f}s)")
            except Exception as ex:
                failed = True
                p = shot(f"FAILED-{name}")
                print(f"FAIL  {name}: {ex}\n      {p}")
                break
    finally:
        adb("shell", "settings", "put", "system", "user_rotation", "0")
        srv.stop()
        shutil.rmtree(srv.dir, ignore_errors=True)
    print("screenshots:", e2e.OUT)
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
