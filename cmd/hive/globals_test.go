package main

import (
	"slices"
	"testing"
)

// label and close have their own --session and --window switches; after the
// command they must not be read as the global target flags.
func TestCommandSwitchesAreNotGlobals(t *testing.T) {
	cases := []struct {
		argv         []string
		session, win string
		rest         []string
	}{
		{[]string{"-s", "work", "label", "--window", "editor"}, "work", "", []string{"label", "--window", "editor"}},
		{[]string{"label", "--session", "api"}, "", "", []string{"label", "--session", "api"}},
		{[]string{"close", "--window"}, "", "", []string{"close", "--window"}},
		{[]string{"-w", "2", "close", "--session"}, "", "2", []string{"close", "--session"}},
		// Before the command, and with =, they are still the global flags.
		{[]string{"--session", "work", "ls"}, "work", "", []string{"ls"}},
		{[]string{"close", "--window=3"}, "", "3", []string{"close"}},
		{[]string{"capture", "--session", "work"}, "work", "", []string{"capture"}},
		// -C/--client was never wired to anything and is gone.
		{[]string{"-C", "x", "ls"}, "", "", []string{"-C", "x", "ls"}},
	}
	for _, c := range cases {
		g, rest, err := parseGlobals(c.argv)
		if err != nil {
			t.Errorf("%v: %v", c.argv, err)
			continue
		}
		if g.session != c.session || g.window != c.win || !slices.Equal(rest, c.rest) {
			t.Errorf("%v: session=%q window=%q rest=%v, want %q %q %v", c.argv, g.session, g.window, rest, c.session, c.win, c.rest)
		}
	}
}
