//go:build !linux

package ipc

import (
	"errors"
	"net"
)

const ShellUID = 2000

var errUnsupported = errors.New("ipc: abstract unix sockets need Linux/Android")

func ListenAbstract(string) (*net.UnixListener, error) { return nil, errUnsupported }
func PeerCred(*net.UnixConn) (uint32, int32, error)    { return 0, 0, errUnsupported }
