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

package netpoll

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func init() {
	var startData syscall.WSAData
	var version uint32 = 0x0202
	syscall.WSAStartup(version, &startData)
}

func inet_addr(ipaddr string) [4]byte {
	var (
		ips = strings.Split(ipaddr, ".")
		ip  [4]uint64
		ret [4]byte
	)
	for i := 0; i < 4; i++ {
		ip[i], _ = strconv.ParseUint(ips[i], 10, 8)
	}
	for i := 0; i < 4; i++ {
		ret[i] = byte(ip[i])
	}
	return ret
}

func socketPair(family int, typ int, protocol int) (fd [2]fdtype, err error) {
	var (
		listen_addr syscall.SockaddrInet4
	)
	if protocol != 0 || family != syscall.AF_INET {
		return fd, errors.New("protocol or family error")
	}
	listener, err := syscall.Socket(syscall.AF_INET, typ, 0)
	if err != nil {
		return fd, err
	}
	defer syscall.Closesocket(listener)

	listen_addr.Addr = inet_addr("127.0.0.1")
	listen_addr.Port = 0
	if err = syscall.Bind(listener, &listen_addr); err != nil {
		return fd, err
	}
	if err = syscall.Listen(listener, 1); err != nil {
		return fd, err
	}
	connector, err := syscall.Socket(syscall.AF_INET, typ, 0)
	if err != nil {
		return fd, err
	}
	connector_addr, err := syscall.Getsockname(listener)
	if err != nil {
		return fd, err
	}
	if err = syscall.Connect(connector, connector_addr); err != nil {
		return fd, err
	}
	acceptor, _, err := acceptProc.Call(uintptr(listener), 0, 0)
	if acceptor == 0 {
		return fd, err
	}
	fd[0] = connector
	fd[1] = fdtype(acceptor)
	return fd, nil
}

// GetSysFdPairs creates and returns the fds of a pair of sockets.
func GetSysFdPairs() (r, w fdtype) {
	fds, _ := socketPair(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	return fds[0], fds[1]
}

// setTCPNoDelay set the TCP_NODELAY flag on socket
func setTCPNoDelay(fd fdtype, b bool) (err error) {
	return syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, boolint(b))
}

// Wrapper around the socket system call that marks the returned file
// descriptor as nonblocking and close-on-exec.
func sysSocket(family, sotype, proto int) (fdtype, error) {
	// See ../syscall/exec_unix.go for description of ForkLock.
	syscall.ForkLock.RLock()
	s, err := syscall.Socket(family, sotype, proto)
	if err == nil {
		syscall.CloseOnExec(s)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return fdtype(0), os.NewSyscallError("socket", err)
	}
	if err = sysSetNonblock(s, true); err != nil {
		syscall.Close(s)
		return fdtype(0), os.NewSyscallError("setnonblock", err)
	}
	return s, nil
}

const barriercap = 32

type barrier struct {
	bs  [][]byte
	ivs []iovec
}

// writev wraps the writev system call.
func writev(fd fdtype, bs [][]byte, ivs []iovec) (n int, err error) {
	iovLen := iovecs(bs, ivs)
	if iovLen == 0 {
		return 0, nil
	}
	var (
		sendBytes  uint32
		overlapped syscall.Overlapped
	)
	e := syscall.WSASend(fd, &ivs[0], uint32(iovLen), &sendBytes, 0, &overlapped, nil)
	resetIovecs(bs, ivs[:iovLen])
	return int(sendBytes), e
}

// readv wraps the readv system call.
// return 0, nil means EOF.
func readv(fd fdtype, bs [][]byte, ivs []iovec) (n int, err error) {
	iovLen := iovecs(bs, ivs)
	if iovLen == 0 {
		return 0, nil
	}
	var (
		recvBytes  uint32
		overlapped syscall.Overlapped
		flags      uint32
	)
	e := syscall.WSARecv(fd, &ivs[0], uint32(iovLen), &recvBytes, &flags, &overlapped, nil)
	resetIovecs(bs, ivs[:iovLen])
	return int(recvBytes), e
}

// TODO: read from sysconf(_SC_IOV_MAX)? The Linux default is
//  1024 and this seems conservative enough for now. Darwin's
//  UIO_MAXIOV also seems to be 1024.
func iovecs(bs [][]byte, ivs []iovec) (iovLen int) {
	for i := 0; i < len(bs); i++ {
		chunk := bs[i]
		if len(chunk) == 0 {
			continue
		}
		ivs[iovLen].Buf = &chunk[0]
		ivs[iovLen].Len = uint32(len(chunk))
		iovLen++
	}
	return iovLen
}

func resetIovecs(bs [][]byte, ivs []iovec) {
	for i := 0; i < len(bs); i++ {
		bs[i] = nil
	}
	for i := 0; i < len(ivs); i++ {
		ivs[i].Buf = nil
	}
}

// Boolean to int.
func boolint(b bool) int {
	if b {
		return 1
	}
	return 0
}
