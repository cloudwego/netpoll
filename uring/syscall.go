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

package uring

import (
	"os"
	"syscall"
	"unsafe"
)

// sysRegister registers user buffers or files for use in an io_uring(7) instance referenced by fd.
// Registering files or user buffers allows the kernel to take long term references to internal data structures
// or create long term mappings of application memory, greatly reducing per-I/O overhead.
func sysRegister(ringFd int, op int, arg unsafe.Pointer, nrArgs int) error {
	_, _, err := syscall.Syscall6(SYS_IO_URING_REGISTER, uintptr(ringFd), uintptr(op), uintptr(arg), uintptr(nrArgs), 0, 0)
	if err != 0 {
		return os.NewSyscallError("io_uring_register", err)
	}
	return nil
}

// sysSetUp sets up a SQ and CQ with at least entries entries, and
// returns a file descriptor which can be used to perform subsequent operations on the io_uring instance.
// The SQ and CQ are shared between userspace and the kernel, which eliminates the need to copy data when initiating and completing I/O.
func sysSetUp(entries uint32, params *ringParams) (int, error) {
	p, _, err := syscall.Syscall(SYS_IO_URING_SETUP, uintptr(entries), uintptr(unsafe.Pointer(params)), uintptr(0))
	if err != 0 {
		return int(p), os.NewSyscallError("io_uring_setup", err)
	}
	return int(p), err
}

// sysEnter is used to initiate and complete I/O using the shared SQ and CQ setup by a call to io_uring_setup(2).
// A single call can both submit new I/O and wait for completions of I/O initiated by this call or previous calls to io_uring_enter().
func sysEnter(fd int, toSubmit uint32, minComplete uint32, flags uint32, sig unsafe.Pointer) (uint, error) {
	return sysEnter6(fd, toSubmit, minComplete, flags, sig, NSIG/8)
}

func sysEnter6(fd int, toSubmit uint32, minComplete uint32, flags uint32, sig unsafe.Pointer, sz int) (uint, error) {
	p, _, err := syscall.Syscall6(SYS_IO_URING_ENTER, uintptr(fd), uintptr(toSubmit), uintptr(minComplete), uintptr(flags), uintptr(unsafe.Pointer(sig)), uintptr(sz))
	if err != 0 {
		return 0, os.NewSyscallError("iouring_enter", err)
	}
	if p == 0 {
		return 0, os.NewSyscallError("iouring_enter", syscall.Errno(-p))
	}
	return uint(p), err
}

func min(a, b uint32) uint32 {
	if a > b {
		return b
	}
	return a
}

//go:linkname sockaddr syscall.Sockaddr.sockaddr
func sockaddr(addr syscall.Sockaddr) (unsafe.Pointer, uint32, error)
