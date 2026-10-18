package remote

import (
	"net"

	"golang.org/x/sys/unix"
)

func peerUID(c *net.UnixConn) (uint32, error) {
	rc, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var gerr error
	if err := rc.Control(func(fd uintptr) {
		var u *unix.Ucred
		if u, gerr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED); gerr == nil {
			uid = u.Uid
		}
	}); err != nil {
		return 0, err
	}
	return uid, gerr
}
