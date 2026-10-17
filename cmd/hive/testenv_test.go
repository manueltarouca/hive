package main

import (
	"os"
	"path/filepath"
)

// testEnv is the environment for a test's hive: its own socket, state and
// settings in dir, always together, so a test never reaches the user's
// server, state or settings file. HIVE_SESSION and HIVE_BLOCK are cleared:
// tests run inside hive panes too.
func testEnv(dir string) []string {
	return append(os.Environ(),
		"HIVE_SERVER="+filepath.Join(dir, "s.sock"),
		"HIVE_STATE_DIR="+filepath.Join(dir, "state"),
		"HIVE_CONFIG="+filepath.Join(dir, "config"),
		"HIVE_SESSION=", "HIVE_BLOCK=")
}
