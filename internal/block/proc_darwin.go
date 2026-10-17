package block

/*
#include <libproc.h>
#include <sys/proc_info.h>
#include <string.h>

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
	"fmt"
	"unsafe"
)

func describe(pid int) (Process, error) {
	var name [256]C.char
	if C.proc_name(C.int(pid), unsafe.Pointer(&name[0]), C.uint32_t(len(name))) <= 0 {
		return Process{}, fmt.Errorf("no process %d", pid)
	}
	p := Process{PID: pid, Name: C.GoString(&name[0])}
	var cwd [C.MAXPATHLEN]C.char
	if C.hive_proc_cwd(C.int(pid), &cwd[0], C.int(len(cwd))) == 0 {
		p.Cwd = C.GoString(&cwd[0])
	}
	return p, nil
}
