package events

import "testing"

func TestScopesAndSince(t *testing.T) {
	b := New(100)
	b.Server("session_created", map[string]any{"session": "s1"})
	b.Session("s1", "window_created", map[string]any{"window_id": "w"})
	b.Block("s1", "blk", "bell", nil)
	b.Session("s2", "window_created", nil)
	b.Server("client_joined", nil)

	all := b.Subscribe(Filter{All: true}, 0, 16)
	defer b.Unsubscribe(all)
	if got := drain(all); len(got) != 5 || got[0].Seq != 1 || got[4].Seq != 5 {
		t.Fatalf("all replay = %+v", got)
	}

	s1 := b.Subscribe(Filter{SessionID: "s1"}, 0, 16)
	defer b.Unsubscribe(s1)
	got := drain(s1)
	if len(got) != 2 || got[1].Type != "session_event" {
		t.Fatalf("s1 = %+v", got)
	}
	inner := got[1].Payload["event"].(Inner)
	if inner.Type != "block_event" || inner.Payload["name"] != "bell" || inner.Payload["creator_name"] != "hive.terminal" {
		t.Fatalf("inner = %+v", inner)
	}

	// Lifecycle only: session_created/destroyed and session_event summaries are
	// not included, only server lifecycle events.
	life := b.Subscribe(Filter{}, 0, 16)
	defer b.Unsubscribe(life)
	if got := drain(life); len(got) != 1 || got[0].Type != "session_created" {
		t.Fatalf("lifecycle = %+v", got)
	}

	since := b.Subscribe(Filter{All: true}, 3, 16)
	defer b.Unsubscribe(since)
	if got := drain(since); len(got) != 2 || got[0].Seq != 4 {
		t.Fatalf("since = %+v", got)
	}
}

func TestSlowSubscriberIsCut(t *testing.T) {
	b := New(100)
	s := b.Subscribe(Filter{All: true}, Live, 2)
	for i := 0; i < 10; i++ {
		b.Server("tick", nil)
	}
	n := 0
	for range s.C {
		n++
	}
	if n > 2 || !s.Dropped() {
		t.Fatalf("n=%d dropped=%v", n, s.Dropped())
	}
}

func drain(s *Sub) []Event {
	var out []Event
	for {
		select {
		case e := <-s.C:
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestSinceOlderThanRingIsAGap(t *testing.T) {
	b := New(3)
	for i := 0; i < 10; i++ {
		b.Server("tick", nil)
	}
	s := b.Subscribe(Filter{All: true}, 2, 16)
	defer b.Unsubscribe(s)
	if !s.Gap {
		t.Fatal("events 3-7 are gone; the subscriber must be told")
	}
	ok := b.Subscribe(Filter{All: true}, 7, 16)
	defer b.Unsubscribe(ok)
	if ok.Gap {
		t.Fatal("no gap when everything after since is in the ring")
	}
}
