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
	"errors"
	"unsafe"
)

// URing means I/O Userspace Ring
type URing struct {
	cqRing *uringCQ
	sqRing *uringSQ

	fd int

	Params *ringParams
}

// uringSQ means Submit Queue
type uringSQ struct {
	buff    []byte
	sqeBuff []byte

	kHead        *uint32
	kTail        *uint32
	kRingMask    *uint32
	kRingEntries *uint32
	kFlags       *uint32
	kDropped     *uint32
	array        *uint32
	sqes         *URingSQE

	sqeHead uint32
	sqeTail uint32

	ringSize uint64
}

// uringCQ means Completion Queue
type uringCQ struct {
	buff   []byte
	kFlags uintptr

	kHead        *uint32
	kTail        *uint32
	kRingMask    *uint32
	kRingEntries *uint32
	kOverflow    *uint32
	cqes         *URingCQE

	ringSize uint64
}

// IOURing create new io_uring instance with Setup Options
func IOURing(entries uint32, ops ...setupOp) (r *URing, err error) {
	params := &ringParams{}
	for _, op := range ops {
		op(params)
	}
	fd, err := sysSetUp(entries, params)
	if err != nil {
		return nil, err
	}
	r = &URing{Params: params, fd: fd, sqRing: &uringSQ{}, cqRing: &uringCQ{}}
	err = r.sysMmap(params)

	return
}

// Fd will return fd of URing
func (u *URing) Fd() int {
	return u.fd
}

// Close implements URing
func (u *URing) Close() error {
	err := u.sysMunmap()
	return err
}

// Advance implements URing, it must be called after EachCQE()
func (u *URing) Advance(nr uint32) {
	if nr != 0 {
		// Ensure that the kernel only sees the new value of the head
		// index after the CQEs have been read.
		SMP_STORE_RELEASE_U32(u.cqRing.kHead, *u.cqRing.kHead+nr)
	}
}

// getSQE will return a submission queue entry that can be used to submit an I/O operation.
func (u *URing) getSQE() *URingSQE {
	return u.sqRing.sqes
}

// nextSQE implements URing
func (u *URing) nextSQE() (sqe *URingSQE, err error) {
	head := SMP_LOAD_ACQUIRE_U32(u.sqRing.kHead)
	next := u.sqRing.sqeTail + 1

	if *u.sqRing.kRingEntries >= next-head {
		idx := u.sqRing.sqeTail & *u.sqRing.kRingMask * uint32(unsafe.Sizeof(URingSQE{}))
		sqe = (*URingSQE)(unsafe.Pointer(&u.sqRing.sqeBuff[idx]))
		u.sqRing.sqeTail = next
	} else {
		err = errors.New("sq ring overflow")
	}
	return
}

// flushSQ implements URing
func (u *URing) flushSQ() uint32 {
	mask := *u.sqRing.kRingMask
	tail := SMP_LOAD_ACQUIRE_U32(u.sqRing.kTail)
	subCnt := u.sqRing.sqeTail - u.sqRing.sqeHead

	if subCnt == 0 {
		return tail - SMP_LOAD_ACQUIRE_U32(u.sqRing.kHead)
	}

	for i := subCnt; i > 0; i-- {
		*(*uint32)(unsafe.Add(unsafe.Pointer(u.sqRing.array), tail&mask*uint32(unsafe.Sizeof(uint32(0))))) = u.sqRing.sqeHead & mask
		tail++
		u.sqRing.sqeHead++
	}

	SMP_STORE_RELEASE_U32(u.sqRing.kTail, tail)

	return tail - SMP_LOAD_ACQUIRE_U32(u.sqRing.kHead)
}

// getProbe implements URing, it returns io_uring probe
func (u *URing) getProbe() (probe *Probe, err error) {
	probe = &Probe{}
	err = sysRegister(u.fd, IORING_REGISTER_PROBE, unsafe.Pointer(probe), 256)
	return
}

// registerProbe implements URing
func (r URing) registerProbe(p *Probe, nrOps int) error {
	err := sysRegister(r.fd, IORING_REGISTER_PROBE, unsafe.Pointer(p), nrOps)
	return err
}

// caRingNeedEnter implements URing
func (u *URing) caRingNeedEnter() bool {
	return u.Params.flags&IORING_SETUP_IOPOLL != 0 || u.cqRingNeedFlush()
}

// cqRingNeedFlush implements URing
func (u *URing) cqRingNeedFlush() bool {
	return READ_ONCE_U32(u.sqRing.kFlags)&(IORING_SQ_CQ_OVERFLOW|IORING_SQ_TASKRUN) != 0
}

// sqRingNeedEnter implements URing
func (u *URing) sqRingNeedEnter(flags *uint32) bool {
	if u.Params.flags&IORING_SETUP_SQPOLL == 0 {
		return true
	}
	if READ_ONCE_U32(u.sqRing.kFlags)&IORING_ENTER_SQ_WAKEUP != 0 {
		*flags |= IORING_ENTER_SQ_WAKEUP
		return true
	}
	return false
}

// ready implements URing
func (c *uringCQ) ready() uint32 {
	return SMP_LOAD_ACQUIRE_U32(c.kTail) - SMP_LOAD_ACQUIRE_U32(c.kHead)
}

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
