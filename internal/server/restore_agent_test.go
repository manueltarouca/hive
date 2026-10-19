package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/block"
	"github.com/manueltarouca/hive/internal/persist"
)

// An agent is resumed after a restart only if it was still running: one
// killed without its SessionEnd hook leaves the pane at a shell prompt,
// and resuming would reopen a session the user closed.
func TestRestoreResumesOnlyRunningAgents(t *testing.T) {
	resume := func(h, id string) []string { return []string{h, "--resume", id} }
	agent := &persist.Agent{Harness: "claude", SessionID: "s1", State: "idle"}
	cases := []struct {
		name string
		fg   *block.Process
		want []string
	}{
		{"agent in front", &block.Process{Name: "2.1.302", Program: "claude"}, []string{"claude", "--resume", "s1"}},
		{"older state, no program", &block.Process{Name: "2.1.302"}, []string{"claude", "--resume", "s1"}},
		{"nothing recorded", nil, []string{"claude", "--resume", "s1"}},
		{"back at zsh", &block.Process{Name: "zsh", Program: "zsh"}, nil},
		{"older state at a login shell", &block.Process{Name: "-bash"}, nil},
	}
	for _, c := range cases {
		st := &persist.BlockState{Agent: agent, Foreground: c.fg}
		got, _ := restoreCommand(st, resume)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: argv = %v, want %v", c.name, got, c.want)
		}
	}
}

// State saved by an older server can hold an agent still "idle" in a pane
// that was back at its shell (it was killed). It isn't resumed, and it
// comes back ended, not idle.
func TestRestoreEndsAgentsItDoesNotResume(t *testing.T) {
	state := t.TempDir()
	resume := func(h, id string) []string {
		return []string{"sh", "-c", "echo resumed " + h + " " + id + "; exec cat"}
	}
	h := start(t, state, resume)
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)
	bid := strs(created["block_ids"])[0]
	h.stop()

	store := persist.New(state)
	saved, err := store.LoadState()
	if err != nil || saved == nil {
		t.Fatalf("state: %v %v", saved, err)
	}
	saved.Blocks[bid].Agent = &persist.Agent{Harness: "claude", SessionID: "old-1", State: "idle"}
	saved.Blocks[bid].Foreground = &block.Process{PID: 1, Name: "zsh"} // as an older server saved it
	if err := store.SaveState(saved); err != nil {
		t.Fatal(err)
	}

	h2 := start(t, state, resume)
	defer h2.stop()
	view := h2.call("session.view", map[string]any{"session_id": sid})
	b := view["blocks"].(map[string]any)[bid].(map[string]any)
	if a, _ := b["agent"].(map[string]any); a["state"] != "ended" {
		t.Fatalf("agent after restore = %v, want ended", a)
	}
}

