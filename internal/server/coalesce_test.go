package server

import (
	"testing"

	"github.com/manueltarouca/hive/internal/block"
)

// A macOS pty gives at most ~1 KB a read, so at 128 MB/s a client got
// 133,000 WebSocket frames a second, a syscall each way. Output already
// queued for a subscriber goes out as one frame; a resize or resync stays
// where it is in the stream.
func TestCoalesceMergesQueuedOutput(t *testing.T) {
	q := make(chan block.Frame, 8)
	q <- block.Frame{BlockID: "b", Seq: 2, Data: []byte("cd")}
	q <- block.Frame{BlockID: "b", Seq: 4, Data: []byte("ef")}
	q <- block.Frame{BlockID: "b", Seq: 6, Resize: true, Cols: 80, Rows: 24}
	q <- block.Frame{BlockID: "b", Seq: 6, Data: []byte("gh")}
	first := block.Frame{BlockID: "b", Seq: 0, Data: []byte("ab")}

	m, next := coalesce(first, q, 1<<20)
	if m.Seq != 0 || string(m.Data) != "abcdef" {
		t.Fatalf("merged = %d %q", m.Seq, m.Data)
	}
	if next == nil || !next.Resize || next.Seq != 6 {
		t.Fatalf("next = %+v, want the resize", next)
	}
	if string(first.Data) != "ab" {
		t.Fatalf("the first frame's data changed: %q", first.Data)
	}
	if f := <-q; string(f.Data) != "gh" {
		t.Fatalf("left in the queue: %+v", f)
	}

	// Nothing queued: the frame goes as it is, nothing waits.
	m, next = coalesce(first, q, 1<<20)
	if string(m.Data) != "ab" || next != nil {
		t.Fatalf("alone: %q %+v", m.Data, next)
	}

	// A cap: what doesn't fit stays for the next frame.
	q <- block.Frame{BlockID: "b", Seq: 2, Data: []byte("cd")}
	q <- block.Frame{BlockID: "b", Seq: 4, Data: []byte("ef")}
	m, next = coalesce(first, q, 4)
	if string(m.Data) != "abcd" || next == nil || string(next.Data) != "ef" || next.Seq != 4 {
		t.Fatalf("capped: %q, next %+v", m.Data, next)
	}

	// A resync ends the merge and comes back to the caller.
	q <- block.Frame{BlockID: "b", Resync: true}
	if m, next = coalesce(first, q, 1<<20); string(m.Data) != "ab" || next == nil || !next.Resync {
		t.Fatalf("resync: %q %+v", m.Data, next)
	}
}
