package server

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// helperNamed copies this test binary under a name, to run as a program
// called that (HIVE_TEST_SLEEPER=1 makes it just wait).
func helperNamed(t *testing.T, name string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), name)
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(bin, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	dst.Close()
	return bin
}

func init() {
	if os.Getenv("HIVE_TEST_SLEEPER") == "1" {
		time.Sleep(60 * time.Second)
		os.Exit(0)
	}
}

// A "claude" report from a pane running another agent is that agent's:
// grok runs Claude Code's hooks. Resuming grok's session with claude fails.
func TestAgentReportNamesTheRunningAgent(t *testing.T) {
	// A process named like grok's release binary: this test binary, copied.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "grok-1.0.62-macos-aarch64")
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.OpenFile(bin, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	src.Close()
	dst.Close()

	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"/usr/bin/env", "HIVE_TEST_SLEEPER=1", bin,
		"-test.run=^TestAgentReportNamesTheRunningAgent$"}})
	bid := strs(created["block_ids"])[0]
	deadline := time.Now().Add(5 * time.Second)
	for {
		p := h.call("hive.terminal.process", map[string]any{"block_id": bid})
		if fg, _ := p["foreground"].(map[string]any); fg != nil && fg["program"] == "grok" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("grok never in the foreground: %v", p)
		}
		time.Sleep(50 * time.Millisecond)
	}
	out := h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle",
		"session_id": "01a14b1e-1122-7a50-a819-084bfc5e3bbf"})
	if out["harness"] != "grok" {
		t.Fatalf("harness = %v, want grok", out["harness"])
	}
}

// An agent killed before its SessionEnd hook leaves the pane at the shell:
// the server sees the shell come back and ends the agent, so it isn't shown
// or resumed. A later report from the agent brings it back.
func TestAgentEndsWhenTheShellReturns(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	sh, shName := testShell(t)
	created := h.call("session.create", map[string]any{"command": sh})
	bid := strs(created["block_ids"])[0]
	program := func() string {
		p := h.call("hive.terminal.process", map[string]any{"block_id": bid})
		fg, _ := p["foreground"].(map[string]any)
		s, _ := fg["program"].(string)
		return s
	}
	waitUntil := func(what string, ok func() bool) {
		deadline := time.Now().Add(5 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	sid, _ := created["session_id"].(string)
	state := func() string { // the pane's record; agent.list leaves ended agents out
		out := h.call("session.view", map[string]any{"session_id": sid})
		b, _ := out["blocks"].(map[string]any)[bid].(map[string]any)
		a, _ := b["agent"].(map[string]any)
		s, _ := a["state"].(string)
		return s
	}
	waitUntil(shName, func() bool { return program() == shName })
	h.call("hive.terminal.write", map[string]any{"block_id": bid,
		"args": map[string]any{"data": "HIVE_TEST_SLEEPER=1 " + self + " -test.run=^TestAgentEndsWhenTheShellReturns$\r"}})
	waitUntil("the agent in front", func() bool { p := program(); return p != shName && p != "" })
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "working", "session_id": "s-1"})
	if s := state(); s != "working" {
		t.Fatalf("state = %q, want working", s)
	}
	h.call("hive.terminal.send_key", map[string]any{"block_id": bid, "args": map[string]any{"keys": []string{"ctrl+c"}}})
	waitUntil(shName+" again", func() bool { return program() == shName })
	waitUntil("the agent ended", func() bool { return state() == "ended" })
	// A late report while the shell is in front (a hook finishing after the
	// agent exited) doesn't bring it back.
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "done", "session_id": "s-1"})
	if s := state(); s != "ended" {
		t.Fatalf("late report at the shell: state = %q, want ended", s)
	}
	// The agent in front again, reporting, is back.
	h.call("hive.terminal.write", map[string]any{"block_id": bid,
		"args": map[string]any{"data": "HIVE_TEST_SLEEPER=1 " + self + " -test.run=^TestAgentEndsWhenTheShellReturns$\r"}})
	waitUntil("the agent in front again", func() bool { p := program(); return p != shName && p != "" })
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle", "session_id": "s-1"})
	if s := state(); s != "idle" {
		t.Fatalf("after a new report, state = %q, want idle", s)
	}
}

