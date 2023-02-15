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

//go:build !windows
// +build !windows

package netpoll

import (
	"math"
	"os"
	"syscall"
	"unsafe"
)

// GetSysFdPairs creates and returns the fds of a pair of sockets.
func GetSysFdPairs() (r, w int) {
	fds, _ := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	return fds[0], fds[1]
}

// setTCPNoDelay set the TCP_NODELAY flag on socket
func setTCPNoDelay(fd int, b bool) (err error) {
	return syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, boolint(b))
}

// Wrapper around the socket system call that marks the returned file
// descriptor as nonblocking and close-on-exec.
func sysSocket(family, sotype, proto int) (int, error) {
	// See ../syscall/exec_unix.go for description of ForkLock.
	syscall.ForkLock.RLock()
	s, err := syscall.Socket(family, sotype, proto)
	if err == nil {
		syscall.CloseOnExec(s)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return -1, os.NewSyscallError("socket", err)
	}
	if err = syscall.SetNonblock(s, true); err != nil {
		syscall.Close(s)
		return -1, os.NewSyscallError("setnonblock", err)
	}
	return s, nil
}

const barriercap = 32

type barrier struct {
	bs  [][]byte
	ivs []syscall.Iovec
}

// writev wraps the writev system call.
func writev(fd int, bs [][]byte, ivs []syscall.Iovec) (n int, err error) {
	iovLen := iovecs(bs, ivs)
	if iovLen == 0 {
		return 0, nil
	}
	// syscall
	r, _, e := syscall.RawSyscall(syscall.SYS_WRITEV, uintptr(fd), uintptr(unsafe.Pointer(&ivs[0])), uintptr(iovLen))
	resetIovecs(bs, ivs[:iovLen])
	if e != 0 {
		return int(r), syscall.Errno(e)
	}
	return int(r), nil
}

// readv wraps the readv system call.
// return 0, nil means EOF.
func readv(fd int, bs [][]byte, ivs []syscall.Iovec) (n int, err error) {
	iovLen := iovecs(bs, ivs)
	if iovLen == 0 {
		return 0, nil
	}
	// syscall
	r, _, e := syscall.RawSyscall(syscall.SYS_READV, uintptr(fd), uintptr(unsafe.Pointer(&ivs[0])), uintptr(iovLen))
	resetIovecs(bs, ivs[:iovLen])
	if e != 0 {
		return int(r), syscall.Errno(e)
	}
	return int(r), nil
}

// TODO: read from sysconf(_SC_IOV_MAX)? The Linux default is
//  1024 and this seems conservative enough for now. Darwin's
//  UIO_MAXIOV also seems to be 1024.
// iovecs limit length to 2GB(2^31)
func iovecs(bs [][]byte, ivs []syscall.Iovec) (iovLen int) {
	totalLen := 0
	for i := 0; i < len(bs); i++ {
		chunk := bs[i]
		l := len(chunk)
		if l == 0 {
			continue
		}
		ivs[iovLen].Base = &chunk[0]
		ivs[iovLen].SetLen(l)
		totalLen += l
		iovLen++
	}
	// iovecs limit length to 2GB(2^31)
	if totalLen <= math.MaxInt32 {
		return iovLen
	}
	// reset here
	totalLen = math.MaxInt32
	for i := 0; i < iovLen; i++ {
		l := int(ivs[i].Len)
		if l < totalLen {
			totalLen -= l
			continue
		}
		ivs[i].SetLen(totalLen)
		iovLen = i + 1
		resetIovecs(nil, ivs[iovLen:])
		return iovLen
	}
	return iovLen
}

func resetIovecs(bs [][]byte, ivs []syscall.Iovec) {
	for i := 0; i < len(bs); i++ {
		bs[i] = nil
	}
	for i := 0; i < len(ivs); i++ {
		ivs[i].Base = nil
	}
}

// Boolean to int.
func boolint(b bool) int {
	if b {
		return 1
	}
	return 0
}
