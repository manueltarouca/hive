package block

import (
	"fmt"
	"os"
	"strconv"
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

// stat returns the fields of /proc/<pid>/stat after the command name:
// state, ppid, pgrp, session, tty_nr, ...
func stat(pid int) []string {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return nil
	}
	s := string(raw) // comm can hold spaces and parentheses: read after the last ')'
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return nil
	}
	return strings.Fields(s[i+1:])
}

// Parent returns a process's parent, or -1.
func Parent(pid int) int {
	f := stat(pid)
	if len(f) < 2 {
		return -1
	}
	ppid, err := strconv.Atoi(f[1])
	if err != nil {
		return -1
	}
	return ppid
}

// TTY returns the device of a process's controlling terminal: -1 if it has
// none or can't be read.
func TTY(pid int) int64 {
	f := stat(pid)
	if len(f) < 5 {
		return -1
	}
	t, err := strconv.ParseInt(f[4], 10, 64)
	if err != nil || t == 0 {
		return -1
	}
	return t
}
