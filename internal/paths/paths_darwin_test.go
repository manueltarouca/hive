package paths

import (
	"path/filepath"
	"testing"
)

// Over SSH there is no TMPDIR: the socket must still be the one Hive.app and
// its background server use (launchd's TMPDIR is the user's temp dir).
func TestSocketWithoutTMPDIR(t *testing.T) {
	t.Setenv("HIVE_SERVER", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	d := userTempDir()
	if d == "" {
		t.Fatal("no per-user temp dir")
	}
	t.Setenv("TMPDIR", d)
	with := Socket()
	t.Setenv("TMPDIR", "")
	if got := Socket(); filepath.Clean(got) != filepath.Clean(with) {
		t.Fatalf("without TMPDIR %q, with %q", got, with)
	}
}
