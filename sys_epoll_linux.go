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

//go:build !arm64
// +build !arm64

package netpoll

import (
	"syscall"
	"unsafe"
)

const EPOLLET = -syscall.EPOLLET

type epollevent struct {
	events uint32
	data   [8]byte // unaligned uintptr
}

// EpollCtl implements epoll_ctl.
func EpollCtl(epfd int, op int, fd int, event *epollevent) (err error) {
	_, _, err = syscall.RawSyscall6(syscall.SYS_EPOLL_CTL, uintptr(epfd), uintptr(op), uintptr(fd), uintptr(unsafe.Pointer(event)), 0, 0)
	if err == syscall.Errno(0) {
		err = nil
	}
	return err
}

//go:noescape
func epollwait(epfd int32, ev *epollevent, nev, timeout int32) int32

//go:noescape
func epollwaitblocking(epfd int32, ev *epollevent, nev, timeout int32) int32

//go:nosplit
func EpollWait(epfd int, events []epollevent, msec int) (n int, err error) {
	_n := epollwait(int32(epfd), &events[0], int32(len(events)), int32(msec))
	if _n < 0 {
		return 0, syscall.Errno(-n)
	}
	return int(_n), nil
}

//go:nosplit
func EpollWaitBlocking(epfd int, events []epollevent, msec int) (n int, err error) {
	_n := epollwaitblocking(int32(epfd), &events[0], int32(len(events)), int32(msec))
	if _n < 0 {
		return 0, syscall.Errno(-n)
	}
	return int(_n), nil
}
