package server

import (
	"os"
	"path/filepath"
	"testing"
)

// A block started without a cwd starts in the server account's home, not
// wherever the server happens to run (the daemon runs in /). A remote
// client has no path of its own to give.
func TestEmptyCwdIsHome(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	out := h.call("session.create", map[string]any{"command": []string{"/bin/sh", "-c", "pwd -P; sleep 30"}})
	sid := out["session_id"].(string)
	bid := strs(out["block_ids"])[0]
	h.waitScreen(sid, bid, home)
	if wd, _ := os.Getwd(); wd == home {
		t.Fatal("the test runs in the home it checks for")
	}
}
