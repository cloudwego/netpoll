package netpoll

import (
	"log"
)

const Trace = true

func trace(c *connection, format string, args ...interface{}) {
	if c != nil {
		format = "NETPOLL: rip=%s://%s, fd=%d - " + format
		v := make([]interface{}, 0, 3+len(args))
		v = append(v, c.network, c.RemoteAddr(), c.Fd())
		v = append(v, args...)
		args = v
	} else {
		format = "NETPOLL: " + format
	}
	log.Printf(format, args...)
}
