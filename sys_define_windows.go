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
	"os"
	"syscall"
	"unsafe"
)

type iovec = syscall.WSABuf
type fdtype = syscall.Handle

const SEND_RECV_AGAIN = WSAEWOULDBLOCK

var ws2_32_mod = syscall.NewLazyDLL("ws2_32.dll")

var recvProc = ws2_32_mod.NewProc("recv")
var sendProc = ws2_32_mod.NewProc("send")
var acceptProc = ws2_32_mod.NewProc("accept")
var ioctlsocketProc = ws2_32_mod.NewProc("ioctlsocket")
var getsockoptProc = ws2_32_mod.NewProc("getsockopt")

const (
	SO_ERROR                     = 0x4
	FIONBIO                      = 0x8004667e
	WSAEWOULDBLOCK syscall.Errno = 10035
	WSAEALREADY    syscall.Errno = 10037
	WSAEINPROGRESS syscall.Errno = 10036
)

func sysRead(fd fdtype, p []byte) (n int, err error) {
	rnu, _, err := recvProc.Call(uintptr(fd), uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)), 0)
	rn := int32(rnu)
	if rn <= 0 {
		return int(rn), err
	}
	return int(rn), nil
}

func sysWrite(fd fdtype, p []byte) (n int, err error) {
	wnu, _, err := sendProc.Call(uintptr(fd), uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)), 0)
	wn := int(wnu)
	if wn <= 0 {
		return wn, err
	}
	return wn, nil
}

func sysSetNonblock(fd fdtype, is bool) error {
	imode := 0
	if is {
		imode = 1
	}
	r, _, err := ioctlsocketProc.Call(uintptr(fd), FIONBIO, uintptr(unsafe.Pointer(&imode)))
	if r != 0 {
		return err
	}
	return nil
}

func sysGetsockoptInt(fd fdtype, level int, optname int) (int, error) {
	out := 1
	len := unsafe.Sizeof(out)
	nerr, _, err := getsockoptProc.Call(uintptr(fd), uintptr(level), uintptr(optname), uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&len)))
	if nerr != 0 {
		return out, os.NewSyscallError("getsockopt", err)
	}
	return out, nil
}

func isChanClose(ch chan int) bool {
	select {
	case _, received := <-ch:
		return !received
	default:
	}
	return false
}
