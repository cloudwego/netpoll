package netpoll

import (
	"context"

	skt "github.com/mdlayher/socket"
	"golang.org/x/sys/unix"
)

// VSockConnection implements Connection.
type VSockConnection struct {
	connection
}

// newVSockConnection wraps *VSockConnection.
func newVSockConnection(conn Conn) (connection *VSockConnection, err error) {
	connection = &VSockConnection{}
	err = connection.init(conn, nil)
	if err != nil {
		return nil, err
	}
	return connection, nil
}

func vSockSocket(ctx context.Context, cid, port uint32) (*netFD, error) {
	c, err := skt.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0, "vsock", nil)
	if err != nil {
		return nil, err
	}

	sa := &unix.SockaddrVM{CID: cid, Port: port}
	rsa, err := c.Connect(ctx, sa)
	if err != nil {
		_ = c.Close()
		return nil, err
	}

	// TODO(mdlayher): getpeername(2) appears to return nil in the GitHub CI
	// environment, so in the event of a nil sockaddr, fall back to the previous
	// method of synthesizing the remote address.
	if rsa == nil {
		rsa = sa
	}

	lsa, err := c.Getsockname()
	if err != nil {
		_ = c.Close()
		return nil, err
	}

	lsavm := lsa.(*unix.SockaddrVM)
	rsavm := rsa.(*unix.SockaddrVM)
	conn := &netFD{
		fd: int(c.File().Fd()),
		//localAddr:  lsavm,
		//remoteAddr: rsavm,
	}
	_ = lsavm
	_ = rsavm
	return conn, nil
}
