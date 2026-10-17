package persist

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/manueltarouca/hive/internal/session"
)

func TestStateRoundTripAndSnapshots(t *testing.T) {
	dir := t.TempDir()
	st := New(dir)
	in := &State{
		Sessions: []*session.Session{{ID: "session:1", Label: "work", Windows: []*session.Window{{ID: "window:1", Root: &session.Node{Type: "block", BlockID: "block:aa"}}}}},
		Blocks: map[string]*BlockState{"block:aa": {SessionID: "session:1", Cwd: "/tmp",
			Agent: &Agent{Harness: "claude", SessionID: "abc"}}},
	}
	if err := st.SaveState(in); err != nil {
		t.Fatal(err)
	}
	out, err := st.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != Version || out.Sessions[0].Label != "work" || out.Blocks["block:aa"].Agent.SessionID != "abc" {
		t.Fatalf("out = %+v", out)
	}

	if err := st.SaveSnapshot("block:aa", func(w io.Writer) error { _, err := w.Write([]byte("snap")); return err }); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.LoadSnapshot("block:aa"); !bytes.Equal(got, []byte("snap")) {
		t.Fatalf("snapshot = %q", got)
	}
	// A snapshot whose block is gone is removed by GC; temp files too.
	st.SaveSnapshot("block:bb", func(w io.Writer) error { return nil })
	os.WriteFile(filepath.Join(dir, "blocks", "x.tmp"), nil, 0o600)
	st.GC(map[string]bool{"block:aa": true})
	if _, err := st.LoadSnapshot("block:bb"); err == nil {
		t.Fatal("orphan snapshot survived GC")
	}
	if _, err := os.Stat(filepath.Join(dir, "blocks", "x.tmp")); err == nil {
		t.Fatal("temp file survived GC")
	}
	if _, err := st.LoadSnapshot("block:aa"); err != nil {
		t.Fatal("live snapshot removed by GC")
	}
}

func TestLoadMissingStateIsEmpty(t *testing.T) {
	out, err := New(t.TempDir()).LoadState()
	if err != nil || out != nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestFailedSnapshotKeepsOld(t *testing.T) {
	st := New(t.TempDir())
	st.SaveSnapshot("block:aa", func(w io.Writer) error { w.Write([]byte("good")); return nil })
	err := st.SaveSnapshot("block:aa", func(w io.Writer) error { w.Write([]byte("par")); return io.ErrUnexpectedEOF })
	if err == nil {
		t.Fatal("want error")
	}
	if got, _ := st.LoadSnapshot("block:aa"); string(got) != "good" {
		t.Fatalf("snapshot = %q", got)
	}
}
