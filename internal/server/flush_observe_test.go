package server

import (
	"sync"
	"testing"
	"time"
)

// Benchmarks watch saves: OnFlush reports each one's duration and how many
// terminals it snapshotted.
func TestOnFlushReportsSaves(t *testing.T) {
	var mu sync.Mutex
	var got []FlushStats
	h := startCfg(t, t.TempDir(), func(c *Config) {
		c.OnFlush = func(f FlushStats) { mu.Lock(); got = append(got, f); mu.Unlock() }
	})
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"sh", "-c", "echo hello; exec cat"}})
	h.waitScreen(created["session_id"].(string), strs(created["block_ids"])[0], "hello")
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		var snaps int
		for _, f := range got {
			snaps += f.Snapshots
		}
		mu.Unlock()
		if n > 0 && snaps > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("saves seen: %d, snapshots %d", n, snaps)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// agent.report saves the agent record at once, without snapshotting every
// terminal: its cost mustn't grow with how many panes have new output.
func TestAgentReportSavesOnlyState(t *testing.T) {
	var mu sync.Mutex
	var saves []FlushStats
	h := startCfg(t, t.TempDir(), func(c *Config) {
		c.FlushDelay = time.Hour // only the report's own save
		c.OnFlush = func(f FlushStats) { mu.Lock(); saves = append(saves, f); mu.Unlock() }
	})
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"sh", "-c", "echo busy; exec cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.waitScreen(sid, bid, "busy")
	mu.Lock()
	saves = nil
	mu.Unlock()
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle", "session_id": "s-1"})
	mu.Lock()
	defer mu.Unlock()
	if len(saves) != 1 || saves[0].Snapshots != 0 {
		t.Fatalf("agent.report's save = %+v, want one save with no snapshots", saves)
	}
}
