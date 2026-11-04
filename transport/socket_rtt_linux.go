//go:build linux

package transport

import (
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// socketRTT is the kernel's smoothed round trip (TCP_INFO tcpi_rtt) on the TCP
// socket under c, or zero when c has none or the kernel does not answer.
func socketRTT(c net.Conn) time.Duration {
	tcp, ok := innermostTCP(c)
	if !ok {
		return 0
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return 0
	}
	var info *unix.TCPInfo
	var infoErr error
	if err := raw.Control(func(fd uintptr) {
		info, infoErr = unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
	}); err != nil || infoErr != nil {
		return 0
	}
	return time.Duration(info.Rtt) * time.Microsecond
}

// innermostTCP unwraps the proxy dial's wrappers down to the TCP socket.
func innermostTCP(c net.Conn) (*net.TCPConn, bool) {
	for {
		switch conn := c.(type) {
		case *net.TCPConn:
			return conn, true
		case *bufferedConn:
			c = conn.Conn
		case *SpeculativeConn:
			c = conn.Conn
		default:
			return nil, false
		}
	}
}
