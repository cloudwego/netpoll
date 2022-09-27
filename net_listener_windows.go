// Copyright 2021 CloudWeGo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build windows

package netpoll

import (
	"errors"
	"net"
	"syscall"
	"unsafe"
)

// Listener extends net.Listener, but supports getting the listener's fd.
type Listener interface {
	net.Listener

	// Fd return listener's fd, used by poll.
	Fd() (fd fdtype)
}

// CreateListener return a new Listener.
func CreateListener(network, addr string) (l Listener, err error) {
	if network == "udp" {
		// TODO: udp listener.
		return udpListener(network, addr)
	}
	// tcp, tcp4, tcp6, unix
	ln, err := net.Listen(network, addr)
	if err != nil {
		return nil, err
	}
	return ConvertListener(ln)
}

// ConvertListener converts net.Listener to Listener
func ConvertListener(l net.Listener) (nl Listener, err error) {
	if tmp, ok := l.(Listener); ok {
		return tmp, nil
	}
	ln := &listener{}
	ln.ln = l
	ln.addr = l.Addr()
	err = ln.parseFD()
	if err != nil {
		return nil, err
	}
	return ln, sysSetNonblock(ln.fd, true)
}

// TODO: udpListener does not work now.
func udpListener(network, addr string) (l Listener, err error) {
	return nil, nil
}

var _ net.Listener = &listener{}

type listener struct {
	fd        fdtype
	addr      net.Addr       // listener's local addr
	ln        net.Listener   // tcp|unix listener
	pconn     net.PacketConn // udp listener
	rawConn   syscall.RawConn
	syncClose chan int
}

// Accept implements Listener.
func (ln *listener) Accept() (net.Conn, error) {
	// udp
	if ln.pconn != nil {
		return ln.UDPAccept()
	}
	// tcp
	var sa syscall.RawSockaddrAny
	var len = unsafe.Sizeof(sa)
	fduintptr, _, err := acceptProc.Call(uintptr(ln.fd), uintptr(unsafe.Pointer(&sa)), uintptr(unsafe.Pointer(&len)))
	fd := fdtype(fduintptr)
	if fd == syscall.InvalidHandle {
		if err == WSAEWOULDBLOCK {
			return nil, nil
		}
		return nil, err
	}
	var nfd = &netFD{}
	nfd.fd = fd
	nfd.localAddr = ln.addr
	nfd.network = ln.addr.Network()
	sa4, err := sa.Sockaddr()
	nfd.remoteAddr = sockaddrToAddr(sa4)
	return nfd, err
}

// TODO: UDPAccept Not implemented.
func (ln *listener) UDPAccept() (net.Conn, error) {
	return nil, Exception(ErrUnsupported, "UDP")
}

// Close implements Listener.
func (ln *listener) Close() error {
	ln.syncClose <- 1
	if ln.fd != 0 {
		syscall.Close(ln.fd)
	}
	if ln.ln != nil {
		ln.ln.Close()
	}
	if ln.pconn != nil {
		ln.pconn.Close()
	}
	return nil
}

// Addr implements Listener.
func (ln *listener) Addr() net.Addr {
	return ln.addr
}

// Fd implements Listener.
func (ln *listener) Fd() (fd fdtype) {
	return ln.fd
}

func (ln *listener) parseFD() (err error) {
	switch netln := ln.ln.(type) {
	case *net.TCPListener:
		ln.rawConn, err = netln.SyscallConn()
	case *net.UnixListener:
		ln.rawConn, err = netln.SyscallConn()
	default:
		return errors.New("listener type can't support")
	}
	if err != nil {
		return err
	}
	fdCh := make(chan uintptr, 1)
	ln.syncClose = make(chan int)
	go func() {
		ln.rawConn.Control(func(fd uintptr) {
			fdCh <- fd
			<-ln.syncClose
		})
	}()
	ln.fd = fdtype(<-fdCh)
	return nil
}
