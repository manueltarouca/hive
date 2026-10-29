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
                        HIVE_CONFIG=f"{self.dir}/config", HIVE_SCRIPTS=f"{self.dir}/scripts", HIVE_SESSION="", HIVE_BLOCK="")

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

def dump(windows=False):
    """The UI as a tree. windows: every window, popups too (Android's floating text toolbar is one)."""
    for _ in range(5):
        adb("shell", "uiautomator", "dump", *(["--windows"] if windows else []), "/sdcard/ui.xml", check=False)
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

def quiet_other_devices():
    """The app on any other emulator would reconnect to this throwaway server (same address and port)
    and take panes as a second device: stopped for the run."""
    me = os.environ.get("ANDROID_SERIAL") or adb("get-serialno").strip()
    for line in sh("adb", "devices").splitlines()[1:]:
        serial = line.split("\t")[0].strip()
        if serial and serial != me and line.endswith("device"):
            for pkg in ("xyz.matm.hive", "xyz.matm.hive.debug"):
                sh("adb", "-s", serial, "shell", "am", "force-stop", pkg, check=False)


def setup(srv):
    quiet_other_devices()
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
    p = shot("first-session")
    # The terminal's own background, in either theme: nothing over it (Android's focus veil greyed it once).
    from PIL import Image
    term = next(n for n in dump().iter("node") if "hello from hive" in (n.get("content-desc") or ""))
    a, b, c, d = bounds(term)
    px = Image.open(p).convert("RGB").getpixel(((a + c) // 2, d - 40))
    want = (0x28, 0x2C, 0x34) if "--night" in sys.argv else (0xFD, 0xFB, 0xF7)
    assert all(abs(x - y) <= 3 for x, y in zip(px, want)), f"the pane's background is {px}, not the theme's {want}"


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
    # A tap on the pane only aims: it never brings up the keyboard.
    tap("hello from hive")
    time.sleep(1)
    assert not keyboard_up(), "a tap on the pane brought up the keyboard"
    tap("Keyboard", exact=True)  # the key bar's keyboard key does
    until("the keyboard", keyboard_up)
    wait("Hide keyboard", exact=True)  # and the key says what it does now
    time.sleep(0.5)
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
    # Nobody held the pane (the CLI made it): the phone took it on opening, as the device in use.
    wait("Give back")  # no one to name
    wait("Sized by this phone")
    cols = srv.view("demo")["blocks"][block]["cols"]
    assert 30 <= cols <= 70, cols
    shot(f"fitted-{cols}-cols")
    tap("Give back")
    wait("Fit to phone")
    # Giving back returns the size it had at once, not when another device is next used.
    cols0 = until("the pane is back at its own size", lambda: (c := srv.view("demo")["blocks"][block]["cols"]) > cols and c)
    wait("Zoomed to fit")
    shot("mac-size")
    # Given back, it stays given back: leaving and coming back doesn't take it again.
    open_switcher()
    tap("agent", exact=True)
    wait("Switch session")
    open_switcher()
    tap("demo", exact=True)
    wait("Fit to phone")
    time.sleep(1.5)
    assert srv.view("demo")["blocks"][block]["cols"] == cols0, "a pane given back was taken again on reopening"
    tap("Fit to phone")
    until("the pane took the phone's size again", lambda: srv.view("demo")["blocks"][block]["cols"] == cols)
    tap("Give back")
    until("and gave it back", lambda: srv.view("demo")["blocks"][block]["cols"] == cols0)
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


def step_stopped(srv):
    """An agent that failed and stopped (Claude Code at its usage limit) reads Stopped, with why: not done."""
    import base64
    msg = base64.b64encode("You've hit your session limit, resets 3am".encode()).decode()
    srv.hive("new", "limit", "--cwd", "/tmp", "--", "sh", "-c",
             f"sleep 3; printf '\\033]7501;state=error:app=claude-code:msg={msg}\\033\\\\'; exec cat")
    block = srv.view("limit")["windows"][0]["layout"]["block_id"]
    srv.call("agent.report", {"block_id": block, "harness": "claude", "state": "working"})
    until("the server says stopped", lambda: srv.view("limit")["blocks"][block].get("agent", {}).get("kind") == "error", timeout=15)
    open_switcher()
    wait("1 stopped")
    shot("switcher-stopped")
    tap("limit", exact=True)
    wait("Claude Code stopped")
    wait("Stopped", exact=True)
    wait("resets 3am")
    shot("agent-stopped")


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


def streaming(timeout=10):
    """The desk shows a pane: its size button is there, whichever it says."""
    until("a pane on the desk", lambda: (r := dump()) is not None and (find(r, "Fit to phone") is not None or find(r, "Give back") is not None), timeout=timeout)


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
    # Under every session: off screen when there are many.
    scroll_to("Hosts")
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


def step_scripts(srv):
    """Scripts' badges on the pane header, and a script's notification whose button runs its action."""
    open_switcher()
    tap("agent", exact=True)
    block = srv.view("agent")["windows"][0]["layout"]["block_id"]
    for b in ({"id": "tests", "state": "error", "msg": "2 failing"}, {"id": "ci", "state": "working", "msg": "CI", "progress": 40},
              {"id": "branch", "state": "idle", "msg": "attach-resize-sequencing-with-a-long-name"}):
        srv.call("status.set", dict(b, target=block))
    wait("2 failing")
    wait("CI 40%")
    wait("+1")
    shot("badges")
    srv.call("status.clear", {"target": block})
    wait("2 failing", gone=True)
    # A real script: an action, and a notification offering it.
    code = os.path.join(srv.dir, "ping.ts")
    with open(code, "w") as f:
        f.write(f"""
let runs = 0;
h.actions.register({{ name: "ping", title: "Ping", run: async () => {{
  runs++;
  await h.status.set({{ target: "{block}", id: "ping", state: "done", msg: "pinged " + runs }});
}} }});
await h.notify.send({{ title: "Tests failed in api", body: "2 failures", target: "{block}", level: "error",
  actions: [{{ title: "Ping", action: "ping", step: "Puts a badge on the pane" }}] }});
""")
    run = subprocess.Popen([HIVE, "script", "run", "--allow", "ui", "--for", "90s", code], env=srv.env,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        adb("shell", "input", "keyevent", "KEYCODE_HOME")
        until("the script's notification", lambda: "Tests failed in api" in adb("shell", "dumpsys", "notification", "--noredact"))
        adb("shell", "cmd", "statusbar", "expand-notifications")
        wait("Tests failed in api")
        if find(dump(), "Ping", exact=True) is None:  # collapsed: its Expand control shows the button
            root = dump()
            card = bounds(find(root, "Tests failed in api"))
            exp = [n for n in root.iter("node") if (n.get("content-desc") or "") == "Expand" and abs(bounds(n)[1] - card[1]) < 150]
            if exp:
                a, b, c, d = bounds(exp[0])
                adb("shell", "input", "tap", str((a + c) // 2), str((b + d) // 2))
        wait("Ping", exact=True)
        shot("script-notification")
        tap("Ping", exact=True)
        until("the action ran", lambda: any(x["id"] == "ping" for x in srv.call("status.list", {"target": block})["badges"]))
        adb("shell", "cmd", "statusbar", "collapse")
        launch()
        wait("pinged 1")
        shot("action-badge")
        # The same action from the switcher, under its host.
        open_switcher()
        wait("Action Ping")
        shot("switcher-action")
        tap("Action Ping")
        wait("pinged 2")
    finally:
        run.terminate()
        run.wait(timeout=10)
        adb("shell", "cmd", "statusbar", "collapse")


TYPIST = """
export default hive.script({
  name: "typist",
  description: "Watches every agent's terminal for a usage limit and, once it resets, types a short message asking the agent to go on where it left off. Turn it off here any time.",
  permissions: ["screen", "input"],
  options: { message: { title: "What to type", type: "text", default: "go on" } },
  setup(h) {},
});
"""


def scripts_state(srv):
    return {s["name"]: s for s in srv.call("script.list")["scripts"]}


def option(srv, name, key):
    return next(o for o in scripts_state(srv)[name]["options"] if o["key"] == key)["value"]


def scroll_to(key, times=8, back=False):
    """Swipes (mid-screen, away from the edges) until [key] is on screen: up when it's below or not there yet (back: down), down when above."""
    for _ in range(times):
        n = find(dump(), key)
        if n is not None:
            top, bottom = bounds(n)[1], bounds(n)[3]
            if bottom < 2250 and top > 260 and bottom - top > 20:  # on screen, not under the bars
                return
            if bottom <= 260:
                adb("shell", "input", "swipe", "540", "900", "540", "1500", "300")
                time.sleep(0.6)
                continue
        if back:
            adb("shell", "input", "swipe", "540", "900", "540", "1600", "300")
        else:
            adb("shell", "input", "swipe", "540", "1700", "540", "1000", "300")
        time.sleep(0.6)
    wait(key)


def clear_field():
    """Empties the focused field: to its end, then deletes. Select-all (ctrl+a) sometimes didn't take,
    and the typed text went in beside the old value."""
    adb("shell", "input", "keyevent", "KEYCODE_MOVE_END")
    adb("shell", "input", "keyevent", *(["KEYCODE_DEL"] * 8))


def tap_on_screen(key):
    scroll_to(key)
    tap(key)


def open_scripts():
    """From the desk: Settings, then this host's scripts."""
    open_switcher()
    tap("Settings", exact=True)
    wait("Appearance")
    scroll_to(f"Scripts on {LABEL}")  # the Scripts section sits below the fold
    tap(f"Scripts on {LABEL}")


def step_script_page(srv):
    """The Scripts screen: empty, can't read, then scripts to allow, turn off, watch, and set; a chip when one acts."""
    folder = srv.env["HIVE_SCRIPTS"]
    os.makedirs(folder, exist_ok=True)
    open_scripts()
    wait("No scripts on this server")
    shot("scripts-empty")
    # The server goes: the screen says it couldn't read them, and reads them when it's back.
    srv.stop()
    wait("Couldn't read the scripts", timeout=15)
    shot("scripts-error")
    srv.start()
    wait("No scripts on this server", timeout=30)
    # Two examples (they ask for ui) and one that asks to read and type; one allowed from here.
    for f in ("digest.ts", "needs-you.ts"):
        shutil.copy(os.path.join(ROOT, "examples", "scripts", f), folder)
    with open(os.path.join(folder, "typist.ts"), "w") as f:
        f.write(TYPIST)
    srv.call("script.reload")
    until("three scripts, pending", lambda: len(scripts_state(srv)) == 3 and all(x["state"] == "pending" for x in scripts_state(srv).values()), timeout=20)
    srv.call("script.grant", {"name": "needs-you"})
    until("needs-you running", lambda: scripts_state(srv)["needs-you"]["state"] == "running")
    tap("Back")
    wait(f"Scripts on {LABEL}")  # where you left Settings
    scroll_to(f"Scripts on {LABEL}")
    until("the Settings row counts them", lambda: find(dump(), "1 running, 2 waiting for you") is not None, timeout=10)
    shot("settings-scripts")
    tap(f"Scripts on {LABEL}")
    wait("digest", exact=True)
    wait("Waiting for your OK")
    wait("It asks to notify you, mark panes and bring hive to the front.")
    shot("scripts-pending")
    # Allow digest.
    tap("Allow digest")
    until("digest allowed", lambda: scripts_state(srv)["digest"]["state"] == "running")
    wait("Allow digest", gone=True)
    # Off and on again.
    tap_on_screen("needs-you on")
    until("needs-you off", lambda: scripts_state(srv)["needs-you"]["state"] == "disabled")
    wait("Turned off")
    shot("scripts-off")
    tap_on_screen("needs-you on")
    until("needs-you on again", lambda: scripts_state(srv)["needs-you"]["state"] == "running")
    # Watch only.
    tap_on_screen("Watch only, needs-you")
    until("needs-you watches", lambda: scripts_state(srv)["needs-you"]["mode"] == "watch")
    wait("Running, watch only")
    scroll_to("set to watch only by you")
    # A long number label keeps one line, on its own: the − and + sit below it.
    scroll_to("Remind me after this many minutes")
    root = dump()
    label = find(root, "Remind me after this many minutes", exact=True)
    plus = find(root, "More, Remind me after this many minutes")
    assert label is not None and plus is not None, "the option's label or its + is missing"
    assert bounds(label)[3] - bounds(label)[1] < 70, f"the option's label wraps: {bounds(label)}"
    assert bounds(label)[3] <= bounds(plus)[1], "the option's label shares a line with its + button"
    # An option: + once, then typed, then one out of range.
    tap_on_screen("More, Remind me after this many minutes")
    until("remind_min 11", lambda: option(srv, "needs-you", "remind_min") == 11)
    tap_on_screen("Remind me after this many minutes, needs-you")
    until("the keyboard", keyboard_up)
    clear_field()
    type_text("30")
    time.sleep(0.8)
    shot("scripts-keyboard")
    adb("shell", "input", "keyevent", "KEYCODE_ENTER")  # Done
    until("remind_min 30", lambda: option(srv, "needs-you", "remind_min") == 30)
    tap_on_screen("Remind me after this many minutes, needs-you")
    clear_field()
    type_text("999")
    adb("shell", "input", "keyevent", "KEYCODE_ENTER")
    wait("At most 240.")
    assert option(srv, "needs-you", "remind_min") == 30, "an out of range value was sent"
    sets = [e for e in srv.call("audit.list", {"name": "needs-you"})["entries"] if e.get("summary") == "remind_min = 30"]
    assert len(sets) == 1, f"remind_min = 30 was sent {len(sets)} times"
    hide_keyboard()
    # Text: typed, saved on Done.
    scroll_to("Send it each day at (HH:MM, empty: never), digest", back=True)
    tap("Send it each day at (HH:MM, empty: never), digest")
    until("the keyboard", keyboard_up)
    type_text("08:30")
    time.sleep(0.8)
    shot("scripts-text-keyboard")
    adb("shell", "input", "keyevent", "KEYCODE_ENTER")
    until("morning 08:30", lambda: option(srv, "digest", "morning") == "08:30")
    hide_keyboard()
    tap_on_screen("Also agents that finished, needs-you")
    until("include_done on", lambda: option(srv, "needs-you", "include_done") is True)
    shot("scripts-options")
    # The one that asks to read and type, still pending, says so plainly.
    for _ in range(6):
        if find(dump(), "It can read your terminals and type into them") is not None:
            break
        adb("shell", "input", "swipe", "540", "1900", "540", "700", "300")
        time.sleep(0.6)
    wait("It can read your terminals and type into them")
    adb("shell", "input", "swipe", "540", "1500", "540", "900", "300")  # the whole card in view
    time.sleep(0.6)
    shot("scripts-typist")
    tap("Back")
    # Back in Settings where you left it, not at the top: the first section is out of view.
    wait("Notifications")
    root = dump()
    top = find(root, "Appearance", exact=True)
    assert top is None or bounds(top)[3] <= 260, "Settings scrolled back to the top"
    n = find(root, f"Scripts on {LABEL}")
    assert n is not None and 260 < bounds(n)[1] and bounds(n)[3] < 2250, "the Scripts row isn't in view"
    tap("Back")
    # A script acts on the pane on screen: a chip says so, and opens the scripts.
    srv.hive("new", "acts", "--cwd", "/tmp", "--", "sh")  # one pane: the one on screen
    open_switcher()
    tap("acts", exact=True)
    wait("Give back")  # nobody held it: the phone took it on opening
    block = srv.view("acts")["windows"][0]["layout"]["block_id"]
    rows = srv.view("acts")["blocks"][block]["rows"]
    time.sleep(1)  # the shell's prompt
    srv.hive("script", "run", "--allow", "input", "-e", f'const b = await h.block("{block}"); await b.write("echo acted\\n");')
    until("the script typed", lambda: "acted" in srv.text(block))
    wait("typed")
    shot("scripts-chip")
    root = dump()
    # The chip's own node (its description starts with the script), not the terminal's text.
    chip = next(n for n in root.iter("node") if (n.get("content-desc") or "").startswith("script typed"))
    a, b, c, d = bounds(chip)
    # In the pane's header, above the terminal's text: it covers no line, and the pane keeps its rows.
    term = next(n for n in root.iter("node") if "echo acted" in (n.get("content-desc") or ""))
    assert d <= bounds(term)[1], f"the chip {bounds(chip)} is over the terminal {bounds(term)}"
    assert srv.view("acts")["blocks"][block]["rows"] == rows, "the chip resized the pane"
    adb("shell", "input", "tap", str((a + c) // 2), str((b + d) // 2))
    wait("Recent activity")
    tap("Back")
    wait("Switch session")
    wait("typed", gone=True, timeout=14)  # it goes after about 10 seconds


def typed_line(srv, block):
    """What the fake Claude pane shows in its input box: the keys it got, by name."""
    for line in srv.text(block).splitlines():
        if line.startswith("> "):
            return line[2:]
    return ""


def press(key, ms):
    """Holds a key on the bar for [ms]."""
    a, b, c, d = bounds(wait(key, exact=True))
    adb("shell", "input", "swipe", str((a + c) // 2), str((b + d) // 2), str((a + c) // 2), str((b + d) // 2), str(ms))


def step_keys(srv):
    """The key bar: arrows (held, they repeat), shift+tab, ctrl and alt latches, More; a hardware keyboard, with no keyboard on screen."""
    srv.hive("new", "keys", "--cwd", "/tmp", "--", "python3", os.path.join(HERE, "fake-claude-long.py"))
    open_switcher()
    tap("keys", exact=True)
    streaming()
    block = srv.view("keys")["windows"][0]["layout"]["block_id"]
    until("the pane drew", lambda: "accept edits" in srv.text(block))
    hide_keyboard()
    for name in ("Up", "Down", "Left", "Right"):
        tap(name, exact=True)
    until("the arrows", lambda: typed_line(srv, block) == "[↑][↓][←][→]")
    tap("Shift Tab", exact=True)
    until("shift+tab", lambda: typed_line(srv, block).endswith("[⇧Tab]"))
    tap("Control C", exact=True)  # the fake clears its line on ^C
    until("ctrl+c", lambda: typed_line(srv, block) == "")
    # Held: the arrow repeats.
    press("Up", 1300)
    time.sleep(0.5)
    n = typed_line(srv, block).count("↑") + typed_line(srv, block).count("⎋[A")
    assert n >= 6, f"a held arrow sent {n}"
    tap("Control C", exact=True)
    until("cleared", lambda: typed_line(srv, block) == "")
    # ctrl once: for the next key only. Typed from the bar's / then a letter from the keyboard.
    tap("Control", exact=True)
    wait("Control, on for the next key", exact=True)
    shot("ctrl-once")
    adb("shell", "input", "text", "r")
    until("ctrl+r", lambda: typed_line(srv, block) == "[^R]")
    adb("shell", "input", "text", "r")
    until("then a plain r", lambda: typed_line(srv, block) == "[^R]r")
    # alt: escape first, as terminals send it.
    tap("Alt", exact=True)
    adb("shell", "input", "text", "b")
    until("alt+b", lambda: typed_line(srv, block).endswith("[⎋b]"))
    # ctrl locked: a second tap at once; it stays for every key until tapped off.
    a_, b_, c_, d_ = bounds(wait("Control", exact=True))
    x_, y_ = (a_ + c_) // 2, (b_ + d_) // 2
    adb("shell", f"input tap {x_} {y_}; input tap {x_} {y_}")  # two taps in a row, as a thumb does
    wait("Control, locked", exact=True)
    shot("ctrl-locked")
    adb("shell", "input", "text", "a")
    adb("shell", "input", "text", "e")
    until("ctrl+a then ctrl+e", lambda: typed_line(srv, block).endswith("[^A][^E]"))
    tap("Control, locked", exact=True)
    wait("Control", exact=True)
    tap("Control C", exact=True)
    until("cleared", lambda: typed_line(srv, block) == "")
    # More: the keys the bar has no room for, open for several in a row.
    tap("More keys", exact=True)
    wait("Control D, end of input", exact=True)  # Control first, Claude Code's own among them
    shot("more-keys")
    tap_on_screen("F5")  # the panel scrolls to the function keys
    tap_on_screen("Page up")
    until("F5 and page up", lambda: typed_line(srv, block) == "[⎋[15~][PgUp]")
    tap("Fewer keys", exact=True)
    wait("Control D, end of input", exact=True, gone=True)
    assert not keyboard_up(), "a key brought up the keyboard"
    # A hardware keyboard types into the pane with no keyboard on screen.
    tap("Control C", exact=True)
    until("cleared", lambda: typed_line(srv, block) == "")
    adb("shell", "input", "keyevent", "KEYCODE_ESCAPE")
    adb("shell", "input", "keycombination", "113", "46")  # ctrl+r
    adb("shell", "input", "keyevent", "KEYCODE_MOVE_HOME")
    adb("shell", "input", "text", "hw")
    until("hardware keys", lambda: typed_line(srv, block) == "[Esc][^R][Home]hw")
    time.sleep(0.5)
    assert not keyboard_up(), "a hardware key brought up the on-screen keyboard"


def clipboard():
    out = adb("shell", "am", "broadcast", "-n", f"{PKG}/xyz.matm.hive.DebugClip")
    m = re.search(r'data="(.*)"', out, re.S)
    return m.group(1) if m else ""


def step_select(srv):
    """A long press selects the word under the finger; Copy puts it on the clipboard; a tap lets go."""
    if "keys" not in srv.sessions():
        srv.hive("new", "keys", "--cwd", "/tmp", "--", "python3", os.path.join(HERE, "fake-claude-long.py"))
    open_switcher()
    tap("keys", exact=True)
    streaming()
    block = srv.view("keys")["windows"][0]["layout"]["block_id"]
    time.sleep(1)
    root = dump()
    term = next(n for n in root.iter("node") if "accept edits" in (n.get("content-desc") or ""))
    a, b, c, d = bounds(term)
    lines = term.get("content-desc").split("\n")
    cols = srv.view("keys")["blocks"][block]["cols"]
    rows = srv.view("keys")["blocks"][block]["rows"]
    density = int(adb("shell", "wm", "density").split(":")[-1].strip()) / 160
    pad = round(6 * density)
    cw = (c - a - 2 * pad) / cols
    ch = (d - b - pad) / rows  # at least the cell height; whole rows fit
    # A word of letters on a row near the top: the first one of five or more.
    for r, line in enumerate(lines[:6]):
        m = re.search(r"[A-Za-z]{5,}", line)
        if m:
            break
    else:
        raise AssertionError(f"no word to select in {lines[:6]}")
    x = int(a + pad + (m.start() + 2.5) * cw)
    y = int(b + pad / 2 + (r + 0.5) * ch)
    adb("shell", "input", "swipe", str(x), str(y), str(x), str(y), "900")  # a long press
    copy = until("the Copy button over the selection", lambda: [n for n in [find(dump(windows=True), "Copy", exact=True)] if n is not None], timeout=5)[0]
    shot("selected")
    x1, y1, x2, y2 = bounds(copy)
    adb("shell", "input", "tap", str((x1 + x2) // 2), str((y1 + y2) // 2))
    # The word as the app reads it: up to spaces, brackets, quotes; dots and slashes inside (paths stay whole).
    brk = set(" \t()[]{}<>'\"`,;│|─")
    a0, b0 = m.start(), m.end() - 1
    while a0 > 0 and line[a0 - 1] not in brk:
        a0 -= 1
    while b0 < len(line) - 1 and line[b0 + 1] not in brk:
        b0 += 1
    while b0 > a0 and line[b0] in ".:!?":
        b0 -= 1
    word = line[a0:b0 + 1]
    until(f"{word!r} on the clipboard", lambda: clipboard() == word, timeout=5)
    time.sleep(0.5)
    assert find(dump(windows=True), "Copy", exact=True) is None, "the Copy bar stayed after copying"
    assert not keyboard_up(), "selecting brought up the keyboard"


def pane_tap(srv, name, marker, token, nth=0):
    """Taps the middle of `token` (the nth time it shows) in session `name`'s pane, found by `marker` in its text."""
    block = srv.view(name)["windows"][0]["layout"]["block_id"]
    info = srv.view(name)["blocks"][block]
    root = dump()
    term = next(n for n in root.iter("node") if marker in (n.get("content-desc") or ""))
    a, b, c, d = bounds(term)
    lines = term.get("content-desc").split("\n")
    density = int(adb("shell", "wm", "density").split(":")[-1].strip()) / 160
    pad = round(6 * density)
    cw = (c - a - 2 * pad) / info["cols"]
    ch = (d - b - pad) / info["rows"]
    hits = [(r, m.start()) for r, line in enumerate(lines) for m in re.finditer(re.escape(token), line)]
    assert len(hits) > nth, f"{token!r} not on screen: {lines[:12]}"
    r, col = hits[nth]
    x = int(a + pad + (col + len(token) / 2) * cw)
    y = int(b + pad / 2 + (r + 0.5) * ch)
    adb("shell", "input", "tap", str(x), str(y))


def close_viewer():
    tap("Close", exact=True)
    until("the viewer closed", lambda: find(dump(), "Open in another app", exact=True) is None and find(dump(), "Getting it from", exact=False) is None, timeout=5)


def step_files(srv):
    """Tapping a path a program printed opens it: an image, text at a line, the end of a big log, a
    folder and a file in it, a PDF through Open in…, a missing one, and a path wrapped across rows."""
    d = os.path.join(srv.dir, "files-" + "a-rather-long-folder-name-so-paths-wrap")
    os.makedirs(os.path.join(d, "sub"), exist_ok=True)
    import shutil
    shutil.copy(os.path.join(HERE, "..", "build", "e2e", sorted(f for f in os.listdir(os.path.join(HERE, "..", "build", "e2e")) if f.endswith(".png"))[0]), os.path.join(d, "shot.png"))
    with open(os.path.join(d, "notes.txt"), "w") as f:
        f.write("first line\nsecond line\nthird line, the one asked for\nfourth\n")
    with open(os.path.join(d, "big.log"), "w") as f:
        for i in range(100000):
            f.write(f"{i:06d} request served in {i % 97} ms\n")
        f.write("the very last line\n")
    with open(os.path.join(d, "doc.pdf"), "wb") as f:
        f.write(b"%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj << >> endobj\ntrailer << >>\n%%EOF\n")
    with open(os.path.join(d, "sub", "inner.txt"), "w") as f:
        f.write("inside the folder\n")
    script = ("printf 'files ready\\nSaved shot.png\\nSee notes.txt:3\\nLog big.log\\nDoc doc.pdf\\nDir sub/\\nGone missing.txt\\nAlso ~/surely-not-there.txt\\n"
              + d + "/notes.txt\\n'; exec cat")
    srv.hive("new", "files", "--cwd", d, "--", "sh", "-c", script)
    open_switcher()
    tap("files", exact=True)
    streaming()
    time.sleep(1.5)

    pane_tap(srv, "files", "files ready", "shot.png")
    until("the image", lambda: find(dump(), "PNG image", exact=True) is not None, timeout=15)
    shot("file-image")
    close_viewer()

    pane_tap(srv, "files", "files ready", "notes.txt:3")
    until("the text", lambda: find(dump(), "4 lines", exact=True) is not None, timeout=15)
    assert find(dump(), "third line, the one asked for", exact=True) is not None, "the line asked for isn't in view"
    shot("file-text")
    close_viewer()

    pane_tap(srv, "files", "files ready", "big.log")
    tap("Show the last 2 MB", timeout=15)
    until("the end of the log", lambda: find(dump(), "the very last line", exact=True) is not None, timeout=15)
    assert find(dump(), "the last 2 MB", exact=True) is not None
    shot("file-tail")
    close_viewer()

    pane_tap(srv, "files", "files ready", "sub/")
    tap("inner.txt", timeout=15)
    until("a file of the folder", lambda: find(dump(), "inside the folder", exact=True) is not None, timeout=15)
    tap("Back to the folder", exact=True)
    until("the folder again", lambda: find(dump(), "inner.txt", exact=True) is not None, timeout=5)
    shot("file-folder")
    close_viewer()

    # Only looked like a path: nothing opens (a tap to focus or aim lands on such words).
    pane_tap(srv, "files", "files ready", "missing.txt")
    time.sleep(3)
    assert find(dump(), "Close", exact=True) is None, "a viewer opened for a word that isn't a file"
    # An explicit path that's gone says so.
    pane_tap(srv, "files", "files ready", "surely-not-there.txt")
    until("not found", lambda: find(dump(), "Not on ", exact=False) is not None, timeout=15)
    shot("file-missing")
    tap("Close", exact=True)

    # The absolute path, longer than the pane is wide: tapped on its second row, still the whole path.
    pane_tap(srv, "files", "files ready", "notes.txt", nth=1)
    until("the wrapped path's file", lambda: find(dump(), "4 lines", exact=True) is not None, timeout=15)
    close_viewer()

    pane_tap(srv, "files", "files ready", "doc.pdf")
    until("the PDF's card", lambda: find(dump(), "PDF document", exact=False) is not None, timeout=15)
    shot("file-other")
    adb("logcat", "-c")
    tap("Open in…", timeout=15)
    # The chooser, or (one app for PDFs) that app at once, given hive's file read-only.
    until("an app asked to view the PDF", lambda: [l for l in adb("logcat", "-d").splitlines()
        if "act=android.intent.action.CHOOSER" in l or ("act=android.intent.action.VIEW" in l and f"content://{PKG}.files" in l)], timeout=15)
    time.sleep(2)
    shot("file-open-in")
    adb("shell", "am", "start", "-n", f"{PKG}/xyz.matm.hive.MainActivity")
    time.sleep(1.5)
    if find(dump(), "Open in another app", exact=True) is not None:
        close_viewer()
    assert not keyboard_up(), "tapping paths brought up the keyboard"


def step_previews(srv):
    """The tab sheet previews what agents did, not their input box and status bar; their glyphs draw."""
    srv.hive("new", "look", "--cwd", "/tmp", "--", "python3", os.path.join(HERE, "fake-claude-screen.py"), "bypass")
    sid = srv.sessions()["look"]["session_id"]
    for kind in ("manual", "picker"):
        w = srv.call("session.new_window", {"session_id": sid, "layout": {"cwd": "/tmp", "command": ["python3", os.path.join(HERE, "fake-claude-screen.py"), kind]}})
        srv.call("session.set_window_label", {"session_id": sid, "window_id": w["window_id"], "label": kind})
    time.sleep(1)
    for w, state in zip(srv.view("look")["windows"], ("working", "done", "blocked")):
        srv.call("agent.report", {"block_id": w["layout"]["block_id"], "harness": "claude", "state": state, "message": "Which database?" if state == "blocked" else ""})
    for i in range(4):  # quiet tabs: enough that the strip folds into a count
        srv.call("session.new_window", {"session_id": sid, "layout": {"cwd": "/tmp"}})
    open_switcher()
    tap("look", exact=True)
    wait("Switch session")
    tap("All 7 tabs", timeout=10)
    wait("Filter tabs")
    wait("Updated docs/scripts.md")
    wait("Looking at the screenshot")
    wait("Postgres")
    root = dump()
    for chrome in ("bypass permissions", "manual mode on", "Enter to select"):
        # Only the sheet: the pane behind it still shows its own status bar.
        sheet_top = bounds(find(root, "Filter tabs"))[1]
        for n in root.iter("node"):
            if chrome in (n.get("text") or "") and bounds(n)[1] > sheet_top:
                raise AssertionError(f"a card previews {chrome!r}")
    shot("previews")
    tap("Updated docs/scripts.md")  # its card opens it: the pane draws its own status bar, ⏵⏵ included
    wait("Filter tabs", gone=True)
    time.sleep(1.5)
    shot("claude-pane")


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
    streaming()
    if find(dump(), "Give back") is not None:  # fitted on opening (run alone): start from its own size
        tap("Give back")
    wait("Fit to phone")
    d = srv.view("demo")
    w = next(x for x in d["windows"] if x["window_id"] == d["active_window_id"])
    cols = lambda: {b: v["cols"] for b, v in srv.view("demo")["blocks"].items() if v["window_id"] == w["window_id"]}
    before = cols()  # each pane's own size (a split's other pane may be fitted already)
    tap("Fit to phone")
    changed = until("the pane fitted", lambda: [b for b, c in cols().items() if c != before[b]])
    b = changed[0]
    assert cols()[b] < 55, f"larger text still fits {cols()[b]} columns"
    shot(f"fitted-larger-{cols()[b]}-cols")
    tap("Give back")
    until("its own size again", lambda: cols()[b] == before[b])
    # Panes open fitted: and giving back still gives the size back.
    open_switcher()
    tap("Settings", exact=True)
    tap("Open every pane fitted to the phone")
    tap("Back")
    tap("agent", exact=True)
    wait("Give back")  # it opened fitted
    open_switcher()
    tap("demo", exact=True)
    until("the pane opened fitted", lambda: cols()[b] != before[b])
    tap("Give back")
    until("giving back an opened-fitted pane returns its size", lambda: cols()[b] == before[b])
    open_switcher()
    tap("Settings", exact=True)
    tap("Open every pane fitted to the phone")
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
    streaming(timeout=10)


def step_away(srv):
    open_switcher()
    tap("demo", exact=True)
    streaming()  # the desk, streaming a pane
    srv.stop()
    wait(f"Reconnecting to {LABEL}", timeout=15, exact=True)
    wait("hive keeps trying")
    shot("away")
    # The same sessions come back with the server (its state dir keeps them).
    srv.start()
    wait("Reconnecting to", timeout=30, gone=True)
    wait("Switch session")
    shot("back")


STEPS = [step_add_host, step_add_host_problem, step_switcher, step_type, step_paste, step_fit, step_two_devices, step_tabs, step_agent, step_stopped, step_notify, step_scripts, step_script_page, step_keys, step_select, step_files, step_previews, step_settings, step_leave, step_code, step_hosts, step_pins, step_away]


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
            if PKG == "xyz.matm.hive" and step in (step_paste, step_select):
                print(f"skip  {name} (debug builds only: it needs the clipboard hook)")
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
