package server

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A "claude" report from a pane running another agent is that agent's:
// grok runs Claude Code's hooks. Resuming grok's session with claude fails.
func TestAgentReportNamesTheRunningAgent(t *testing.T) {
	if os.Getenv("HIVE_TEST_SLEEPER") == "1" {
		time.Sleep(30 * time.Second) // the helper process: just be grok for a while
		os.Exit(0)
	}
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
	if os.Getenv("HIVE_TEST_SLEEPER") == "1" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"/bin/zsh", "-f"}})
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
	waitUntil("zsh", func() bool { return program() == "zsh" })
	h.call("hive.terminal.write", map[string]any{"block_id": bid,
		"args": map[string]any{"data": "HIVE_TEST_SLEEPER=1 " + self + " -test.run=^TestAgentEndsWhenTheShellReturns$\r"}})
	waitUntil("the agent in front", func() bool { p := program(); return p != "zsh" && p != "" })
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "working", "session_id": "s-1"})
	if s := state(); s != "working" {
		t.Fatalf("state = %q, want working", s)
	}
	h.call("hive.terminal.send_key", map[string]any{"block_id": bid, "args": map[string]any{"keys": []string{"ctrl+c"}}})
	waitUntil("zsh again", func() bool { return program() == "zsh" })
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
	waitUntil("the agent in front again", func() bool { p := program(); return p != "zsh" && p != "" })
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
