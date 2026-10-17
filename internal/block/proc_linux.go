package block

import (
	"fmt"
	"os"
	"strings"
)

func describe(pid int) (Process, error) {
	comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return Process{}, err
	}
	p := Process{PID: pid, Name: strings.TrimSpace(string(comm))}
	p.Cwd, _ = os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	exe, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	var argv []string
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		argv = strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	}
	// The arguments stay here: they can hold secrets, so only the program
	// they name leaves the server.
	p.Program = programOf(p.Name, exe, argv)
	return p, nil
}