// A resumed agent runs in the user's shell, as one started by hand: quit
// it, and the pane is at a prompt in the same place, not a closed program.
func TestResumedAgentLeavesAShell(t *testing.T) {
	state := t.TempDir()
	resume := func(h, id string) []string { return []string{"sh", "-c", "echo resumed " + id + "; exit 0"} }
	h := start(t, state, resume)
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle", "session_id": "s-9"})
	h.stop()

	h2 := start(t, state, resume)
	defer h2.stop()
	h2.waitScreen(sid, bid, "resumed s-9")
	// The agent quit: the shell under it answers.
	h2.call("hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{"data": "echo $((40+2))\r"}})
	h2.waitScreen(sid, bid, "\n42")
}

// A resume that fails leaves the shell in front: once the shell has had
// its time to start the agent, the agent is ended, not idle forever.
func TestFailedResumeEndsAgent(t *testing.T) {
	defer func(g time.Duration) { resumeGrace = g }(resumeGrace)
	resumeGrace = 1500 * time.Millisecond
	state := t.TempDir()
	// Gone at once: the shell that ran it is in front from the start, and
	// stays (exec keeps its process), so nothing in front changes later.
	resume := func(string, string) []string { return []string{"false"} }
	h := start(t, state, resume)
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle", "session_id": "s-1"})
	h.stop()
	h2 := start(t, state, resume)
	defer h2.stop()
	deadline := time.Now().Add(5 * time.Second)
	for {
		view := h2.call("session.view", map[string]any{"session_id": sid})
		a, _ := view["blocks"].(map[string]any)[bid].(map[string]any)["agent"].(map[string]any)
		if a["state"] == "ended" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent after a failed resume = %v", a)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Restarted again while the shell is still starting the agent: the shell
// in front then isn't the agent gone, so it's resumed again.
func TestRestartWhileResumingResumesAgain(t *testing.T) {
	state := t.TempDir()
	// The shell never gets to it before the next restart.
	resume := func(h, id string) []string { return []string{"sh", "-c", "sleep 30; exec sh"} }
	h := start(t, state, resume)
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle", "session_id": "s-2"})
	h.stop()
	h2 := start(t, state, resume)
	time.Sleep(1200 * time.Millisecond) // the shell, then sh, in front and saved
	h2.stop()
	h3 := start(t, state, resume)
	defer h3.stop()
	view := h3.call("session.view", map[string]any{"session_id": sid})
	a, _ := view["blocks"].(map[string]any)[bid].(map[string]any)["agent"].(map[string]any)
	if a["state"] == "ended" {
		t.Fatalf("agent after a second restart = %v", a)
	}
}

// The agent took the terminal: past the grace it's still running, and
// when it quits the pane is at a prompt and the agent ended.
func TestResumedAgentRunsPastTheGrace(t *testing.T) {
	defer func(g time.Duration) { resumeGrace = g }(resumeGrace)
	resumeGrace = 800 * time.Millisecond
	// A program the server takes for claude: cat, by that name.
	bin := filepath.Join(t.TempDir(), "claude")
	data, err := os.ReadFile("/bin/cat")
	if err != nil {
		t.Skip(err)
	}
	if err := os.WriteFile(bin, data, 0o755); err != nil {
		t.Fatal(err)
	}
	// macOS kills a copied system binary until it's signed again.
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("codesign", "-s", "-", "-f", bin).CombinedOutput(); err != nil {
			t.Skipf("codesign: %v %s", err, out)
		}
	}
	state := t.TempDir()
	resume := func(string, string) []string { return []string{bin} }
	h := start(t, state, resume)
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle", "session_id": "s-3"})
	h.stop()
	h2 := start(t, state, resume)
	defer h2.stop()
	agent := func() string {
		view := h2.call("session.view", map[string]any{"session_id": sid})
		a, _ := view["blocks"].(map[string]any)[bid].(map[string]any)["agent"].(map[string]any)
		s, _ := a["state"].(string)
		return s
	}
	time.Sleep(resumeGrace + 700*time.Millisecond)
	if s := agent(); s == "ended" {
		t.Fatal("a running agent was ended at the end of the grace")
	}
	// It quits (end of input): the agent ends, the shell answers.
	h2.call("hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{"data": "\x04"}})
	for deadline := time.Now().Add(5 * time.Second); agent() != "ended"; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("agent after it quit = %q", agent())
		}
	}
	h2.call("hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{"data": "echo $((40+2))\r"}})
	h2.waitScreen(sid, bid, "\n42")
}

// Shells that don't take the wrapper's command line run the agent alone.
func TestInShellOnlyForShellsThatTakeIt(t *testing.T) {
	for _, sh := range []string{"/bin/zsh", "/opt/homebrew/bin/fish", "/bin/bash"} {
		t.Setenv("SHELL", sh)
		if got, ok := inShell([]string{"claude", "--resume", "it's"}); !ok || got[0] != sh ||
			got[len(got)-1] != `'claude' '--resume' 'it'\''s'; exec '`+sh+`' -l` {
			t.Errorf("%s: %q %v", sh, got, ok)
		}
	}
	for _, sh := range []string{"/opt/homebrew/bin/nu", "/bin/tcsh", "/bin/csh"} {
		t.Setenv("SHELL", sh)
		if _, ok := inShell([]string{"claude"}); ok {
			t.Errorf("%s: wrapped", sh)
		}
	}
}