// Interrupting Claude Code runs no hook, so the agent stayed "working".
// Claude reports its own state (OSC 7501) at every change, interrupts too:
// the server follows it.
func TestAgentFollowsItsOwnStatus(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"sh", "-c",
		`sleep 0.6; printf '\033]7501;state=working:app=claude-code\033\\'; sleep 0.6; printf '\033]7501;state=idle:app=claude-code\033\\'; exec cat`}})
	bid := strs(created["block_ids"])[0]
	sid, _ := created["session_id"].(string)
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "working", "session_id": "s-osc"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		out := h.call("session.view", map[string]any{"session_id": sid})
		b, _ := out["blocks"].(map[string]any)[bid].(map[string]any)
		a, _ := b["agent"].(map[string]any)
		if a["state"] == "idle" {
			if a["session_id"] != "s-osc" {
				t.Fatalf("session id lost: %v", a)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent never followed its status to idle: %v", a)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The agent ended because its shell came back returns when the same agent
// is in front again: a tool can run an interactive shell (zsh -i) that takes
// the terminal for a while.
func TestAgentEndedByTheShellComesBack(t *testing.T) {
	claude := helperNamed(t, "claude")
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	sh, shName := testShell(t)
	created := h.call("session.create", map[string]any{"command": sh})
	bid := strs(created["block_ids"])[0]
	sid, _ := created["session_id"].(string)
	program := func() string {
		p := h.call("hive.terminal.process", map[string]any{"block_id": bid})
		fg, _ := p["foreground"].(map[string]any)
		s, _ := fg["program"].(string)
		return s
	}
	state := func() string {
		out := h.call("session.view", map[string]any{"session_id": sid})
		b, _ := out["blocks"].(map[string]any)[bid].(map[string]any)
		a, _ := b["agent"].(map[string]any)
		s, _ := a["state"].(string)
		return s
	}
	waitUntil := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitUntil(shName, func() bool { return program() == shName })
	h.call("hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{"data": "HIVE_TEST_SLEEPER=1 " + claude + "\r"}})
	waitUntil("claude in front", func() bool { return program() == "claude" })
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "working", "session_id": "s-1"})
	// "A tool's interactive shell" takes the terminal: here, ctrl-z.
	h.call("hive.terminal.send_key", map[string]any{"block_id": bid, "args": map[string]any{"keys": []string{"ctrl+z"}}})
	waitUntil("the agent ended", func() bool { return state() == "ended" })
	h.call("hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{"data": "fg\r"}})
	waitUntil("claude back in front", func() bool { return program() == "claude" })
	waitUntil("the agent working again", func() bool { return state() == "working" })
}

// An agent that another agent runs as a tool (grok -p inside Claude) gets
// the pane's HIVE_BLOCK, and its hooks report. Only the agent holding the
// pane, or another process of its program, may change the pane's agent.
func TestNestedAgentReportsAreIgnored(t *testing.T) {
	claude := helperNamed(t, "claude")
	nested := exec.Command(helperNamed(t, "grok-1.0.62-macos-aarch64"))
	nested.Env = append(os.Environ(), "HIVE_TEST_SLEEPER=1")
	if err := nested.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = nested.Process.Kill(); _ = nested.Wait() }()
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"/usr/bin/env", "HIVE_TEST_SLEEPER=1", claude}})
	bid := strs(created["block_ids"])[0]
	var fgPID float64
	deadline := time.Now().Add(5 * time.Second)
	for {
		p := h.call("hive.terminal.process", map[string]any{"block_id": bid})
		fg, _ := p["foreground"].(map[string]any)
		if fg["program"] == "claude" {
			fgPID = fg["pid"].(float64)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("claude never in front")
		}
		time.Sleep(50 * time.Millisecond)
	}
	out := h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "working",
		"session_id": "host-1", "agent_pid": int(fgPID)})
	if out["session_id"] != "host-1" {
		t.Fatalf("the pane's own agent was refused: %v", out)
	}
	out = h.call("agent.report", map[string]any{"block_id": bid, "harness": "grok", "state": "ended",
		"session_id": "child-9", "agent_pid": nested.Process.Pid})
	if out["session_id"] != "host-1" || out["state"] != "working" || out["harness"] != "claude" {
		t.Fatalf("a nested agent changed the pane's agent: %v", out)
	}
	// The same agent run as a tool (claude -p inside Claude) has no hold on
	// the pane's terminal: ignored too.
	inner := exec.Command(claude)
	inner.Env = append(os.Environ(), "HIVE_TEST_SLEEPER=1")
	if err := inner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inner.Process.Kill(); _ = inner.Wait() }()
	out = h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "done",
		"session_id": "child-10", "agent_pid": inner.Process.Pid})
	if out["session_id"] != "host-1" {
		t.Fatalf("a nested claude changed the pane's agent: %v", out)
	}
	// A report from something that isn't an agent (a script, a test, a
	// daemon) is taken as before.
	out = h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle",
		"session_id": "host-1", "agent_pid": os.Getpid()})
	if out["state"] != "idle" {
		t.Fatalf("a report from a non-agent was refused: %v", out)
	}
}

// testShell is an interactive shell without the user's rc files: zsh where
// it's installed (macOS), else bash (most Linux).
func testShell(t *testing.T) ([]string, string) {
	if _, err := os.Stat("/bin/zsh"); err == nil {
		return []string{"/bin/zsh", "-f"}, "zsh"
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return []string{p, "--norc", "--noprofile"}, "bash"
	}
	t.Skip("no zsh or bash")
	return nil, ""
}
