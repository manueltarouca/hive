#!/usr/bin/env python3
"""A visual tour: every main screen of the app, on any device, in one theme
and orientation, for looking at, not for asserting. It reuses the e2e
helpers and a throwaway server (never the user's).

    ANDROID_SERIAL=emulator-5556 python3 script/tour.py NAME [--night] [--landscape] [--keep]

Shots go to build/tour/NAME. Coordinates come from the UI dump, so it runs
on phones and tablets alike.
"""
import os
import shutil
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import e2e  # noqa: E402
from e2e import adb, wait, tap, find, dump, bounds, until, open_switcher, to_desk, hide_keyboard, keyboard_up  # noqa: E402

HERE = e2e.HERE
NAME = next((a for a in sys.argv[1:] if not a.startswith("--")), "tour")
e2e.OUT = os.path.join(e2e.APP, "build", "tour", NAME)
LONG = ["python3", os.path.join(HERE, "fake-claude-long.py")]


def rotate(landscape):
    """Fixes the orientation. A tablet's natural one is landscape, a phone's portrait."""
    w, h = map(int, adb("shell", "wm", "size").split(":")[-1].strip().split("x"))
    natural_landscape = w > h
    adb("shell", "settings", "put", "system", "accelerometer_rotation", "0")
    adb("shell", "settings", "put", "system", "user_rotation", "0" if landscape == natural_landscape else "1")
    time.sleep(1.5)


def sessions(srv):
    srv.hive("new", "demo", "--cwd", "/tmp", "--", "sh", "-c", "echo hello from hive; exec sh")
    # work: Claude Code mid-task; a split with a shell beside it; more tabs.
    srv.hive("new", "work", "--cwd", "/tmp", "--", *LONG)
    sid = srv.sessions()["work"]["session_id"]
    first = srv.view("work")["windows"][0]
    srv.call("session.set_window_label", {"session_id": sid, "window_id": first["window_id"], "label": "claude"})
    w = srv.call("session.new_window", {"session_id": sid, "layout": {"cwd": "/tmp", "command": LONG}})
    srv.call("session.set_window_label", {"session_id": sid, "window_id": w["window_id"], "label": "split"})
    v = srv.view("work")
    split = v["windows"][-1]["layout"]["block_id"]
    srv.call("session.new_split", {"session_id": sid, "block_id": split, "direction": "horizontal",
                                   "layout": {"cwd": "/tmp", "command": ["sh", "-c", "ls -la /usr/bin | head -40; exec sh"]}})
    for label in ("tests", "server", "notes"):
        w = srv.call("session.new_window", {"session_id": sid, "layout": {"cwd": "/tmp"}})
        srv.call("session.set_window_label", {"session_id": sid, "window_id": w["window_id"], "label": label})
    srv.call("agent.report", {"block_id": first["layout"]["block_id"], "harness": "claude", "state": "working"})
    srv.call("session.focus_block", {"session_id": sid, "block_id": first["layout"]["block_id"]})
    srv.hive("new", "agent", "--cwd", "/tmp", "--", "python3", os.path.join(HERE, "fake-agent.py"))
    time.sleep(1)
    a = srv.view("agent")["windows"][0]["layout"]["block_id"]
    srv.call("agent.report", {"block_id": a, "harness": "claude", "state": "blocked", "message": "Bash: python backtest.py"})
    for f in ("digest.ts", "needs-you.ts"):
        shutil.copy(os.path.join(e2e.ROOT, "examples", "scripts", f), srv.env["HIVE_SCRIPTS"])
    srv.call("script.reload")


def pick_tab(label):
    """A tab by name: from the strip when it shows, else from the sheet of all tabs."""
    root = dump()
    if find(root, label, exact=True) is None:
        tap("All ")
        wait("Filter tabs")
    tap(label, exact=True)
    wait("Filter tabs", gone=True)
    time.sleep(1.2)


