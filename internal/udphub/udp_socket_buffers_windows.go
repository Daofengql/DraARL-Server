//go:build windows

package udphub

import (
	"net"

	"golang.org/x/sys/windows"
)

func readUDPSocketBufferSizes(conn *net.UDPConn) (readSize, writeSize int, err error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var controlErr error
	err = raw.Control(func(fd uintptr) {
		readSize, controlErr = windows.GetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_RCVBUF)
		if controlErr != nil {
			return
		}
		writeSize, controlErr = windows.GetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_SNDBUF)
	})
	if err != nil {
		return 0, 0, err
	}
	return readSize, writeSize, controlErr
}
