#!/usr/bin/env python3
"""End to end on an emulator: the debug app against a throwaway hive server.

The server listens on a plain TCP port for the emulator (10.0.2.2 is the
Mac), with its own state and config dirs: never the user's server. Every
step drives the real UI (uiautomator, adb input) and checks the result on
the server, where it counts. Screenshots go to build/e2e.

    python3 script/e2e.py [--night] [--keep] [--only NAME...]
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
PKG = "xyz.matm.hive.debug"
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
        self.hive("server", "start", "--extra-listen", f"127.0.0.1:{PORT}", "--allow-anonymous")

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
    """BACK closes the keyboard, but on the desk with none up it leaves the app: only when one is up."""
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
    tap("Switch session")
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
    wait("Filter or create")
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
    cols0 = srv.call("session.view", {"session_id": srv.sessions()["demo"]["session_id"]})["blocks"][block]["cols"]
    wait("This pane is your Mac's size")
    shot("mac-size")
    tap("Fit to phone")
    cols = until("the pane took the phone's size", lambda: (c := srv.view("demo")["blocks"][block]["cols"]) < cols0 and c)
    wait("Mac size")
    wait("This pane is your Mac's size", gone=True)
    shot(f"fitted-{cols}-cols")
    assert 30 <= cols <= 70, cols
    tap("Mac size")
    wait("Fit to phone")


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
    tap("Switch session")
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


def step_notify(srv):
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


def hosts_code(hosts):
    """The Mac's code (HiveKit/HostsCode.swift): sorted keys, base64url without padding."""
    import base64
    d = json.dumps({"hosts": [{"address": a, "label": l} for l, a in hosts], "v": 1}, separators=(",", ":"), sort_keys=True)
    return "hive://hosts?d=" + base64.urlsafe_b64encode(d.encode()).decode().rstrip("=")


def step_code(srv):
    # What a scan of the Mac's code opens: every host in plain view; one off
    # the tailnet, and one changing a saved address, start unchecked.
    code = hosts_code([("box", "https://box.example.ts.net"), ("lab", "http://100.101.2.3:4483"),
                       ("far", "http://203.0.113.9:4483"), (LABEL, "http://10.0.2.2:9999")])
    adb("shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", f'"{code}"', PKG)
    wait("Hosts from your Mac")
    wait("Not a tailnet address")
    wait("Replaces " + ADDRESS)
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
    tap("Switch session")
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


STEPS = [step_add_host, step_add_host_problem, step_switcher, step_type, step_paste, step_fit, step_tabs, step_agent, step_notify, step_code, step_away]


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
            t0 = time.time()
            try:
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
