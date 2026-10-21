package script

import (
	"reflect"
	"strings"
	"testing"
)

func feedAll(chunks ...string) (lines []line, a *assembler) {
	a = &assembler{}
	for _, c := range chunks {
		lines = append(lines, a.feed([]byte(c))...)
	}
	return lines, a
}

func texts(ls []line) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.text)
	}
	return out
}

func TestAssemblerLines(t *testing.T) {
	// Split across reads, colors stripped, OSC titles dropped.
	got, _ := feedAll("ok 1\nFA", "\x1b[31mIL\x1b[0m: TestX\r\n", "\x1b]0;title\x07done\n")
	if want := []string{"ok 1", "FAIL: TestX", "done"}; !reflect.DeepEqual(texts(got), want) {
		t.Fatalf("got %q", texts(got))
	}
	// A spinner redraws with \r: only the last state counts.
	got, _ = feedAll("| 10%\r/ 50%\r- 100%\n")
	if want := []string{"- 100%"}; !reflect.DeepEqual(texts(got), want) {
		t.Fatalf("spinner %q", texts(got))
	}
	// The alternate screen is told apart.
	got, _ = feedAll("main\n\x1b[?1049hfull screen\n\x1b[?1049lback\n")
	if len(got) != 3 || got[0].alt || !got[1].alt || got[2].alt {
		t.Fatalf("alt = %+v", got)
	}
	// A line without a newline comes out when the output goes quiet.
	got, a := feedAll("Listening on :8080")
	if len(got) != 0 {
		t.Fatalf("early: %q", texts(got))
	}
	if l, ok := a.flush(); !ok || l.text != "Listening on :8080" {
		t.Fatalf("flush = %+v %v", l, ok)
	}
	if _, ok := a.flush(); ok {
		t.Fatal("flushed twice")
	}
	if more := a.feed([]byte("\n")); len(more) != 0 {
		t.Fatalf("its newline handed it out again: %q", texts(more))
	}
	if more := a.feed([]byte("next\n")); len(more) != 1 || more[0].text != "next" {
		t.Fatalf("after: %q", texts(more))
	}
	// Very long lines are cut.
	got, _ = feedAll(strings.Repeat("x", 10000) + "\n")
	if len(got) != 1 || len(got[0].text) != maxLine {
		t.Fatalf("long: %d lines, %d bytes", len(got), len(got[0].text))
	}
	// DCS and CSI with parameters, and a lone ESC, never leak.
	got, _ = feedAll("\x1bP1$r0m\x1b\\a\x1b[1;2Hb\x1b7c\n")
	if want := []string{"abc"}; !reflect.DeepEqual(texts(got), want) {
		t.Fatalf("escapes %q", texts(got))
	}
}
