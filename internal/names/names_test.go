package names

import (
	"strings"
	"testing"
)

func TestTwoWordNamesAvoidTaken(t *testing.T) {
	taken := map[string]bool{}
	for i := 0; i < 500; i++ {
		n := New(func(s string) bool { return taken[strings.ToLower(s)] })
		if len(strings.Split(n, "-")) != 2 || n != strings.ToLower(n) || taken[n] {
			t.Fatalf("bad or duplicate name %q", n)
		}
		taken[strings.ToLower(n)] = true
	}
}
