//go:build !darwin

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// spawnDaemon starts the server in its own session, in /, writing to logf.
func spawnDaemon(exe string, argv, env []string, logf *os.File) error {
	cmd := exec.Command(exe, argv[1:]...)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Dir = "/"
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
