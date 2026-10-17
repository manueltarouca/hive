package persist

import (
	"os"

	"golang.org/x/sys/unix"
)

// syncFile makes f's data reach the disk before anything written after it,
// such as the rename that replaces state.json. F_BARRIERFSYNC orders the
// writes without waiting for the drive to flush its whole cache, which is
// what F_FULLFSYNC (Go's f.Sync on macOS) does: with snapshots just
// buffered, that took ~200 ms per agent report at 128 busy panes. A power
// cut can lose the latest save; it never leaves a torn state.json.
func syncFile(f *os.File) error {
	if _, err := unix.FcntlInt(f.Fd(), unix.F_BARRIERFSYNC, 0); err != nil {
		return f.Sync() // a filesystem without barriers
	}
	return nil
}
