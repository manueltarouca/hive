package script

import (
	"slices"
	"sync"
)

// Causes: which script's call made an event. A script's own changes don't
// wake its own h.on handlers (unless self: true), and a chain of scripts
// waking each other stops at maxDepth.
//
// The server publishes the events a method makes while the call runs, so
// an event numbered within a call's window, in a session the call named,
// is that call's. While a changing call is in flight, scripts hold the
// events after its start until it ends and its window is known.

const (
	maxDepth   = 8
	keepWindow = 1024
)

type window struct {
	start, end uint64
	sessions   []string
	run        string // the run that made the call
	depth      int
}

type causes struct {
	mu       sync.Mutex
	next     uint64
	inflight map[uint64]uint64 // call to the bus seq it started at
	windows  []window
}

func newCauses() *causes { return &causes{inflight: map[uint64]uint64{}} }

func (c *causes) begin(start uint64) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	c.inflight[c.next] = start
	return c.next
}

func (c *causes) end(id uint64, w window) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, id)
	if w.end > w.start {
		c.windows = append(c.windows, w)
		if len(c.windows) > keepWindow {
			c.windows = c.windows[len(c.windows)-keepWindow:]
		}
	}
}

// holdAfter is the seq after which events must wait: a changing call that
// started there is still running. ok is false when none is.
func (c *causes) holdAfter() (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var min uint64
	ok := false
	for _, s := range c.inflight {
		if !ok || s < min {
			min, ok = s, true
		}
	}
	return min, ok
}

// of finds the call that made an event, if a script's did.
func (c *causes) of(ev event) (window, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sid := ev.SessionID
	if sid == "" {
		// session_created and session_destroyed name theirs inside.
		if s, ok := ev.Payload["session"].(map[string]any); ok {
			sid, _ = s["session_id"].(string)
		}
		if s, ok := ev.Payload["session_id"].(string); ok && sid == "" {
			sid = s
		}
	}
	for i := len(c.windows) - 1; i >= 0; i-- {
		w := c.windows[i]
		if ev.Seq > w.start && ev.Seq <= w.end && sid != "" && slices.Contains(w.sessions, sid) {
			return w, true
		}
	}
	return window{}, false
}
