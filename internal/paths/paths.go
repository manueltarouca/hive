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
	return filepath.Join(os.TempDir(), fmt.Sprintf("hive-%d", os.Getuid()), "server.sock")
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
