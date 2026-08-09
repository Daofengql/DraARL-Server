//go:build !windows && !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package udphub

import (
	"errors"
	"net"
)

func readUDPSocketBufferSizes(_ *net.UDPConn) (int, int, error) {
	return 0, 0, errors.New("socket buffer inspection is unsupported on this platform")
}
