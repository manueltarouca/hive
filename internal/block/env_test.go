package block

import (
	"slices"
	"testing"
)

// hive shows OSC 8 hyperlinks and opens them: programs that ask the
// environment (Claude Code's file and URL links among them) are told so,
// unless the user said otherwise.
func TestBaseEnvAdvertisesHyperlinks(t *testing.T) {
	t.Setenv("FORCE_HYPERLINK", "")
	t.Setenv("TERM_PROGRAM_VERSION", "1.2.3") // the outer terminal's
	env := baseEnv()
	if !slices.Contains(env, "FORCE_HYPERLINK=1") {
		t.Fatal("FORCE_HYPERLINK=1 missing") // never print the environment: it holds secrets
	}
	if slices.Contains(env, "TERM_PROGRAM_VERSION=1.2.3") {
		t.Fatal("the outer terminal's TERM_PROGRAM_VERSION leaked into a hive pane")
	}
	t.Setenv("FORCE_HYPERLINK", "0")
	if env := baseEnv(); !slices.Contains(env, "FORCE_HYPERLINK=0") || slices.Contains(env, "FORCE_HYPERLINK=1") {
		t.Fatal("the user's FORCE_HYPERLINK=0 wasn't kept")
	}
}
