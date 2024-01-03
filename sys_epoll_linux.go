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

//go:build linux
// +build linux

package netpoll

import (
	"syscall"
	"unsafe"
)

//go:linkname entersyscallblock runtime.entersyscallblock
func entersyscallblock()

//go:linkname exitsyscall runtime.exitsyscall
func exitsyscall()

//go:nosplit
func callEntersyscallblock() {
	entersyscallblock()
}

//go:nosplit
func callExitsyscall() {
	exitsyscall()
}

//go:noescape
func BlockSyscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, errno uintptr)

// EpollCreate implements epoll_create1.
func EpollCreate(flag int) (fd int, err error) {
	var r0 uintptr
	r0, _, err = syscall.RawSyscall(syscall.SYS_EPOLL_CREATE1, uintptr(flag), 0, 0)
	if err == syscall.Errno(0) {
		err = nil
	}
	return int(r0), err
}

// EpollCtl implements epoll_ctl.
func EpollCtl(epfd int, op int, fd int, event *epollevent) (err error) {
	_, _, err = syscall.RawSyscall6(syscall.SYS_EPOLL_CTL, uintptr(epfd), uintptr(op), uintptr(fd), uintptr(unsafe.Pointer(event)), 0, 0)
	if err == syscall.Errno(0) {
		err = nil
	}
	return err
}

// EpollWait implements epoll_wait.
func EpollWait(epfd int, events []epollevent, msec int) (n int, err error) {
	var r0 uintptr
	r0, _, err = syscall.Syscall6(SYS_EPOLL_WAIT, uintptr(epfd), uintptr(unsafe.Pointer(&events[0])), uintptr(len(events)), uintptr(msec), 0, 0)
	if err == syscall.Errno(0) {
		err = nil
	}
	return int(r0), err
}

func EpollWaitRaw(epfd int, events []epollevent, msec int) (n int, err error) {
	var r0 uintptr
	r0, _, err = syscall.RawSyscall6(SYS_EPOLL_WAIT, uintptr(epfd), uintptr(unsafe.Pointer(&events[0])), uintptr(len(events)), uintptr(msec), 0, 0)
	if err == syscall.Errno(0) {
		err = nil
	}
	return int(r0), err
}

func EpollWaitBlock(epfd int, events []epollevent, msec int) (n int, err error) {
	r0, _, errno := BlockSyscall6(SYS_EPOLL_WAIT, uintptr(epfd), uintptr(unsafe.Pointer(&events[0])), uintptr(len(events)), uintptr(msec), 0, 0)
	if errno == 0 {
		err = nil
	} else {
		err = syscall.Errno(errno)
	}
	return int(r0), err
}
