// Package paths finds hive's socket and state directory.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// Socket returns the default server socket path.
func Socket() string {
	if s := os.Getenv("HIVE_SERVER"); s != "" {
		return s
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "hive", "server.sock")
	}
	return filepath.Join(tempDir(), fmt.Sprintf("hive-%d", os.Getuid()), "server.sock")
}

// State returns the state directory.
func State() string {
	if d := os.Getenv("HIVE_STATE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "hive")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "hive")
}

// tempDir is $TMPDIR, else on macOS the user's own temporary directory (what
// launchd sets TMPDIR to: an SSH login has no TMPDIR, and must find the same
// socket as Hive.app and its background server), else /tmp.
func tempDir() string {
	if d := os.Getenv("TMPDIR"); d != "" {
		return d
	}
	if d := userTempDir(); d != "" {
		return d
	}
	return os.TempDir()
}

// LaunchdSocket is the default socket as a process launchd starts sees it
// (TMPDIR is the user's temp dir there): Hive's background server's.
func LaunchdSocket() string {
	if s := os.Getenv("HIVE_SERVER"); s != "" {
		return s
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "hive", "server.sock")
	}
	d := userTempDir()
	if d == "" {
		d = os.TempDir()
	}
	return filepath.Join(d, fmt.Sprintf("hive-%d", os.Getuid()), "server.sock")
}
