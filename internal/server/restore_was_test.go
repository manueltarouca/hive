package server

import (
	"strings"
	"testing"
	"time"
)

// A resumed agent that has since ended must not leave
// its resume command in the saved state: a later restart
// would say "(was claude --resume …)" for a pane at a prompt.
func TestRestoreMarkerAfterEndedResume(t *testing.T) {
	defer func(g time.Duration) { resumeGrace = g }(resumeGrace)
	resumeGrace = 800 * time.Millisecond
	state := t.TempDir()
	resume := func(h, id string) []string { return []string{"sh", "-c", "echo resumed " + id + "; exit 0"} }
	h := start(t, state, resume)
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle", "session_id": "s-7"})
	h.stop()

	h2 := start(t, state, resume)
	h2.waitScreen(sid, bid, "resumed s-7")
	time.Sleep(resumeGrace + 700*time.Millisecond) // the agent is ended, the shell in front
	h2.stop()

	h3 := start(t, state, resume)
	defer h3.stop()
	screen := h3.waitScreen(sid, bid, "restored")
	if last := screen[strings.LastIndex(screen, "restored"):]; strings.Contains(last, "(was") {
		t.Fatalf("second restore marker names the old resume command: %q", strings.SplitN(last, "\n", 2)[0])
	}
}
