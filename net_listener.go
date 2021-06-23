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

// +build darwin netbsd freebsd openbsd dragonfly linux

package netpoll

import (
	"net"
	"os"
	"syscall"
)

// Listener extends net.Listener, but supports getting the listener's fd.
type Listener interface {
	net.Listener

	// Fd return listener's fd, used by poll.
	Fd() (fd int)
}

// CreateListener return a new Listener.
func CreateListener(network, addr string) (l Listener, err error) {
	ln := &listener{
		network: network,
		addr:    addr,
	}

	defer func() {
		if err != nil {
			ln.Close()
		}
	}()

	if ln.network == "udp" {
		// TODO: udp listener.
		ln.pconn, err = net.ListenPacket(ln.network, ln.addr)
		if err != nil {
			return nil, err
		}
		ln.lnaddr = ln.pconn.LocalAddr()
		switch pconn := ln.pconn.(type) {
		case *net.UDPConn:
			ln.file, err = pconn.File()
		}
	} else {
		// tcp, tcp4, tcp6, unix
		ln.ln, err = net.Listen(ln.network, ln.addr)
		if err != nil {
			return nil, err
		}
		ln.lnaddr = ln.ln.Addr()
		switch netln := ln.ln.(type) {
		case *net.TCPListener:
			ln.file, err = netln.File()
		case *net.UnixListener:
			ln.file, err = netln.File()
		}
	}
	if err != nil {
		return nil, err
	}
	ln.fd = int(ln.file.Fd())
	return ln, syscall.SetNonblock(ln.fd, true)
}

var _ net.Listener = &listener{}

type listener struct {
	ln      net.Listener   // tcp|unix listener
	lnaddr  net.Addr       // listener's local addr
	pconn   net.PacketConn // udp listener
	file    *os.File
	fd      int
	network string // tcp, tcp4, tcp6, udp, udp4, udp6, unix
	addr    string
}

// Accept implements Listener.
func (ln *listener) Accept() (net.Conn, error) {
	// udp
	if ln.pconn != nil {
		return ln.UDPAccept()
	}
	// tcp
	var fd, sa, err = syscall.Accept(ln.fd)
	if err != nil {
		if err == syscall.EAGAIN {
			return nil, nil
		}
		return nil, err
	}
	var nfd = &netFD{}
	nfd.fd = fd
	nfd.network = ln.network
	nfd.localAddr = ln.lnaddr
	nfd.remoteAddr = sockaddrToAddr(sa)
	return nfd, nil
}

// TODO: UDPAccept Not implemented.
func (ln *listener) UDPAccept() (net.Conn, error) {
	return nil, Exception(ErrUnsupported, "UDP")
}

// Close implements Listener.
func (ln *listener) Close() error {
	if ln.fd != 0 {
		syscall.Close(ln.fd)
	}
	if ln.file != nil {
		ln.file.Close()
	}
	if ln.ln != nil {
		ln.ln.Close()
	}
	if ln.pconn != nil {
		ln.pconn.Close()
	}
	if ln.network == "unix" {
		os.RemoveAll(ln.addr)
	}
	return nil
}

// Addr implements Listener.
func (ln *listener) Addr() net.Addr {
	return ln.lnaddr
}

// Fd implements Listener.
func (ln *listener) Fd() (fd int) {
	return ln.fd
}
