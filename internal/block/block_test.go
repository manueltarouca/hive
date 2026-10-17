package block

import (
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) emit(e Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *recorder) find(name string) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Name == name {
			return e, true
		}
	}
	return Event{}, false
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func screen(b *Block) string {
	s, _ := b.Format("text", true, false)
	return s
}

func TestRunCommandCaptureAndExit(t *testing.T) {
	rec := &recorder{}
	b, err := Start("block:test1", Options{Command: []string{"sh", "-c", "echo hello-from-pty; exit 3"}, Cols: 80, Rows: 24}, rec.emit)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	select {
	case <-b.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("never exited")
	}
	if !strings.Contains(screen(b), "hello-from-pty") {
		t.Fatalf("screen = %q", screen(b))
	}
	ev, ok := rec.find("child_exited")
	if !ok || ev.Payload["exit_code"] != 3 {
		t.Fatalf("child_exited = %+v", ev)
	}
	if code, exited := b.ExitStatus(); !exited || code != 3 {
		t.Fatalf("exit status = %d %v", code, exited)
	}
}

func TestWriteAndSendKeyInteractive(t *testing.T) {
	b, err := Start("block:test2", Options{Command: []string{"cat"}, Cols: 80, Rows: 24}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.Write([]byte("typed-text")); err != nil {
		t.Fatal(err)
	}
	if err := b.SendKey("enter"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "echo from cat", func() bool { return strings.Count(screen(b), "typed-text") >= 2 })
	if err := b.SendKey("ctrl+c"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("ctrl+c did not stop cat")
	}
}

func TestEnvironmentAndCwd(t *testing.T) {
	dir := t.TempDir()
	b, err := Start("block:test3", Options{
		Command: []string{"sh", "-c", "pwd; echo blk=$HIVE_BLOCK term=$TERM"},
		Cwd:     dir, Env: []string{"HIVE_BLOCK=block:test3"}, Cols: 120, Rows: 24,
	}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	<-b.Done()
	s := screen(b)
	if !strings.Contains(s, dir[len(dir)-10:]) || !strings.Contains(s, "blk=block:test3") || !strings.Contains(s, "term=xterm-256color") {
		t.Fatalf("screen = %q", s)
	}
}

func TestAttachSnapshotThenFrames(t *testing.T) {
	b, err := Start("block:test4", Options{Command: []string{"cat"}, Cols: 80, Rows: 24}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Write([]byte("before\r"))
	waitFor(t, "before", func() bool { return strings.Contains(screen(b), "before") })

	sub, snap, seq, err := b.Attach(64)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Detach(sub)
	if len(snap) == 0 || seq == 0 {
		t.Fatalf("snapshot %d bytes at seq %d", len(snap), seq)
	}
	b.Write([]byte("after\r"))
	var got []byte
	next := seq
	deadline := time.After(5 * time.Second)
	for !strings.Contains(string(got), "after") {
		select {
		case f := <-sub.C:
			if f.Resync {
				t.Fatal("unexpected resync")
			}
			if f.Seq != next {
				t.Fatalf("gap: frame seq %d, want %d", f.Seq, next)
			}
			next += uint64(len(f.Data))
			got = append(got, f.Data...)
		case <-deadline:
			t.Fatalf("no frames; got %q", got)
		}
	}
}

func TestSlowSubscriberGetsResync(t *testing.T) {
	b, err := Start("block:test5", Options{Command: []string{"sh", "-c", "for i in $(seq 1 3000); do echo line $i; done; sleep 5"}, Cols: 80, Rows: 24}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	sub, _, _, err := b.Attach(2) // tiny buffer, never read until the burst is over
	if err != nil {
		t.Fatal(err)
	}
	defer b.Detach(sub)
	waitFor(t, "burst", func() bool { return strings.Contains(screen(b), "line 3000") })
	sawResync := false
	for len(sub.C) > 0 {
		if f := <-sub.C; f.Resync {
			sawResync = true
		}
	}
	if !sawResync {
		t.Fatal("overflowing subscriber was not told to resync")
	}
}

func TestForegroundProcess(t *testing.T) {
	dir := t.TempDir()
	b, err := Start("block:test6", Options{Command: []string{"sh", "-c", "cd " + dir + " && exec sleep 30"}, Cols: 80, Rows: 24}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var p Process
	waitFor(t, "sleep in foreground", func() bool {
		p, _ = b.Foreground()
		return p.Name == "sleep"
	})
	if p.PID == 0 || !strings.HasSuffix(p.Cwd, dir[len(dir)-10:]) {
		t.Fatalf("process = %+v", p)
	}
}

func TestRestoreKeepsCursorBelowOldOutput(t *testing.T) {
	for _, alt := range []bool{false, true} {
		src, err := Start("block:src", Options{Command: []string{"cat"}, Cols: 60, Rows: 20}, func(Event) {})
		if err != nil {
			t.Fatal(err)
		}
		// Old output on the primary screen, then optionally a TUI on the
		// alternate screen, as when vim or an agent is open at shutdown.
		src.term.Feed([]byte("old-1\r\nold-2\r\nold-3\r\n$ "))
		if alt {
			src.term.Feed([]byte("\x1b[?1049h\x1b[2J\x1b[Htui screen"))
		}
		snap, _ := src.Snapshot()
		src.Close()

		b, err := Start("block:dst", Options{Command: []string{"sh", "-c", "echo NEW-SHELL; exec cat"},
			Cols: 60, Rows: 20, Restore: snap, Preamble: []byte("\r\nBANNER\r\n")}, func(Event) {})
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "new shell output", func() bool { return strings.Contains(screen(b), "NEW-SHELL") })
		s := screen(b)
		b.Close()
		i3, ib, in := strings.Index(s, "old-3"), strings.Index(s, "BANNER"), strings.Index(s, "NEW-SHELL")
		if !(i3 >= 0 && i3 < ib && ib < in) || strings.Contains(s, "tui screen") {
			t.Fatalf("alt=%v: want old output, banner, new output in order:\n%s", alt, s)
		}
	}
}

func TestCorruptRestoreStartsFresh(t *testing.T) {
	b, err := Start("block:bad", Options{Command: []string{"sh", "-c", "echo NEW; exec cat"}, Restore: []byte("garbage")}, func(Event) {})
	if err != nil {
		t.Fatalf("start with a bad snapshot: %v", err)
	}
	defer b.Close()
	waitFor(t, "fresh terminal", func() bool { return strings.Contains(screen(b), "NEW") })
	if !strings.Contains(screen(b), "scrollback could not be restored") || b.PID() <= 0 {
		t.Fatalf("screen = %q pid = %d", screen(b), b.PID())
	}
}
