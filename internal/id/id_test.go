package id

import (
	"strings"
	"testing"
)

func TestNewHasKindPrefixAndIsUUIDv7(t *testing.T) {
	s := New(Block)
	if !strings.HasPrefix(s, "block:") {
		t.Fatalf("missing prefix: %s", s)
	}
	u := strings.TrimPrefix(s, "block:")
	if len(u) != 36 || u[14] != '7' {
		t.Fatalf("not a uuidv7: %s", u)
	}
	if KindOf(s) != Block {
		t.Fatalf("kind = %q", KindOf(s))
	}
}

func TestNewIsMonotonicAndUnique(t *testing.T) {
	prev := New(Session)
	for range 1000 {
		next := New(Session)
		if next <= prev {
			t.Fatalf("not increasing: %s <= %s", next, prev)
		}
		prev = next
	}
}

func TestShort(t *testing.T) {
	s := "session:01a10cf3-2655-7e48-9b9c-43dd6612bef5"
	if Short(s) != "12bef5" {
		t.Fatalf("short = %q", Short(s))
	}
}
