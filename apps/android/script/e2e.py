#!/usr/bin/env python3
"""End to end on an emulator: the debug app against a throwaway hive server.

The server listens on a plain TCP port for the emulator (10.0.2.2 is the
Mac), with its own state and config dirs: never the user's server. Every
step drives the real UI (uiautomator, adb input) and checks the result on
the server, where it counts. Screenshots go to build/e2e.

    python3 script/e2e.py [--release] [--night] [--keep] [--only NAME...]
"""
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import xml.etree.ElementTree as ET

HERE = os.path.dirname(os.path.abspath(__file__))
APP = os.path.dirname(HERE)
ROOT = os.path.dirname(os.path.dirname(APP))
HIVE = os.path.join(ROOT, "bin", "hive")
OUT = os.path.join(APP, "build", "e2e")
# --release: the signed release build (minified); paste needs the debug build's clipboard hook.
PKG = "xyz.matm.hive" if "--release" in sys.argv else "xyz.matm.hive.debug"
PORT = 4490
ADDRESS = f"http://10.0.2.2:{PORT}"
LABEL = "10-0-2-2"


def sh(*args, check=True, env=None, input=None):
    r = subprocess.run(args, capture_output=True, text=True, env=env, input=input)
    if check and r.returncode != 0:
        raise RuntimeError(f"{' '.join(args)}: {r.stderr.strip() or r.stdout.strip()}")
    return r.stdout


def adb(*args, check=True):
    return sh("adb", *args, check=check)


class Server:
    """A throwaway hive server: its own socket, state and config."""

    def __init__(self):
        self.dir = tempfile.mkdtemp(prefix="hive-e2e.")
        self.env = dict(os.environ, HIVE_SERVER=f"{self.dir}/s.sock", HIVE_STATE_DIR=f"{self.dir}/state",
                        HIVE_CONFIG=f"{self.dir}/config", HIVE_SESSION="", HIVE_BLOCK="")

    def hive(self, *args):
        return sh(HIVE, *args, env=self.env)

    def start(self):
        # A second port: the same server at another address, as a Mac by name and by tailnet IP.
        self.hive("server", "start", "--extra-listen", f"127.0.0.1:{PORT}", "--extra-listen", f"127.0.0.1:{PORT + 1}", "--allow-anonymous")

    def stop(self):
        sh(HIVE, "server", "stop", env=self.env, check=False)

    def call(self, method, params=None):
        out = sh("curl", "-s", "--unix-socket", self.env["HIVE_SERVER"], "http://hive/v1/call",
                 "-H", "content-type: application/json", "-d", json.dumps({"method": method, "params": params or {}}))
        j = json.loads(out)
        if "error" in j:
            raise RuntimeError(f"{method}: {j['error']}")
        return j["result"]

    def sessions(self):
        return {s["label"]: s for s in self.call("session.list")["sessions"]}

    def view(self, label):
        return self.call("session.view", {"session_id": self.sessions()[label]["session_id"]})

    def text(self, block):
        return self.call("hive.terminal.format", {"block_id": block, "args": {"trim": True}})["content"]

    def clients(self):
        return self.call("client.list")["clients"]


# MARK: the UI

def dump():
    for _ in range(5):
        adb("shell", "uiautomator", "dump", "/sdcard/ui.xml", check=False)
        x = subprocess.run(["adb", "exec-out", "cat", "/sdcard/ui.xml"], capture_output=True).stdout
        try:
            return ET.fromstring(x)
        except ET.ParseError:
            time.sleep(0.3)
    raise RuntimeError("uiautomator dump failed")


def find(root, key, exact=False):
    for n in root.iter("node"):
        for attr in ("content-desc", "text"):
            v = n.get(attr) or ""
            if (v == key) if exact else (key in v):
                return n
    return None


def bounds(n):
    a, b, c, d = map(int, re.findall(r"\d+", n.get("bounds")))
    return a, b, c, d


def wait(key, timeout=10, gone=False, exact=False):
    end = time.time() + timeout
    while time.time() < end:
        n = find(dump(), key, exact)
        if (n is None) == gone:
            return n
        time.sleep(0.4)
    raise AssertionError(f"{'still there' if gone else 'never appeared'}: {key!r}")