def select_word():
    """Long presses a word near the pane's top, as a thumb does, and waits for the Copy bar."""
    root = dump()
    term = next(n for n in root.iter("node") if "accept edits" in (n.get("content-desc") or ""))
    a, b, c, d = bounds(term)
    x, y = a + (c - a) // 4, b + 60
    adb("shell", "input", "swipe", str(x), str(y), str(x), str(y), "900")
    until("the Copy bar", lambda: [n for n in [find(dump(windows=True), "Copy", exact=True)] if n is not None], timeout=5)


def go(label):
    open_switcher()
    tap(label, exact=True)
    wait("Switch session")
    time.sleep(1.2)


def main():
    landscape = "--landscape" in sys.argv
    adb("shell", "cmd", "uimode", "night", "yes" if "--night" in sys.argv else "no")
    shutil.rmtree(e2e.OUT, ignore_errors=True)
    srv = e2e.Server()
    os.makedirs(srv.env["HIVE_SCRIPTS"], exist_ok=True)
    try:
        e2e.quiet_other_devices()
        srv.start()
        sessions(srv)
        adb("logcat", "-b", "crash", "-c")
        adb("shell", "pm", "clear", e2e.PKG)
        adb("shell", "pm", "grant", e2e.PKG, "android.permission.POST_NOTIFICATIONS", check=False)
        adb("shell", "settings", "put", "secure", "stylus_handwriting_enabled", "0")
        rotate(landscape)
        e2e.step_add_host(srv)  # shots: add-host, add-host-typed, first-session (demo)
        go("work")
        e2e.shot("claude")
        tap("Keyboard")
        until("the keyboard", keyboard_up)
        time.sleep(0.8)
        e2e.shot("claude-keyboard")
        hide_keyboard()
        tap("More keys", exact=True)
        wait("Control D, end of input", exact=True)
        time.sleep(0.5)
        e2e.shot("more-keys")
        tap("Fewer keys", exact=True)
        tap("Control", exact=True)
        time.sleep(0.4)
        e2e.shot("ctrl-once")
        time.sleep(0.5)
        tap("Control, on for the next key", exact=True)  # off again
        select_word()
        e2e.shot("selected")
        adb("shell", "input", "keyevent", "KEYCODE_ESCAPE")  # a key lets go of it
        time.sleep(0.5)
        pick_tab("split")
        e2e.shot("split")
        go("agent")
        e2e.shot("agent-asks")
        open_switcher()
        e2e.shot("switcher")
        tap("Back")
        go("work")
        n = find(dump(), "All ")
        if n is not None:
            tap("All ")
            wait("Filter tabs")
            time.sleep(0.6)
            e2e.shot("tab-sheet")
            adb("shell", "input", "keyevent", "KEYCODE_BACK")
            time.sleep(0.6)
        open_switcher()
        tap("Settings", exact=True)
        wait("Appearance")
        e2e.shot("settings")
        e2e.scroll_to(f"Scripts on {e2e.LABEL}")
        tap(f"Scripts on {e2e.LABEL}")
        wait("digest", exact=True)
        time.sleep(0.6)
        e2e.shot("scripts")
        to_desk()
        open_switcher()
        tap("Hosts", exact=True)
        wait("Stay connected")
        e2e.shot("hosts")
        crash = adb("logcat", "-d", "-b", "crash", check=False)
        if e2e.PKG in crash:
            print("CRASH:\n" + crash[:2000])
        print("ok    tour", NAME)
    except Exception as ex:
        p = e2e.shot("FAILED")
        print(f"FAIL  tour {NAME}: {ex}\n      {p}")
        sys.exit(1)
    finally:
        rotate(False)
        if "--keep" not in sys.argv:
            srv.stop()
            shutil.rmtree(srv.dir, ignore_errors=True)
        else:
            print("server kept:", srv.env["HIVE_SERVER"])


if __name__ == "__main__":
    main()
