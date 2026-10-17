package server

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/persist"
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
	// The save follows the report in the background.
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(saves)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // and nothing else follows
	mu.Lock()
	defer mu.Unlock()
	if len(saves) != 1 || saves[0].Snapshots != 0 {
		t.Fatalf("agent.report's save = %+v, want one save with no snapshots", saves)
	}
	if st, err := persist.New(h.state).LoadState(); err != nil || st.Blocks[bid].Agent == nil {
		t.Fatalf("the agent record wasn't saved: %v", err)
	}
}

// A save writing snapshots (scrollback can take seconds with many full
// panes) doesn't hold up agent.report, which only needs state.json. And
// that save, finishing later, doesn't put back the state from before the
// report.
func TestAgentReportDoesNotWaitForSnapshots(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var armed atomic.Bool
	h := startCfg(t, t.TempDir(), func(c *Config) {
		c.FlushDelay = time.Hour
		c.beforeSnapshot = func(string) {
			if armed.Load() {
				select {
				case entered <- struct{}{}:
				default:
				}
				<-release
			}
		}
	})
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"sh", "-c", "echo busy; exec cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.waitScreen(sid, bid, "busy")

	armed.Store(true)
	saved := make(chan error, 1)
	go func() { saved <- h.srv.flush(true) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the save never reached a snapshot")
	}
	reported := make(chan error, 1)
	go func() {
		reported <- h.c.Call(context.Background(), "agent.report",
			map[string]any{"block_id": bid, "harness": "claude", "state": "working", "session_id": "s-1"}, nil)
	}()
	select {
	case err := <-reported:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		armed.Store(false)
		close(release)
		t.Fatal("agent.report waited for the snapshot save")
	}
	armed.Store(false)
	close(release)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	st, err := persist.New(h.state).LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if a := st.Blocks[bid].Agent; a == nil || a.State != "working" {
		t.Fatalf("after the slow save, the agent record is %+v", a)
	}
}

// agent.report doesn't wait for the disk at all: Claude Code waits for its
// hooks, and a busy disk (many full snapshots being written) made each
// report take ~200 ms. Its record is saved right after, in the background.
func TestAgentReportDoesNotWaitForItsSave(t *testing.T) {
	release := make(chan struct{})
	var stuck atomic.Bool
	h := startCfg(t, t.TempDir(), func(c *Config) {
		c.FlushDelay = time.Hour
		c.beforeStateSave = func() {
			if stuck.Load() {
				<-release
			}
		}
	})
	defer h.stop()
	defer close(release)
	created := h.call("session.create", map[string]any{"command": []string{"sh", "-c", "echo busy; exec cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.waitScreen(sid, bid, "busy")
	stuck.Store(true)
	reported := make(chan error, 1)
	go func() {
		reported <- h.c.Call(context.Background(), "agent.report",
			map[string]any{"block_id": bid, "harness": "claude", "state": "working", "session_id": "s-1"}, nil)
	}()
	select {
	case err := <-reported:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent.report waited for its save")
	}
	stuck.Store(false)
}
