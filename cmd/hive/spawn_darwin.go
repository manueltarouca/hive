package main

/*
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <spawn.h>
#include <stdlib.h>

// Starts the server in its own session, in /, stdout and stderr on logfd, and
// responsible for itself: macOS checks what its panes' programs may do (local
// network, screen recording, accessibility) against the server, which has its
// own name and usage text, not against whatever started it. Without that, a
// server started from Hive.app loses its grants once that app instance quits,
// and one started from a shell gets that shell's.
static int hive_spawn_daemon(const char *path, char **argv, char **envp, int logfd) {
	posix_spawnattr_t a;
	posix_spawn_file_actions_t fa;
	int r;
	if ((r = posix_spawnattr_init(&a)) != 0) return r;
	if ((r = posix_spawn_file_actions_init(&fa)) != 0) { posix_spawnattr_destroy(&a); return r; }
	int (*disclaim)(posix_spawnattr_t *, int) = dlsym(RTLD_DEFAULT, "responsibility_spawnattrs_setdisclaim");
	// The _np name: posix_spawn_file_actions_addchdir is new in macOS 26, hive runs on 14.
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
	if ((r = posix_spawnattr_setflags(&a, POSIX_SPAWN_SETSID)) == 0 &&
	    (!disclaim || (r = disclaim(&a, 1)) == 0) &&
	    (r = posix_spawn_file_actions_addopen(&fa, 0, "/dev/null", O_RDONLY, 0)) == 0 &&
	    (r = posix_spawn_file_actions_adddup2(&fa, logfd, 1)) == 0 &&
	    (r = posix_spawn_file_actions_adddup2(&fa, logfd, 2)) == 0 &&
	    (r = posix_spawn_file_actions_addchdir_np(&fa, "/")) == 0) {
		pid_t pid;
		r = posix_spawn(&pid, path, &fa, &a, argv, envp);
	}
#pragma clang diagnostic pop
	posix_spawn_file_actions_destroy(&fa);
	posix_spawnattr_destroy(&a);
	return r;
}
*/
import "C"

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// spawnDaemon starts the server in its own session, in /, writing to logf,
// responsible for itself (see hive_spawn_daemon).
func spawnDaemon(exe string, argv, env []string, logf *os.File) error {
	cs := func(ss []string) **C.char {
		p := C.malloc(C.size_t(len(ss)+1) * C.size_t(unsafe.Sizeof(uintptr(0))))
		a := unsafe.Slice((**C.char)(p), len(ss)+1)
		for i, s := range ss {
			a[i] = C.CString(s)
		}
		a[len(ss)] = nil
		return (**C.char)(p)
	}
	free := func(p **C.char, n int) {
		a := unsafe.Slice(p, n)
		for _, s := range a {
			C.free(unsafe.Pointer(s))
		}
		C.free(unsafe.Pointer(p))
	}
	path := C.CString(exe)
	defer C.free(unsafe.Pointer(path))
	cargv, cenv := cs(argv), cs(env)
	defer free(cargv, len(argv))
	defer free(cenv, len(env))
	// As os/exec does: no fd half-way to close-on-exec may leak into the server.
	syscall.ForkLock.Lock()
	r := C.hive_spawn_daemon(path, cargv, cenv, C.int(logf.Fd()))
	syscall.ForkLock.Unlock()
	if r != 0 {
		return fmt.Errorf("start the server: %w", os.NewSyscallError("posix_spawn", syscall.Errno(r)))
	}
	return nil
}
