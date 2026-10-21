// Package events is hive's event bus. Events are numbered; a subscriber can
// start from a number it already saw to catch up after a reconnect, as long
// as the event is still in the replay ring. A subscriber that can't keep up
// is cut off rather than slowing the server down; it reconnects with ?since=.
package events

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Event is one event. Session events wrap their inner event as
// {"session_id", "event"}; block events wrap theirs again as
// {"block_id", "creator_name", "name", "payload"}.
type Event struct {
	Seq     uint64         `json:"seq"`
	Time    time.Time      `json:"time"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`

	sessionID string
	scoped    bool
}

// Creator is the name of hive's terminal block kind.
const Creator = "hive.terminal"

// Filter chooses events. With no field set, a subscriber hears session
// lifecycle only: server events whose type starts with "session_", such as
// session_created and session_destroyed. Types, when set, chooses by type
// alone, and is the only way to hear scoped events.
type Filter struct {
	SessionID string
	All       bool
	Types     []string
	// Scoped names scoped events an All subscriber hears too (scripts
	// hear other scripts end, but not their every log line).
	Scoped []string
}

func (f Filter) match(e Event) bool {
	switch {
	case len(f.Types) > 0:
		return slices.Contains(f.Types, e.Type)
	case e.scoped:
		return f.All && slices.Contains(f.Scoped, e.Type)
	case f.All:
		return true
	case f.SessionID != "":
		return e.sessionID == f.SessionID
	}
	return e.sessionID == "" && strings.HasPrefix(e.Type, "session_")
}

// Inner is an event nested in another: a session's event, or a block's.
type Inner struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

// Sub is a subscription.
type Sub struct {
	C chan Event
	// Gap is set when since asked for events no longer in the ring: the
	// subscriber missed some and should re-read state.
	Gap     bool
	filter  Filter
	mu      sync.Mutex
	dropped bool
}

// Dropped reports whether the bus cut this subscriber off for being slow.
func (s *Sub) Dropped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Bus distributes events.
type Bus struct {
	mu   sync.Mutex
	seq  uint64
	ring []Event
	size int
	// Scoped events have their own history, so a chatty script can't push
	// the apps' events out of theirs.
	sring []Event
	subs  map[*Sub]struct{}
}

// scopedRing is how many scoped events are kept for replay.
const scopedRing = 1024

// New makes a bus that keeps the last ringSize events for replay.
func New(ringSize int) *Bus {
	return &Bus{size: ringSize, subs: map[*Sub]struct{}{}}
}

func (b *Bus) publish(sessionID, typ string, payload map[string]any, scoped bool) Event {
	if payload == nil {
		payload = map[string]any{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	e := Event{Seq: b.seq, Time: time.Now().UTC(), Type: typ, Payload: payload, sessionID: sessionID, scoped: scoped}
	if scoped {
		b.sring = keep(append(b.sring, e), scopedRing)
	} else {
		b.ring = keep(append(b.ring, e), b.size)
	}
	for s := range b.subs {
		if !s.filter.match(e) {
			continue
		}
		select {
		case s.C <- e:
		default:
			b.cut(s)
		}
	}
	return e
}

func keep(ring []Event, n int) []Event {
	if len(ring) > n {
		return ring[len(ring)-n:]
	}
	return ring
}

func (b *Bus) cut(s *Sub) {
	delete(b.subs, s)
	s.mu.Lock()
	s.dropped = true
	s.mu.Unlock()
	close(s.C)
}

// Server publishes an event about the server as a whole.
func (b *Bus) Server(typ string, payload map[string]any) Event {
	return b.publish("", typ, payload, false)
}

// Scoped publishes a server event only subscribers naming its type hear,
// such as a script's log lines.
func (b *Bus) Scoped(typ string, payload map[string]any) Event {
	return b.publish("", typ, payload, true)
}

// Session publishes an event about one session.
func (b *Bus) Session(sessionID, typ string, payload map[string]any) Event {
	if payload == nil {
		payload = map[string]any{}
	}
	return b.publish(sessionID, "session_event", map[string]any{
		"session_id": sessionID,
		"event":      Inner{Type: typ, Payload: payload},
	}, false)
}

// Block publishes an event a block reported.
func (b *Bus) Block(sessionID, blockID, name string, payload map[string]any) Event {
	if payload == nil {
		payload = map[string]any{}
	}
	return b.Session(sessionID, "block_event", map[string]any{
		"block_id": blockID, "creator_name": Creator, "name": name, "payload": payload,
	})
}

// Seq returns the number of the latest event.
func (b *Bus) Seq() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}

// Live, passed as since, subscribes to new events only.
const Live = -1

// Subscribe starts a subscription. With since >= 0, events numbered above
// since that are still in the ring are delivered first.
func (b *Bus) Subscribe(f Filter, since int64, buffer int) *Sub {
	b.mu.Lock()
	defer b.mu.Unlock()
	var backlog []Event
	gap := false
	if since >= 0 {
		// A gap is judged on the history the filter mostly reads: scoped
		// events for a filter by type, the rest otherwise.
		rings, judged := [][]Event{b.ring}, b.ring
		if len(f.Types) > 0 || len(f.Scoped) > 0 {
			rings, judged = append(rings, b.sring), b.sring
		}
		gap = len(judged) > 0 && uint64(since)+1 < judged[0].Seq
		for _, ring := range rings {
			for _, e := range ring {
				if e.Seq > uint64(since) && f.match(e) {
					backlog = append(backlog, e)
				}
			}
		}
		slices.SortFunc(backlog, func(a, b Event) int { return cmp.Compare(a.Seq, b.Seq) })
	}
	s := &Sub{C: make(chan Event, max(buffer, len(backlog)+1)), filter: f, Gap: gap}
	for _, e := range backlog {
		s.C <- e
	}
	b.subs[s] = struct{}{}
	return s
}

// Unsubscribe ends a subscription.
func (b *Bus) Unsubscribe(s *Sub) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		close(s.C)
	}
}
