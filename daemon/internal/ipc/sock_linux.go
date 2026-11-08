package ipc

import (
	"fmt"
	"net"
	"syscall"
)

// ShellUID is the uid of `adb shell`; both daemon and helper run as it.
const ShellUID = 2000

// ListenAbstract listens on the abstract unix socket @name. Abstract sockets
// have no filesystem permissions, so callers must check PeerCred.
func ListenAbstract(name string) (*net.UnixListener, error) {
	return net.ListenUnix("unix", &net.UnixAddr{Name: "@" + name, Net: "unix"})
}

// PeerCred returns the uid and pid of the process on the other end.
func PeerCred(c *net.UnixConn) (uid uint32, pid int32, err error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if credErr != nil {
		return 0, 0, fmt.Errorf("SO_PEERCRED: %w", credErr)
	}
	return cred.Uid, cred.Pid, nil
}
