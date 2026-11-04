//go:build !linux

package transport

import (
	"net"
	"time"
)

// socketRTT reads TCP_INFO, which only Linux offers here.
func socketRTT(net.Conn) time.Duration { return 0 }
