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

package uring

import (
	"math"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// SysRegister registers user buffers or files for use in an io_uring(7) instance referenced by fd.
// Registering files or user buffers allows the kernel to take long term references to internal data structures
// or create long term mappings of application memory, greatly reducing per-I/O overhead.
func SysRegister(ringFd int, op int, arg unsafe.Pointer, nrArgs int) error {
	_, _, err := syscall.Syscall6(SYS_IO_URING_REGISTER, uintptr(ringFd), uintptr(op), uintptr(arg), uintptr(nrArgs), 0, 0)
	if err != 0 {
		return os.NewSyscallError("io_uring_register", err)
	}
	return nil
}

// SysSetUp sets up a SQ and CQ with at least entries entries, and
// returns a file descriptor which can be used to perform subsequent operations on the io_uring instance.
// The SQ and CQ are shared between userspace and the kernel, which eliminates the need to copy data when initiating and completing I/O.
func SysSetUp(entries uint32, params *ringParams) (int, error) {
	p, _, err := syscall.Syscall(SYS_IO_URING_SETUP, uintptr(entries), uintptr(unsafe.Pointer(params)), 0)
	if err != 0 {
		return int(p), os.NewSyscallError("io_uring_setup", err)
	}
	return int(p), nil
}

// SysEnter is used to initiate and complete I/O using the shared SQ and CQ setup by a call to io_uring_setup(2).
// A single call can both submit new I/O and wait for completions of I/O initiated by this call or previous calls to io_uring_enter().
func SysEnter(fd int, toSubmit uint32, minComplete uint32, flags uint32, sig unsafe.Pointer, sz int) (uint, error) {
	p, _, err := syscall.Syscall6(SYS_IO_URING_ENTER, uintptr(fd), uintptr(toSubmit), uintptr(minComplete), uintptr(flags), uintptr(unsafe.Pointer(sig)), uintptr(sz))
	if err != 0 {
		return 0, os.NewSyscallError("iouring_enter", err)
	}
	return uint(p), nil
}

// _sizeU32 is size of uint32
const _sizeU32 = unsafe.Sizeof(uint32(0))

// _sizeUR is size of URing
const _sizeUR = unsafe.Sizeof(URing{})

// _sizeCQE is size of URingCQE
const _sizeCQE = unsafe.Sizeof(URingCQE{})

// _sizeSQE is size of URingSQE
const _sizeSQE = unsafe.Sizeof(URingSQE{})

// _sizeEventsArg is size of eventsArg
const _sizeEventsArg = unsafe.Sizeof(eventsArg{})

// Init system call numbers
const (
	SYS_IO_URING_SETUP    = 425
	SYS_IO_URING_ENTER    = 426
	SYS_IO_URING_REGISTER = 427

	NSIG = 64
)

// Flags of uringSQ
const (
	// IORING_SQ_NEED_WAKEUP means needs io_uring_enter wakeup
	IORING_SQ_NEED_WAKEUP uint32 = 1 << iota
	// IORING_SQ_CQ_OVERFLOW means CQ ring is overflown
	IORING_SQ_CQ_OVERFLOW
	// IORING_SQ_TASKRUN means task should enter the kernel
	IORING_SQ_TASKRUN
)

// Flags of uringCQ
// IORING_CQ_EVENTFD_DISABLED means disable eventfd notifications
const IORING_CQ_EVENTFD_DISABLED uint32 = 1 << iota

const INT_FLAG_REG_RING = 1
const LIBURING_UDATA_TIMEOUT = math.MaxUint64

func WRITE_ONCE_U32(p *uint32, v uint32) {
	atomic.StoreUint32(p, v)
}

func READ_ONCE_U32(p *uint32) uint32 {
	return atomic.LoadUint32(p)
}

func SMP_STORE_RELEASE_U32(p *uint32, v uint32) {
	atomic.StoreUint32(p, v)
}

func SMP_LOAD_ACQUIRE_U32(p *uint32) uint32 {
	return atomic.LoadUint32(p)
}
