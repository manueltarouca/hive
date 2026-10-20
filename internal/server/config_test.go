package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func startWithConfig(t *testing.T, state, conf string, resume func(string, string) []string) *harness {
	t.Helper()
	return startCfg(t, state, func(c *Config) {
		c.ConfigPath, c.ConfigPoll = conf, 50*time.Millisecond
		if resume != nil {
			c.ResumeArgv = resume
		}
	})
}

// The server reads its settings file: scrollback-limit applies to every
// terminal, config.set writes the file, and edits are picked up.
func TestServerConfig(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(conf, []byte("# mine\nscrollback-limit = 64k\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := startWithConfig(t, t.TempDir(), conf, nil)
	defer h.stop()
	got := h.call("config.get", nil)
	if v := got["values"].(map[string]any); v["scrollback-limit"] != float64(64<<10) {
		t.Fatalf("config.get = %v", got)
	}
	created := h.call("session.create", map[string]any{"command": []string{"sh", "-c",
		`i=0; while [ $i -lt 3000 ]; do echo "line $i xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"; i=$((i+1)); done; echo filled; exec cat`}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.waitScreen(sid, bid, "filled")
	out := h.call("hive.terminal.format", map[string]any{"block_id": bid})
	text, _ := out["content"].(string)
	if text == "" {
		t.Fatalf("format = %v", out)
	}
	if strings.Contains(text, "line 0 ") || !strings.Contains(text, "line 2999 ") {
		t.Fatalf("a 64k limit kept the first lines (or lost the last)")
	}

	// config.set: the file changes in place, comments kept, and applies.
	h.call("config.set", map[string]any{"key": "scrollback-limit", "value": "1m"})
	b, _ := os.ReadFile(conf)
	if string(b) != "# mine\nscrollback-limit = 1m\n" {
		t.Fatalf("file after config.set = %q", b)
	}
	if v := h.call("config.get", nil)["values"].(map[string]any); v["scrollback-limit"] != float64(1<<20) {
		t.Fatalf("after set: %v", v)
	}

	// A person's edit is picked up.
	if err := os.WriteFile(conf, []byte("scrollback-limit = 2m\nnonsense = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := h.call("config.get", nil)
		if got["values"].(map[string]any)["scrollback-limit"] == float64(2<<20) {
			if d := got["diagnostics"].([]any); len(d) != 1 {
				t.Fatalf("diagnostics = %v", d)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the edit was never picked up")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// resume-agents = false: after a restart, agents aren't resumed.
func TestResumeAgentsOff(t *testing.T) {
	state, conf := t.TempDir(), filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(conf, []byte("resume-agents = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resume := func(h, id string) []string {
		return []string{"sh", "-c", fmt.Sprintf("echo resumed %s %s; exec cat", h, id)}
	}
	h := startWithConfig(t, state, conf, resume)
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle", "session_id": "s-1"})
	h.stop()
	h2 := startWithConfig(t, state, conf, resume)
	defer h2.stop()
	view := h2.call("session.view", map[string]any{"session_id": sid})
	b := view["blocks"].(map[string]any)[bid].(map[string]any)
	if a, _ := b["agent"].(map[string]any); a["state"] != "ended" {
		t.Fatalf("agent = %v, want ended (not resumed)", a)
	}
}

// Without a config path the server uses the defaults and reads no file.
func TestNoConfigPath(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	got := h.call("config.get", nil)
	if got["path"] != "" || got["values"].(map[string]any)["scrollback-limit"] != float64(16<<20) {
		t.Fatalf("config.get = %v", got)
	}
}

// A listener in the settings file is added at start, however the server was
// started (the Mac app starts it without options).
func TestListenFromConfig(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(conf, []byte("listen = 127.0.0.1:0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := startWithConfig(t, t.TempDir(), conf, nil)
	defer h.stop()
	v := h.call("server.version", nil)
	ls, _ := v["listeners"].([]any)
	if len(ls) == 0 {
		t.Fatalf("listeners = %v, want the one from the settings file", v["listeners"])
	}
}
