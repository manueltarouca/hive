//go:build !darwin

package persist

import "os"

// syncFile is fsync, which on Linux already writes through the drive cache.
func syncFile(f *os.File) error { return f.Sync() }
