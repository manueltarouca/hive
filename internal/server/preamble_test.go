package server

import (
	"strings"
	"testing"
	"time"
)

// The line marking where a restored terminal's old output ends says when,
// and what ran there, in words: no separator glyphs.
func TestRestorePreamble(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	got := string(restorePreamble("claude", at))
	if !strings.Contains(got, "restored 2026-10-18 12:00 (was claude)") || strings.Contains(got, "·") {
		t.Fatalf("preamble = %q", got)
	}
	if got := string(restorePreamble("", at)); strings.Contains(got, "was") {
		t.Fatalf("without a program: %q", got)
	}
}