def tap(key, timeout=10, exact=False):
    n = wait(key, timeout, exact=exact)
    a, b, c, d = bounds(n)
    adb("shell", "input", "tap", str((a + c) // 2), str((b + d) // 2))
    time.sleep(0.4)


def type_text(s):
    adb("shell", "input", "text", s.replace(" ", "%s"))


def until(what, f, timeout=10):
    end = time.time() + timeout
    last = None
    while time.time() < end:
        try:
            last = f()
            if last:
                return last
        except Exception as e:  # the server may be mid-change
            last = e
        time.sleep(0.3)
    raise AssertionError(f"{what}: {last!r}")


SHOT = [0]


def shot(name):
    SHOT[0] += 1
    os.makedirs(OUT, exist_ok=True)
    p = os.path.join(OUT, f"{SHOT[0]:02d}-{name}.png")
    with open(p, "wb") as f:
        f.write(subprocess.run(["adb", "exec-out", "screencap", "-p"], capture_output=True).stdout)
    return p


def launch():
    adb("shell", "am", "start", "-n", f"{PKG}/xyz.matm.hive.MainActivity")
    time.sleep(1.5)


def keyboard_up():
    return "mImeShowing=true" in adb("shell", "dumpsys", "window", check=False)


def hide_keyboard():
    """BACK closes the keyboard, but with none up it navigates: only when one stays up."""
    end = time.time() + 1.5
    while keyboard_up() and time.time() < end:  # it may be on its way down already (a sheet closing)
        time.sleep(0.2)
    if not keyboard_up():
        return
    adb("shell", "input", "keyevent", "KEYCODE_BACK")
    end = time.time() + 3
    while keyboard_up() and time.time() < end:
        time.sleep(0.2)
    time.sleep(0.6)  # the layout slides back down: taps before then land off target


# MARK: the steps

def setup(srv):
    srv.start()
    srv.hive("new", "demo", "--cwd", "/tmp", "--", "sh", "-c", "echo hello from hive; exec sh")
    srv.hive("new", "agent", "--cwd", "/tmp", "--", "python3", os.path.join(HERE, "fake-agent.py"))
    adb("logcat", "-b", "crash", "-c")
    adb("shell", "cmd", "statusbar", "collapse")  # a failed run may have left the shade open
    adb("shell", "pm", "clear", PKG)
    adb("shell", "pm", "grant", PKG, "android.permission.POST_NOTIFICATIONS", check=False)
    adb("shell", "settings", "put", "secure", "stylus_handwriting_enabled", "0")


def step_add_host(srv):
    launch()
    wait("Host address")
    shot("add-host")
    tap("Host address")
    type_text(ADDRESS)
    wait(f"Connect to “{ADDRESS}”")
    shot("add-host-typed")
    adb("shell", "input", "keyevent", "KEYCODE_ENTER")
    # It opens the host's first session.
    wait("Switch session", timeout=15)
    until("the phone is a client", lambda: any(c.get("platform") == "android" for c in srv.clients()))
    wait("hello from hive", timeout=10)
    shot("first-session")


def step_add_host_problem(srv):
    open_switcher()
    tap("Add remote host")
    tap("Host address")
    type_text("http://10.0.2.2:4499")
    adb("shell", "input", "keyevent", "KEYCODE_ENTER")
    wait("No hive server answers at 10.0.2.2", timeout=15)
    wait("Tried HTTP on 4499.")
    shot("add-host-problem")
    hide_keyboard()
    tap("Back")
    wait("Filter or create")


def step_switcher(srv):
    open_switcher()
    for s in ("demo", "agent", f"New session on {LABEL}", "Add remote host"):
        wait(s)
    shot("switcher")
    tap("demo", exact=True)
    wait("hello from hive")


def step_type(srv):
    block = srv.view("demo")["windows"][0]["layout"]["block_id"]
    tap("hello from hive")  # the pane: the keyboard comes up
    time.sleep(0.8)
    type_text("echo phone-typed-$((6*7))")
    adb("shell", "input", "keyevent", "KEYCODE_ENTER")
    until("the server ran what the phone typed", lambda: "phone-typed-42\n" in srv.text(block) + "\n")
    wait("phone-typed-42")  # and the phone's mirror shows the output
    shot("typed")
    # The key bar: ctrl latches for the next letter.
    type_text("sleep 30")
    adb("shell", "input", "keyevent", "KEYCODE_ENTER")
    time.sleep(0.5)
    tap("Control C", exact=True)
    until("ctrl+c from the key bar", lambda: srv.text(block).rstrip().endswith("$"))
    type_text("echo abc")
    tap("Control", exact=True)
    type_text("a")  # ctrl+a: to the line's start
    type_text("X")
    adb("shell", "input", "keyevent", "KEYCODE_ENTER")
    until("ctrl latched for one letter", lambda: "Xecho abc" in srv.text(block))
    hide_keyboard()


def clip(text):
    adb("shell", "am", "broadcast", "-n", f"{PKG}/xyz.matm.hive.DebugClip", "--es", "text", f"'{text}'")
    # Android previews a new clipboard over the screen's corner; a touch elsewhere puts it away.
    time.sleep(0.8)
    adb("shell", "input", "tap", "900", "60")
    time.sleep(0.5)


def step_paste(srv):
    block = srv.view("demo")["windows"][0]["layout"]["block_id"]
    hide_keyboard()
    clip("echo pasted-ok")
    tap("Paste", exact=True)
    adb("shell", "input", "keyevent", "KEYCODE_ENTER")
    until("one line pastes at once", lambda: "pasted-ok\n" in srv.text(block) + "\n")
    # Lines without bracketed paste (sh has none) could run: it asks first.
    clip("echo one\necho two")
    tap("Paste", exact=True)
    wait("Paste 2 lines?")
    shot("paste-confirm")
    tap("Cancel", exact=True)
    time.sleep(0.5)
    assert "echo one" not in srv.text(block), "cancelled paste reached the pane"
    tap("Paste", exact=True)
    tap("Paste anyway", exact=True)
    until("the confirmed paste reached the pane", lambda: "\none\n" in srv.text(block) and "echo two" in srv.text(block))
    adb("shell", "input", "keyevent", "KEYCODE_ENTER")
    hide_keyboard()


def step_fit(srv):
    block = srv.view("demo")["windows"][0]["layout"]["block_id"]
    cols0 = srv.view("demo")["blocks"][block]["cols"]
    wait("Zoomed to fit")
    shot("mac-size")
    tap("Fit to phone")
    cols = until("the pane took the phone's size", lambda: (c := srv.view("demo")["blocks"][block]["cols"]) < cols0 and c)
    wait("Give back")  # nobody else held it (the CLI made the pane): no one to name
    wait("Sized by this phone")
    shot(f"fitted-{cols}-cols")
    assert 30 <= cols <= 70, cols
    tap("Give back")
    wait("Fit to phone")
    # Mac size gives the pane its size back at once, not when the Mac is next used.
    until("the pane is back at the Mac's size", lambda: srv.view("demo")["blocks"][block]["cols"] == cols0)
    shot("back-to-mac-size")


MAC_ROOT = os.path.join(ROOT, "apps", "macos")
MAC_APP = os.path.join(MAC_ROOT, "build", "remote-check", "Hive.app")


def mac_cols(srv, block):
    return srv.view("demo")["blocks"][block]["cols"]


def step_two_devices(srv):
    """The Mac app and the phone on one pane: whoever you use holds its size, and the phone says who."""
    if not os.path.isdir(MAC_APP):
        raise AssertionError(f"build the throwaway Mac app first: HIVE_BUNDLE_OUT=build/remote-check script/bundle.sh debug ({MAC_APP})")
    v = srv.view("demo")
    w = next(x for x in v["windows"] if x["window_id"] == v["active_window_id"])
    block = w["focused_block_id"]
    suite = "xyz.matm.hive.e2e"
    sh("defaults", "delete", suite, check=False)
    sh("defaults", "write", suite, "lastSession", v["session_id"])
    log = os.path.join(OUT, "mac.log")
    env = srv.env
    sh("open", "-n", "--env", f"HIVE_SERVER={env['HIVE_SERVER']}", "--env", f"HIVE_CONFIG={env['HIVE_CONFIG']}",
       "--env", f"HIVE_STATE_DIR={env['HIVE_STATE_DIR']}", "--env", f"HIVE_DEFAULTS_SUITE={suite}",
       "--stdout", log, "--stderr", log, MAC_APP)
    def use_mac():
        """As if you typed in the Mac's pane: the debug build takes it as user input (SIGUSR1)."""
        sh("pkill", "-USR1", "-f", MAC_APP + "/Contents/MacOS/Hive")

    try:
        until("the Mac app connected", lambda: any(c.get("platform") == "darwin" for c in srv.clients()), timeout=20)
        # You use the Mac: it takes the pane.
        time.sleep(2)
        use_mac()
        host = srv.call("server.version")["hostname"]
        # The Mac, in front, holds the pane; the phone names it.
        until("the Mac took the pane", lambda: (srv.view("demo")["blocks"][block].get("size_owner") or "") != "", timeout=10)
        mac = mac_cols(srv, block)
        wait(f"Sized by Hive.app on {host}", timeout=10)
        shot("sized-by-mac")
        tap("Fit to phone")
        cols = until("the phone took it", lambda: (c := mac_cols(srv, block)) != mac and c)
        wait("Sized by this phone")
        wait(f"Back to {host}")
        # The Mac reloads (the resize itself, then a label change): the phone keeps it.
        srv.call("session.set_window_label", {"session_id": v["session_id"], "window_id": w["window_id"], "label": "both"})
        time.sleep(5)
        assert mac_cols(srv, block) == cols, f"the Mac took the pane back on its own: {mac_cols(srv, block)} columns"
        shot("phone-holds")
        # Using the Mac again takes it back.
        use_mac()
        until("the Mac took it back when used", lambda: mac_cols(srv, block) == mac, timeout=10)
        wait(f"Sized by Hive.app on {host}", timeout=10)
        shot("mac-took-back")
    finally:
        sh("pkill", "-f", MAC_APP + "/Contents/MacOS/Hive", check=False)
        sh("defaults", "delete", suite, check=False)
        srv.call("session.set_window_label", {"session_id": v["session_id"], "window_id": w["window_id"], "label": ""})


def step_tabs(srv):
    n0 = len(srv.view("demo")["windows"])
    tap("New tab")
    until("a new tab on the server", lambda: len(srv.view("demo")["windows"]) == n0 + 1)
    wait("tab", timeout=5)
    shot("new-tab")
    # Split the new tab: the dots, and a swipe between panes.
    v = srv.view("demo")
    w = v["windows"][-1]
    srv.call("session.new_split", {"session_id": v["session_id"], "block_id": w["layout"]["block_id"], "direction": "horizontal"})
    wait("Pane 2 of 2", timeout=10)
    shot("split-dots")
    tap("Pane 1 of 2")
    tap("Pane 2 of 2")
    shot("second-pane")


def step_agent(srv):
    s = srv.sessions()["agent"]
    v = srv.view("agent")
    block = v["windows"][0]["layout"]["block_id"]
    srv.call("agent.report", {"block_id": block, "harness": "claude", "state": "blocked",
                              "message": "Claude needs your permission to use Bash"})
    open_switcher()
    wait("1 blocked")
    shot("switcher-blocked")
    tap("agent", exact=True)
    wait("Do you want to proceed?")
    wait("Yes, this session")
    shot("ask-card")
    tap("Yes", exact=True)
    until("Yes went to the pane as 1", lambda: "got b'1'" in srv.text(block))
    # Words for the agent: escape, then the text and return.
    srv.call("agent.report", {"block_id": block, "harness": "claude", "state": "blocked", "message": "again"})
    tap("Reply")
    type_text("use the 5m model")
    hide_keyboard()
    shot("reply-typed")
    tap("Send reply")
    until("the reply reached the pane", lambda: "use the 5m model" in srv.text(block) and "\\x1b" in srv.text(block))
    shot("replied")


def hold(key, exact=False):
    """A long press, for a tab's actions."""
    a, b, c, d = bounds(wait(key, exact=exact))
    x, y = (a + c) // 2, (b + d) // 2
    adb("shell", "input", "swipe", str(x), str(y), str(x), str(y), "900")
    time.sleep(0.5)


def pinned(srv, label):
    return {w.get("label"): w.get("pinned", False) for w in srv.view("demo")["windows"]}.get(label)


def mac_shot(srv, name):
    """The throwaway Mac app on the demo session: its window alone (by window ID), for what the phone set to show there too."""
    if not os.path.isdir(MAC_APP):
        raise AssertionError(f"build the throwaway Mac app first: HIVE_BUNDLE_OUT=build/remote-check script/bundle.sh debug ({MAC_APP})")
    winid = os.path.join(MAC_ROOT, "build", "tools", "winid")
    if not os.path.exists(winid):
        sh("swiftc", "-O", os.path.join(MAC_ROOT, "script", "tools", "winid.swift"), "-o", winid)
    suite = "xyz.matm.hive.e2e"
    sh("defaults", "delete", suite, check=False)
    sh("defaults", "write", suite, "lastSession", srv.view("demo")["session_id"])
    env = srv.env
    log = os.path.join(OUT, "mac.log")
    sh("open", "-n", "--env", f"HIVE_SERVER={env['HIVE_SERVER']}", "--env", f"HIVE_CONFIG={env['HIVE_CONFIG']}",
       "--env", f"HIVE_STATE_DIR={env['HIVE_STATE_DIR']}", "--env", f"HIVE_DEFAULTS_SUITE={suite}",
       "--stdout", log, "--stderr", log, MAC_APP)
    try:
        until("the Mac app connected", lambda: any(c.get("platform") == "darwin" for c in srv.clients()), timeout=20)
        time.sleep(3)
        pid = sh("pgrep", "-n", "-f", MAC_APP + "/Contents/MacOS/Hive").strip()
        w = sh(winid, pid).strip()
        p = os.path.join(OUT, f"{SHOT[0]:02d}-{name}.png")
        sh("screencapture", "-x", "-o", "-l", w, p)
        return p
    finally:
        sh("pkill", "-f", MAC_APP + "/Contents/MacOS/Hive", check=False)
        sh("defaults", "delete", suite, check=False)


def step_pins(srv):
    """B Tab sheet: many tabs fold into a count; pins come first, set from a swipe, the strip or the sheet, and reach the server."""
    open_switcher()
    tap("demo", exact=True)
    wait("Switch session")
    v = srv.view("demo")
    sid = v["session_id"]
    names = ["alpha", "beta", "gamma", "delta", "eps", "zeta"]
    made = []
    for n in names:
        # An agent reports only while a program, not the shell, is in front.
        layout = {"cwd": "/tmp", "command": ["python3", os.path.join(HERE, "fake-agent.py")]} if n in ("beta", "gamma") else {"cwd": "/tmp"}
        w = srv.call("session.new_window", {"session_id": sid, "layout": layout})
        srv.call("session.set_window_label", {"session_id": sid, "window_id": w["window_id"], "label": n})
        made.append(w["window_id"])
    # beta works, gamma needs you: cards; the rest are quiet.
    by = {w.get("label"): w for w in srv.view("demo")["windows"]}
    srv.call("agent.report", {"block_id": by["beta"]["layout"]["block_id"], "harness": "claude", "state": "working", "message": "Baking"})
    srv.call("agent.report", {"block_id": by["gamma"]["layout"]["block_id"], "harness": "claude", "state": "blocked", "message": "May I?"})
    n = len(srv.view("demo")["windows"])
    wait(f"All {n} tabs", timeout=10)
    # The strip keeps what needs you; the quiet ones wait in the sheet.
    root = dump()
    assert find(root, "gamma", exact=True) is not None, "gamma (blocked) is not in the strip"
    assert find(root, "zeta", exact=True) is None, "a quiet tab stayed in the strip"
    shot("pins-strip")
    tap(f"All {n} tabs")
    wait("Filter tabs")
    wait("Needs you")
    wait("Working")
    shot("pins-sheet")
    # Swipe the working card right: pinned, here and on the server.
    a, b, c, d = bounds(wait("beta", exact=True))
    y = (b + d) // 2
    x = (a + c) // 2
    adb("shell", "input", "swipe", str(x - 120), str(y), str(x + 260), str(y), "400")
    until("beta pinned on the server", lambda: pinned(srv, "beta") is True)
    wait("beta, pinned")
    wait("Pinned")
    shot("pins-swiped")
    # Hold a quiet tab: Pin tab.
    hold("zeta", exact=True)
    tap("Pin tab")
    until("zeta pinned on the server", lambda: pinned(srv, "zeta") is True)
    # Filter by name.
    tap("Filter tabs")
    type_text("alp")
    wait("alpha", exact=True)
    wait("eps", exact=True, gone=True)
    wait("Needs you", gone=True)
    hide_keyboard()
    shot("pins-filter")
    adb("shell", "input", "keyevent", "KEYCODE_BACK")
    wait("Filter tabs", gone=True)
    # The strip: pins first.
    bx = bounds(wait("beta, pinned"))[0]
    zx = bounds(wait("zeta, pinned"))[0]
    gx = bounds(wait("gamma", exact=True))[0]
    assert bx < gx and zx < gx, f"pins aren't first: beta {bx}, zeta {zx}, gamma {gx}"
    shot("pins-first")
    mac_shot(srv, "pins-mac")
    # Hold a pin in the strip: Unpin tab.
    hold("zeta, pinned")
    wait("Back among the others")
    shot("pins-menu")
    tap("Unpin tab")
    until("zeta unpinned on the server", lambda: pinned(srv, "zeta") is False)
    # Rename from the menu.
    hold("beta, pinned")
    tap("Rename")
    wait("Tab name")
    type_text("bee")
    shot("pins-rename")
    tap("Save the tab name")
    until("beta renamed on the server", lambda: pinned(srv, "bee") is True)
    # Close, asked first.
    hold("gamma", exact=True)
    tap("Close tab")
    wait("Close gamma now")
    shot("pins-close")
    tap("Close gamma now")
    until("gamma closed on the server", lambda: "gamma" not in {w.get("label") for w in srv.view("demo")["windows"]})
    # Leave the session as the other steps know it.
    for w in srv.view("demo")["windows"]:
        if w["window_id"] in made:
            srv.call("session.close_window", {"session_id": sid, "window_id": w["window_id"]})
    wait("All ", gone=True, timeout=10)


def to_desk():
    """Back to the desk (the session), from wherever the last step ended, so each step starts from a known place."""
    for _ in range(4):
        root = dump()
        if find(root, "Switch session") is not None:
            return
        if find(root, "Back") is not None:
            tap("Back")
        else:
            launch()
    wait("Switch session")


def open_switcher():
    """The switcher, from the desk or already there."""
    if find(dump(), "Filter or create") is None:
        tap("Switch session")
    wait("Filter or create")


def open_hosts():
    """From the desk, the switcher, or already there: the Hosts screen."""
    root = dump()
    if find(root, "Scan the code") is not None and find(root, "Stay connected") is not None:
        return  # hive reopens where you left it
    if find(root, "Filter or create") is None:
        tap("Switch session")
    tap("Hosts", exact=True)
    wait("Stay connected")


def stay_connected(on):
    open_hosts()
    # The help line under the switch says which it is.
    is_on = lambda: find(dump(), "In the background too") is not None
    if is_on() != on:
        tap(f"Stay connected to {LABEL}")
    until("the choice took", lambda: is_on() == on)
    tap("Back")
    wait("Filter or create")


def step_notify(srv):
    # Background notifications come from hosts set to stay connected (on-demand ones let go).
    stay_connected(True)
    tap("agent", exact=True)
    v = srv.view("agent")
    block = v["windows"][0]["layout"]["block_id"]
    srv.call("agent.report", {"block_id": block, "harness": "claude", "state": "working"})
    time.sleep(1)
    adb("shell", "input", "keyevent", "KEYCODE_HOME")
    time.sleep(1)
    srv.call("agent.report", {"block_id": block, "harness": "claude", "state": "blocked",
                              "message": "Claude needs your permission to use Bash"})
    until("a notification", lambda: "Claude Code needs you" in adb("shell", "dumpsys", "notification", "--noredact"))
    adb("shell", "cmd", "statusbar", "expand-notifications")
    wait("Claude Code needs you")
    shot("notification")
    tap("Allow")
    until("Allow went to the pane", lambda: srv.text(block).count("got b'1'") >= 2)
    # The agent moved on to another question before the tap: Allow sends nothing.
    srv.call("agent.report", {"block_id": block, "harness": "claude", "state": "working"})
    time.sleep(1)
    srv.call("agent.report", {"block_id": block, "harness": "claude", "state": "blocked", "message": "Claude wants to delete the branch"})
    until("a second notification", lambda: "delete the branch" in adb("shell", "dumpsys", "notification", "--noredact"))
    srv.call("agent.report", {"block_id": block, "harness": "claude", "state": "blocked", "message": "Claude wants to force push"})
    time.sleep(1)
    wait("delete the branch")
    tap("Allow")
    wait("Couldn't answer", timeout=12)
    shot("allow-refused")
    assert srv.text(block).count("got b'1'") == 2, "Allow answered a question it didn't describe"
    adb("shell", "cmd", "statusbar", "collapse")
    launch()
    stay_connected(False)


def background_luma():
    from PIL import Image  # only for this check
    import io
    png = subprocess.run(["adb", "exec-out", "screencap", "-p"], capture_output=True).stdout
    im = Image.open(io.BytesIO(png)).convert("L")
    return im.getpixel((im.width // 2, im.height - 60))


def step_settings(srv):
    open_switcher()
    tap("Settings", exact=True)
    wait("Appearance")
    shot("settings")
    # The theme: Day whatever the system says.
    tap("Day", exact=True)
    until("the app turned Day", lambda: background_luma() > 200)
    shot("settings-day")
    tap("System", exact=True)
    # Notifications: Never means none, even in the background.
    tap("Never", exact=True)
    v = srv.view("agent")
    block = v["windows"][0]["layout"]["block_id"]
    srv.call("agent.report", {"block_id": block, "harness": "claude", "state": "working"})
    adb("shell", "input", "keyevent", "KEYCODE_HOME")
    time.sleep(1)
    srv.call("agent.report", {"block_id": block, "harness": "claude", "state": "blocked", "message": "Claude wants to read secrets.txt"})
    time.sleep(4)
    assert "secrets.txt" not in adb("shell", "dumpsys", "notification", "--noredact"), "notified with notifications set to Never"
    launch()
    tap("When hive is in the background", exact=True)
    # Larger text: fitting gives fewer columns.
    tap("Larger text", exact=True)
    tap("Larger text", exact=True)
    shot("settings-larger")
    tap("Back")  # to the switcher, where Settings was opened
    tap("demo", exact=True)
    wait("Fit to phone")
    d = srv.view("demo")
    w = next(x for x in d["windows"] if x["window_id"] == d["active_window_id"])
    before = d_cols(srv, w)  # the pane's own size: a split's panes are narrower than the Mac's 120
    tap("Fit to phone")
    cols = until("the pane fitted, fewer columns than the default 55", lambda: (c := d_cols(srv, w)) != before and c)
    assert cols < 55, f"larger text still fits {cols} columns"
    shot(f"fitted-larger-{cols}-cols")
    tap("Give back")
    until("Mac size again", lambda: d_cols(srv, w) == before)
    # Panes open fitted: and Mac size still gives the size back.
    open_switcher()
    tap("Settings", exact=True)
    tap("Open panes fitted to the phone")
    tap("Back")
    tap("agent", exact=True)
    wait("Give back")  # it opened fitted
    open_switcher()
    tap("demo", exact=True)
    until("the pane opened fitted", lambda: d_cols(srv, w) != before)
    tap("Give back")
    until("Mac size gives an auto-fitted pane its size back", lambda: d_cols(srv, w) == before)
    open_switcher()
    tap("Settings", exact=True)
    tap("Open panes fitted to the phone")
    tap("Back")


def d_cols(srv, w):
    v = srv.view("demo")
    return min(v["blocks"][b]["cols"] for b in v["blocks"] if v["blocks"][b]["window_id"] == w["window_id"])


def phone_connected(srv):
    return any(c.get("platform") == "android" for c in srv.clients())


def ongoing():
    return "Connected to" in adb("shell", "dumpsys", "notification", "--noredact")


def step_leave(srv):
    # On demand (the default): leaving the app lets go, after a grace for app switching; nothing stays behind.
    assert phone_connected(srv)
    adb("shell", "input", "keyevent", "KEYCODE_HOME")
    time.sleep(5)
    assert phone_connected(srv), "let go at once: switching apps for a moment shouldn't drop it"
    until("the phone let go of an on-demand host", lambda: not phone_connected(srv), timeout=45)
    assert not ongoing(), "a background notification with nothing kept"
    launch()
    until("back in the app, it reconnects", lambda: phone_connected(srv), timeout=15)
    # Stay connected: kept in the background, with a Disconnect on its notification.
    open_hosts()
    tap(f"Stay connected to {LABEL}")
    shot("stay-connected")
    adb("shell", "input", "keyevent", "KEYCODE_HOME")
    time.sleep(40)
    assert phone_connected(srv), "a host set to stay connected let go"
    adb("shell", "cmd", "statusbar", "expand-notifications")
    n = wait("Connected to 1 host")
    # Silent notifications come collapsed: expand this one for its button.
    y = bounds(n)[1]
    exp = min((e for e in dump().iter("node") if (e.get("content-desc") or "") == "Expand"), key=lambda e: abs(bounds(e)[1] - y))
    a, b, c, d = bounds(exp)
    adb("shell", "input", "tap", str((a + c) // 2), str((b + d) // 2))
    shot("ongoing")
    tap("Disconnect", exact=True)
    until("Disconnect let go", lambda: not phone_connected(srv), timeout=10)
    assert not ongoing(), "the ongoing notification stayed after Disconnect"
    adb("shell", "cmd", "statusbar", "collapse")
    launch()
    until("opening hive connects again", lambda: phone_connected(srv), timeout=15)
    stay_connected(False)


def step_hosts(srv):
    """The Hosts screen: every host command, checked where it counts."""
    open_hosts()
    wait("Connected, hive")
    shot("hosts")
    tap(f"Disconnect from {LABEL}")
    until("Disconnect let go", lambda: not phone_connected(srv))
    wait("Connects when you pick it")
    # The desk says so, with the way back.
    tap("Back")
    tap("Back")
    wait(f"Disconnected from {LABEL}")
    shot("disconnected-desk")
    tap("Connect", exact=True)
    until("Connect from the desk connected", lambda: phone_connected(srv))
    wait(f"Disconnected from {LABEL}", gone=True)
    # A host kept in the background, disconnected by hand: no notification claims it's connected.
    stay_connected(True)
    open_hosts()
    tap(f"Disconnect from {LABEL}")
    until("Disconnect let go", lambda: not phone_connected(srv))
    adb("shell", "input", "keyevent", "KEYCODE_HOME")
    time.sleep(3)
    assert not ongoing(), "Connected to 1 host, for a host disconnected by hand"
    launch()
    open_hosts()
    tap(f"Connect to {LABEL}")
    until("Connect connected", lambda: phone_connected(srv))
    stay_connected(False)
    open_hosts()
    # Rename: the switcher follows; a bad name is refused.
    tap(f"Rename {LABEL}")
    wait("New name")
    type_text("box.local")  # the old name comes selected: typing replaces it
    wait("reads as a host name")
    shot("rename-refused")
    adb("shell", "input", "keycombination", "113", "29")  # ctrl+a
    type_text("studio")
    tap("Save the name")
    wait("Host studio")
    hide_keyboard()
    tap("Back")
    wait("studio", exact=True)
    shot("renamed")
    open_hosts()
    tap("Rename studio")
    wait("New name")
    type_text(LABEL)
    tap("Save the name")
    hide_keyboard()
    wait(f"Host {LABEL}")
    # Forget, with a confirmation: one of the code's hosts.
    tap("Forget box")
    wait("Forget box?")
    shot("forget-confirm")
    tap("Forget box now")
    wait("Host box", gone=True)
    tap("Back")
    wait("Filter or create")
    assert find(dump(), "box", exact=True) is None, "a forgotten host is still in the switcher"


def hosts_code(hosts):
    """The Mac's code (HiveKit/HostsCode.swift): sorted keys, base64url without padding."""
    import base64
    d = json.dumps({"hosts": [{"address": a, "label": l} for l, a in hosts], "v": 1}, separators=(",", ":"), sort_keys=True)
    return "hive://hosts?d=" + base64.urlsafe_b64encode(d.encode()).decode().rstrip("=")


def step_code(srv):
    # What a scan of the Mac's code opens: every host in plain view; one off
    # the tailnet, and one changing a saved address, start unchecked.
    code = hosts_code([("box", "https://box.example.ts.net"), ("lab", "http://100.101.2.3:4483"),
                       ("far", "http://203.0.113.9:4483"), (LABEL, "http://10.0.2.2:9999"),
                       ("studio", f"http://10.0.2.2:{PORT + 1}")])
    adb("shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", f'"{code}"', PKG)
    wait("Hosts from your Mac")
    wait("Not a tailnet address")
    wait("Replaces " + ADDRESS)
    wait(f"Same server as {LABEL}", timeout=15)  # asked who it is: it's one you have, by another address
    wait("Add 2 hosts")
    shot("code-offer")
    tap("Add 2 hosts")
    wait("Filter or create")
    for s in ("box", "lab"):
        wait(s, exact=True)
    assert find(dump(), "far", exact=True) is None, "a host off the tailnet was added unasked"
    shot("code-added")
    # The saved host kept its own address.
    tap(f"New session on {LABEL}")
    wait("Fit to phone", timeout=10)


def step_away(srv):
    open_switcher()
    tap("demo", exact=True)
    wait("Fit to phone")  # the desk, streaming a pane
    srv.stop()
    wait(f"Reconnecting to {LABEL}", timeout=15, exact=True)
    wait("hive keeps trying")
    shot("away")
    # The same sessions come back with the server (its state dir keeps them).
    srv.start()
    wait("Reconnecting to", timeout=30, gone=True)
    wait("Switch session")
    shot("back")


STEPS = [step_add_host, step_add_host_problem, step_switcher, step_type, step_paste, step_fit, step_two_devices, step_tabs, step_agent, step_notify, step_settings, step_leave, step_code, step_hosts, step_pins, step_away]


def main():
    keep = "--keep" in sys.argv
    # Night or Day for the whole run (the system's mode): screenshots of both themes.
    adb("shell", "cmd", "uimode", "night", "yes" if "--night" in sys.argv else "no")
    only = sys.argv[sys.argv.index("--only") + 1:] if "--only" in sys.argv else None
    shutil.rmtree(OUT, ignore_errors=True)
    srv = Server()
    failed = []
    try:
        setup(srv)
        for step in STEPS:
            name = step.__name__.removeprefix("step_")
            if only and name not in only and name != "add_host":
                continue
            if PKG == "xyz.matm.hive" and step is step_paste:
                print("skip  paste (debug builds only)")
                continue
            t0 = time.time()
            try:
                if step is not step_add_host:
                    to_desk()
                step(srv)
                # A crash fails the step even when the UI came back.
                crash = adb("logcat", "-d", "-b", "crash", check=False)
                if PKG in crash:
                    raise AssertionError("the app crashed:\n" + "\n".join(crash.strip().splitlines()[:12]))
                print(f"ok    {name} ({time.time() - t0:.1f}s)")
            except Exception as e:
                failed.append(name)
                p = shot(f"FAILED-{name}")
                print(f"FAIL  {name}: {e}\n      {p}")
                break
    finally:
        if not keep:
            srv.stop()
            shutil.rmtree(srv.dir, ignore_errors=True)
        else:
            print("server kept:", srv.env["HIVE_SERVER"])
    print(f"screenshots: {OUT}")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    signal.signal(signal.SIGINT, lambda *_: sys.exit(130))
    main()
