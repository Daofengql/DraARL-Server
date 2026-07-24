//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package udphub

import (
	"net"

	"golang.org/x/sys/unix"
)

func readUDPSocketBufferSizes(conn *net.UDPConn) (readSize, writeSize int, err error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var controlErr error
	err = raw.Control(func(fd uintptr) {
		readSize, controlErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF)
		if controlErr != nil {
			return
		}
		writeSize, controlErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SNDBUF)
	})
	if err != nil {
		return 0, 0, err
	}
	return readSize, writeSize, controlErr
}
