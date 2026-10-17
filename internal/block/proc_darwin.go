package block

/*
#include <libproc.h>
#include <sys/proc_info.h>
#include <string.h>

static int hive_proc_ppid(int pid) {
	struct proc_bsdinfo bi;
	if (proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &bi, sizeof(bi)) != sizeof(bi)) return -1;
	return (int)bi.pbi_ppid;
}

static long long hive_proc_tty(int pid) {
	struct proc_bsdinfo bi;
	if (proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &bi, sizeof(bi)) != sizeof(bi)) return -1;
	return (long long)bi.e_tdev;
}

static int hive_proc_cwd(int pid, char *out, int len) {
	struct proc_vnodepathinfo vpi;
	int n = proc_pidinfo(pid, PROC_PIDVNODEPATHINFO, 0, &vpi, sizeof(vpi));
	if (n != sizeof(vpi)) return -1;
	strncpy(out, vpi.pvi_cdir.vip_path, len - 1);
	out[len - 1] = 0;
	return 0;
}
*/
import "C"

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// procArgs reads a process's arguments (kern.procargs2: argc, the
// executable path, padding, then argv), or nil.
func procArgs(pid int) []string {
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(buf) < 4 {
		return nil
	}
	argc := int(binary.LittleEndian.Uint32(buf))
	rest := buf[4:]
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return nil
	}
	rest = bytes.TrimLeft(rest[i:], "\x00")
	var argv []string
	for len(argv) < argc && len(rest) > 0 {
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			j = len(rest)
		}
		argv = append(argv, string(rest[:j]))
		rest = rest[min(j+1, len(rest)):]
	}
	return argv
}

// TTY returns the device of a process's controlling terminal: -1 if it has
// none or can't be read.
func TTY(pid int) int64 {
	t := int64(C.hive_proc_tty(C.int(pid)))
	if t == 0 || uint32(t) == 0xFFFFFFFF { // NODEV
		return -1
	}
	return t
}

// Parent returns a process's parent, or -1.
func Parent(pid int) int { return int(C.hive_proc_ppid(C.int(pid))) }

func describe(pid int) (Process, error) {
	var name [256]C.char
	if C.proc_name(C.int(pid), unsafe.Pointer(&name[0]), C.uint32_t(len(name))) <= 0 {
		return Process{}, fmt.Errorf("no process %d", pid)
	}
	p := Process{PID: pid, Name: C.GoString(&name[0])}
	var exe [C.PROC_PIDPATHINFO_MAXSIZE]C.char
	path := ""
	if C.proc_pidpath(C.int(pid), unsafe.Pointer(&exe[0]), C.uint32_t(len(exe))) > 0 {
		path = C.GoString(&exe[0])
	}
	// The arguments stay here: they can hold secrets, so only the program
	// they name leaves the server.
	p.Program = programOf(p.Name, path, procArgs(pid))
	var cwd [C.MAXPATHLEN]C.char
	if C.hive_proc_cwd(C.int(pid), &cwd[0], C.int(len(cwd))) == 0 {
		p.Cwd = C.GoString(&cwd[0])
	}
	return p, nil
}
