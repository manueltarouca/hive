package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/client"
)

// Bug 1: state was marshalled after the lock was released while events
// mutated the same maps. Run with -race.
func TestFlushAndViewsUnderStatusTraffic(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			// cat echoes it back, so the terminal sees a stream of OSC 7501 reports.
			seq := "\x1b]7501;state=working:id=job" + string(rune('a'+i%26)) + ":app=t\x1b\\"
			h.c.Call(context.Background(), "hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{"data": seq}}, nil)
			h.c.Call(context.Background(), "agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "working"}, nil)
		}
	})
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		h.call("server.flush", nil)
		h.call("session.view", map[string]any{"session_id": sid})
		h.call("agent.list", nil)
	}
	close(stop)
	wg.Wait()
}

// Bug 2: an unreadable snapshot must not delete the pane.
func TestCorruptSnapshotFallsBackToFreshTerminal(t *testing.T) {
	state := t.TempDir()
	h := start(t, state, nil)
	created := h.call("session.create", map[string]any{"label": "c", "command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.stop()
	writeFile(t, h.srv.store.Dir()+"/blocks/"+strings.TrimPrefix(bid, "block:")+".snap", "not a snapshot")
	h2 := start(t, state, nil)
	defer h2.stop()
	s := h2.waitScreen(sid, bid, "restored")
	if !strings.Contains(s, "scrollback could not be restored") {
		t.Fatalf("screen:\n%s", s)
	}
}

// Bug 3: a block that exits and closes before wait is called still reports
// its status.
func TestWaitAfterBlockClosed(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)
	split := h.call("session.new_split", map[string]any{"session_id": sid, "block_id": strs(created["block_ids"])[0],
		"layout": map[string]any{"command": []string{"sh", "-c", "exit 5"}}})
	bid := strs(split["block_ids"])[0]
	for range 100 {
		var e *client.Error
		if err := h.c.Call(context.Background(), "hive.terminal.size", map[string]any{"block_id": bid}, nil); errors.As(err, &e) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	out := h.call("hive.terminal.wait", map[string]any{"block_id": bid, "args": map[string]any{"timeout": 1}})
	if out["exited"] != true || out["exit_code"].(float64) != 5 {
		t.Fatalf("wait = %v", out)
	}
}

// Bug 4: a resume that fails leaves the pane open.
func TestFailedResumeKeepsPane(t *testing.T) {
	state := t.TempDir()
	resume := func(string, string) []string { return []string{"sh", "-c", "echo resume-failed; exit 1"} }
	h := start(t, state, resume)
	created := h.call("session.create", map[string]any{"label": "r", "command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle", "session_id": "stale"})
	h.stop()
	h2 := start(t, state, resume)
	defer h2.stop()
	h2.waitScreen(sid, bid, "resume-failed")
	time.Sleep(300 * time.Millisecond)
	if len(h2.call("session.list", nil)["sessions"].([]any)) != 1 {
		t.Fatal("session closed after a failed resume")
	}
}

// Bug 5: closing a block through the API still reports child_exited.
// Bug 6: process reports the child's pid.
func TestCloseReportsChildExitedAndPid(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)
	split := h.call("session.new_split", map[string]any{"session_id": sid, "block_id": strs(created["block_ids"])[0],
		"layout": map[string]any{"command": []string{"cat"}}})
	bid := strs(split["block_ids"])[0]
	if p := h.call("hive.terminal.process", map[string]any{"block_id": bid}); p["child_pid"].(float64) <= 0 {
		t.Fatalf("process = %v", p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	seen := make(chan bool, 1)
	ready := make(chan struct{})
	go h.c.Events(ctx, sid, false, -1, func(e client.Event) bool {
		if e.Type == "subscribed" {
			close(ready)
		}
		if strings.Contains(string(e.Raw), "child_exited") && strings.Contains(string(e.Raw), bid) {
			seen <- true
			return false
		}
		return true
	})
	<-ready
	h.call("block.close", map[string]any{"session_id": sid, "block_id": bid})
	select {
	case <-seen:
	case <-ctx.Done():
		t.Fatal("no child_exited for a block closed through the API")
	}
}

// Should-fix: inner events carry only type and payload; resize emits
// size_changed.
func TestEventShapeAndSizeChanged(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.call("hive.terminal.resize", map[string]any{"block_id": bid, "args": map[string]any{"cols": 70, "rows": 20}})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var found bool
	h.c.Events(ctx, sid, false, 0, func(e client.Event) bool {
		var p struct {
			Event map[string]json.RawMessage `json:"event"`
		}
		json.Unmarshal(e.Payload, &p)
		if p.Event != nil {
			if _, bad := p.Event["seq"]; bad {
				t.Errorf("inner event has seq: %s", e.Raw)
			}
		}
		if strings.Contains(string(e.Raw), `"size_changed"`) && strings.Contains(string(e.Raw), `"cols":70`) {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Fatal("no size_changed event")
	}
}
