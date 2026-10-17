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
	return p, nil
}
