// Copyright 2022 CloudWeGo Authors
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

//go:build darwin || netbsd || freebsd || openbsd || dragonfly || linux
// +build darwin netbsd freebsd openbsd dragonfly linux

package netpoll

import (
	"errors"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// CreateListener return a new Listener.
func CreateListener(network, addr string) (Listener, error) {
	var l net.Listener
	var err error
	switch network {
	case "udp":
		// TODO: udp listener.
		l, err = udpListener(network, addr)
	case "unix": // uds
		l, err = netListener(network, addr)
	default: // tcp, tcp4, tcp6
		if smcEnable {
			l, err = smcListener(network, addr)
		} else {
			l, err = netListener(network, addr)
		}
	}
	if err != nil {
		return nil, err
	}
	return ConvertListener(l)
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
	return ln, syscall.SetNonblock(ln.fd, true)
}

func netListener(network, addr string) (net.Listener, error) {
	return net.Listen(network, addr)
}

func smcListener(network, addr string) (net.Listener, error) {
	var (
		protoOpt int
		sockaddr unix.Sockaddr
	)
	tcpaddr, err := net.ResolveTCPAddr(network, addr)
	ipv4, ipv6 := tcpaddr.IP.To4(), tcpaddr.IP.To16()
	if ipv4 != nil || ipv4 == nil && ipv6 == nil {
		protoOpt = SMCProtoIPv4
		sockaddr4 := &unix.SockaddrInet4{}
		sockaddr4.Port = tcpaddr.Port
		copy(sockaddr4.Addr[:], ipv4[:net.IPv4len])
		sockaddr = sockaddr4
	} else {
		protoOpt = SMCProtoIPv6
		sockaddr6 := &unix.SockaddrInet6{}
		sockaddr6.Port = tcpaddr.Port
		copy(sockaddr6.Addr[:], ipv6[:net.IPv6len])
		sockaddr = sockaddr6
	}

	fd, err := unix.Socket(unix.AF_SMC, unix.SOCK_STREAM, protoOpt)
	if err != nil {
		return nil, err
	}

	err = unix.Bind(fd, sockaddr)
	if err != nil {
		return nil, err
	}

	err = unix.Listen(fd, unix.SOMAXCONN)
	if err != nil {
		return nil, err
	}

	f := os.NewFile(uintptr(fd), "")
	return net.FileListener(f)
}

// TODO: udpListener does not work now.
func udpListener(network, addr string) (l Listener, err error) {
	ln := &listener{}
	ln.pconn, err = net.ListenPacket(network, addr)
	if err != nil {
		return nil, err
	}
	ln.addr = ln.pconn.LocalAddr()
	switch pconn := ln.pconn.(type) {
	case *net.UDPConn:
		ln.file, err = pconn.File()
	}
	if err != nil {
		return nil, err
	}
	ln.fd = int(ln.file.Fd())
	return ln, syscall.SetNonblock(ln.fd, true)
}

var _ net.Listener = &listener{}

type listener struct {
	fd    int
	addr  net.Addr       // listener's local addr
	ln    net.Listener   // tcp|unix listener
	pconn net.PacketConn // udp listener
	file  *os.File
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
	nfd.localAddr = ln.addr
	nfd.network = ln.addr.Network()
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
	return nil
}

// Addr implements Listener.
func (ln *listener) Addr() net.Addr {
	return ln.addr
}

// Fd implements Listener.
func (ln *listener) Fd() (fd int) {
	return ln.fd
}

func (ln *listener) parseFD() (err error) {
	switch netln := ln.ln.(type) {
	case *net.TCPListener:
		ln.file, err = netln.File()
	case *net.UnixListener:
		ln.file, err = netln.File()
	default:
		return errors.New("listener type can't support")
	}
	if err != nil {
		return err
	}
	ln.fd = int(ln.file.Fd())
	return nil
}
