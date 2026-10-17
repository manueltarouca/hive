package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEndToEnd drives the real binary: a session, an agent reported through
// its hook, a server restart, and the agent resumed in the restored layout.
func TestEndToEnd(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hive")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	dir, err := os.MkdirTemp("/tmp", "hive-e2e.")
	if err != nil {
		t.Fatal(err)
	}
	env := append(testEnv(dir), `HIVE_RESUME_CLAUDE=["sh","-c","echo resumed-{id}; exec cat"]`)
	hive := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("hive %v: %v", args, err)
		}
		return string(out), code
	}
	must := func(args ...string) string {
		t.Helper()
		out, code := hive(args...)
		if code != 0 {
			t.Fatalf("hive %v: exit %d\n%s", args, code, out)
		}
		return out
	}
	eventually := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if cond() {
				return
			}
		}
		t.Fatalf("timed out: %s", what)
	}
	defer hive("server", "stop")

	must("new", "e2e", "--cwd", dir, "--", "cat")
	payload := `{"session_id":"abc-123","cwd":"` + dir + `","hook_event_name":"SessionStart","source":"startup"}`
	must("-s", "e2e", "split", "--label", "agent", "--keep-open", "--", "sh", "-c",
		fmt.Sprintf("printf '%%s' '%s' | hive agent hook claude; echo hooked; exec cat", payload))
	eventually("hook ran", func() bool { out, _ := hive("-b", "agent", "capture"); return strings.Contains(out, "hooked") })

	var agents struct {
		Agents []struct {
			Agent struct {
				Harness   string `json:"harness"`
				SessionID string `json:"session_id"`
			} `json:"agent"`
		} `json:"agents"`
	}
	out := must("agents", "--json")
	json.Unmarshal([]byte(out), &agents)
	if len(agents.Agents) != 1 || agents.Agents[0].Agent.SessionID != "abc-123" {
		t.Fatalf("agents: %s", out)
	}

	// Documented exit codes.
	if _, code := hive("-s", "e2e", "run", "--wait", "--", "sh", "-c", "exit 7"); code != 7 {
		t.Errorf("run --wait: exit %d, want 7", code)
	}
	if _, code := hive("-b", "nope", "capture"); code != 3 {
		t.Errorf("missing target: exit %d, want 3", code)
	}
	if _, code := hive("split", "--split", "diagonal", "-b", "agent"); code != 2 {
		t.Errorf("bad flag value: exit %d, want 2", code)
	}

	// Restart: the agent comes back resumed, the layout and labels too.
	must("server", "stop")
	out = must("ls")
	if !strings.Contains(out, "e2e") {
		t.Fatalf("ls after restart: %s", out)
	}
	eventually("agent resumed", func() bool { out, _ := hive("-b", "agent", "capture"); return strings.Contains(out, "resumed-abc-123") })
	out = must("-b", "agent", "capture")
	if !strings.Contains(out, "hooked") || !strings.Contains(out, "restored") {
		t.Fatalf("restored agent pane:\n%s", out)
	}

	// The Claude hook installer, against a scratch config dir.
	cfg := filepath.Join(dir, "claude")
	must("agent", "install", "claude", "--config-dir", cfg)
	data, _ := os.ReadFile(filepath.Join(cfg, "settings.json"))
	for _, ev := range []string{"SessionStart", "SessionEnd", "Notification", "Stop", "UserPromptSubmit"} {
		if !strings.Contains(string(data), ev) {
			t.Errorf("settings.json lacks %s", ev)
		}
	}
}
