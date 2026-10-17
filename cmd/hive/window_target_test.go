package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --window acts on the targeted block's window: the one named with -b, or the
// terminal the command runs in. Only without either is it the active window.
// (It once closed the active window, and the agent running in it.)
func TestWindowSwitchesFollowTheBlock(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hive")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	dir, err := os.MkdirTemp("/tmp", "hive-wt.")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	base := append(os.Environ(), "HIVE_SERVER="+filepath.Join(dir, "s.sock"), "HIVE_STATE_DIR="+filepath.Join(dir, "state"), "HIVE_SESSION=", "HIVE_BLOCK=")
	run := func(env []string, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("hive %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	defer run(base, "server", "stop")
	labels := func() []string {
		var v struct {
			Windows []struct {
				Label string `json:"label"`
			} `json:"windows"`
		}
		if err := json.Unmarshal([]byte(run(base, "-s", "w", "session", "inspect")), &v); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, w := range v.Windows {
			out = append(out, w.Label)
		}
		return out
	}

	// Window "a" stays active; "b" and "c" open behind it.
	run(base, "new", "w", "--cwd", dir)
	run(base, "-s", "w", "label", "--window", "a")
	run(base, "-s", "w", "run", "--label", "b", "--focus=false", "--keep-open", "--", "cat")
	cID := strings.TrimSpace(run(base, "-s", "w", "run", "--label", "c", "--focus=false", "--keep-open", "--", "cat"))

	run(base, "-s", "w", "-b", "b", "label", "--window", "bee")
	if got := strings.Join(labels(), ","); got != "a,bee,c" {
		t.Fatalf("label --window -b b: windows = %s", got)
	}
	run(base, "-s", "w", "-b", "b", "close", "--window")
	if got := strings.Join(labels(), ","); got != "a,c" {
		t.Fatalf("close --window -b b: windows = %s", got)
	}
	// Inside a terminal, with no flags: the terminal's own window.
	sid := strings.TrimSpace(run(base, "api", "call", "target.resolve", `{"kind":"session","target":"w"}`))
	var r struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(sid), &r)
	inside := append(base, "HIVE_SESSION="+r.ID, "HIVE_BLOCK="+cID)
	run(inside, "close", "--window")
	if got := strings.Join(labels(), ","); got != "a" {
		t.Fatalf("close --window inside c: windows = %s", got)
	}
}
