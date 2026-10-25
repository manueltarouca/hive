package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/paths"
)

// fakeShell writes a "shell" that does pre, then runs its -c command (the 4th argument).
func fakeShell(t *testing.T, pre string) string {
	p := filepath.Join(t.TempDir(), "sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+pre+"\neval \"$4\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestShellPath(t *testing.T) {
	t.Setenv("PATH", "/a/b:/c d/e")
	for name, pre := range map[string]string{
		"plain": "",
		// Dotfiles print (a prompt, a banner) around the command's output.
		"noisy": "echo 'Last login: today'; printf '\\033[?2004h'",
		// A helper the dotfiles start keeps the output open: still answers in time.
		"helper": "/bin/sleep 30 &",
	} {
		start := time.Now()
		got, err := shellPath(fakeShell(t, pre), 2*time.Second)
		if err != nil || got != "/a/b:/c d/e" {
			t.Errorf("%s: shellPath = %q, %v", name, got, err)
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("%s: took %v", name, d)
		}
	}
	// A shell that never finishes (a prompt waiting for input) is given up on.
	start := time.Now()
	if got, err := shellPath(fakeShell(t, "/bin/sleep 30"), time.Second); err == nil {
		t.Errorf("hung shell: shellPath = %q, want an error", got)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("hung shell: took %v", d)
	}
}

func TestAgentServes(t *testing.T) {
	for _, k := range []string{"HIVE_SERVER", "HIVE_STATE_DIR", "HIVE_CONFIG", "HIVE_SCRIPTS", "XDG_RUNTIME_DIR", "XDG_STATE_HOME", "XDG_CONFIG_HOME", "HIVE_AGENT_LABEL"} {
		t.Setenv(k, "")
	}
	t.Setenv("TMPDIR", "")
	def := paths.Socket()
	if !agentServes(def, nil, nil) {
		t.Error("the default server isn't the background item's")
	}
	if agentServes(def, []string{"--tailscale"}, nil) {
		t.Error("a server with options went to the background item")
	}
	if agentServes("/tmp/other.sock", nil, nil) {
		t.Error("another socket went to the background item")
	}
	t.Setenv("TMPDIR", t.TempDir())
	if agentServes(paths.Socket(), nil, nil) {
		t.Error("a TMPDIR other than launchd's went to the background item")
	}
	t.Setenv("TMPDIR", "")
	t.Setenv("HIVE_STATE_DIR", "/tmp/s")
	if agentServes(def, nil, nil) {
		t.Error("another state dir went to the background item")
	}
}
