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
	"syscall"
	"unsafe"
)

type iovec = syscall.WSABuf
type fdtype = syscall.Handle

var ws2_32_mod = syscall.NewLazyDLL("ws2_32.dll")
var recvProc = ws2_32_mod.NewProc("recv")
var sendProc = ws2_32_mod.NewProc("send")
var acceptProc = ws2_32_mod.NewProc("accept")
var ioctlsocketProc = ws2_32_mod.NewProc("ioctlsocket")

const (
	SO_ERROR                     = 0x4
	FIONBIO                      = 0x8004667e
	WSAEWOULDBLOCK syscall.Errno = 10035
)

func sysRead(fd fdtype, p []byte) (n int, err error) {
	rnu, _, err := recvProc.Call(uintptr(fd), uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)), 0)
	rn := int(rnu)
	if rn <= 0 {
		return rn, err
	}
	return rn, nil
}

func sysWrite(fd fdtype, p []byte) (n int, err error) {
	wnu, _, err := sendProc.Call(uintptr(fd), uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)), 0)
	wn := int(wnu)
	if wn <= 0 {
		return wn, err
	}
	return wn, nil
}
