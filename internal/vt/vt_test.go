package vt

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestFeedAndFormatPlain(t *testing.T) {
	term, err := New(80, 24, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Feed([]byte("hello \x1b[1;32mworld\x1b[0m\r\n"))
	out, err := term.Format(FormatOpts{Format: "text", Trim: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "hello world" {
		t.Fatalf("got %q", out)
	}
}

func TestHooksFireForTitlePwdBellAndProgramStatus(t *testing.T) {
	var title, pwd string
	var bells int
	var status []ProgramStatus
	term, err := New(80, 24, Hooks{
		Title:         func(s string) { title = s },
		Pwd:           func(s string) { pwd = s },
		Bell:          func() { bells++ },
		ProgramStatus: func(s ProgramStatus) { status = append(status, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Feed([]byte("\x1b]0;my title\x07"))
	term.Feed([]byte("\x1b]7;file://host/tmp/x\x07"))
	term.Feed([]byte("\x07"))
	// msg = base64("Need approval")
	term.Feed([]byte("\x1b]7501;state=blocked:kind=permission:app=claude-code:msg=TmVlZCBhcHByb3ZhbA==\x1b\\"))
	if title != "my title" {
		t.Errorf("title = %q", title)
	}
	if !strings.Contains(pwd, "/tmp/x") {
		t.Errorf("pwd = %q", pwd)
	}
	if bells != 1 {
		t.Errorf("bells = %d", bells)
	}
	if len(status) != 1 || status[0].State != "blocked" || status[0].Kind != "permission" ||
		status[0].App != "claude-code" || status[0].Message != "Need approval" {
		t.Errorf("status = %+v", status)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	term, _ := New(40, 5, Hooks{})
	defer term.Close()
	for i := 0; i < 50; i++ {
		term.Feed([]byte("line\r\n"))
	}
	term.Feed([]byte("last line"))
	snap, err := term.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(snap, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	out, _ := restored.Format(FormatOpts{Format: "text", Trim: true})
	if !strings.HasSuffix(out, "last line") || strings.Count(out, "line\n") < 40 {
		t.Fatalf("scrollback not restored: %q", out)
	}
}

func TestEncodeKey(t *testing.T) {
	term, _ := New(80, 24, Hooks{})
	defer term.Close()
	cases := map[string]string{"enter": "\r", "ctrl+c": "\x03", "escape": "\x1b", "a": "a", "up": "\x1b[A", "tab": "\t"}
	for k, want := range cases {
		got, err := term.EncodeKey(k)
		if err != nil {
			t.Errorf("%s: %v", k, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s: got %q want %q", k, got, want)
		}
	}
}

func TestWritePtyRepliesToDeviceAttributes(t *testing.T) {
	var replies []string
	term, _ := New(80, 24, Hooks{WritePty: func(b []byte) { replies = append(replies, string(b)) }})
	defer term.Close()
	term.Feed([]byte("\x1b[c"))  // DA1
	term.Feed([]byte("\x1b[6n")) // cursor position report
	if len(replies) != 2 || replies[0] != "\x1b[?62;22c" || replies[1] != "\x1b[1;1R" {
		t.Fatalf("replies = %q", replies)
	}
}

func TestHookMayCallBackIntoTerm(t *testing.T) {
	done := make(chan string, 1)
	var term *Term
	term, _ = New(80, 24, Hooks{Title: func(string) { done <- term.Title() }})
	defer term.Close()
	go term.Feed([]byte("\x1b]2;reentrant\x07"))
	select {
	case s := <-done:
		if s != "reentrant" {
			t.Fatalf("title = %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: hook could not call back into Term")
	}
}

// OSC 52 copies reach the hook, but not empty or oversized ones: the text
// goes to every event subscriber, and a truncated paste could be half a
// command.
func TestClipboardWritesAreBounded(t *testing.T) {
	var got []string
	term, err := New(80, 24, Hooks{Clipboard: func(s string) { got = append(got, s) }})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	osc52 := func(s string) []byte {
		return []byte("\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(s)) + "\x07")
	}
	term.Feed(osc52("copied text"))
	term.Feed(osc52(""))
	term.Feed(osc52(strings.Repeat("x", MaxClipboardBytes+1)))
	term.Feed(osc52(strings.Repeat("y", MaxClipboardBytes)))
	if len(got) != 2 || got[0] != "copied text" || len(got[1]) != MaxClipboardBytes {
		lens := []int{}
		for _, g := range got {
			lens = append(lens, len(g))
		}
		t.Fatalf("hook got %d writes, lengths %v", len(got), lens)
	}
}
