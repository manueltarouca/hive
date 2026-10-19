package server

import (
	"os"
	"testing"
)

// Tests start /bin/sh, never the user's own shell: its startup files are
// slow, differ from machine to machine, and aren't the tests' to run.
// HOME too: /bin/sh -l reads ~/.profile.
func TestMain(m *testing.M) {
	os.Setenv("SHELL", "/bin/sh")
	home, err := os.MkdirTemp("", "hive-test-home")
	if err == nil {
		os.Setenv("HOME", home)
	}
	code := m.Run()
	if err == nil {
		os.RemoveAll(home)
	}
	os.Exit(code)
}
