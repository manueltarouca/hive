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
		var x *unix.Xucred
		if x, gerr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED); gerr == nil {
			uid = x.Uid
		}
	}); err != nil {
		return 0, err
	}
	return uid, gerr
}
