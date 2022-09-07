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
	"errors"
	"syscall"
	"time"
	"unsafe"
)

type getData struct {
	submit   uint32
	waitNr   uint32
	getFlags uint32
	sz       int
	arg      unsafe.Pointer
}

type eventsArg struct {
	sigMask   uintptr
	sigMaskSz uint32
	_pad      uint32
	ts        uintptr
}

// getCQE implements URing
func (u *URing) getCQE(data getData) (cqe *URingCQE, err error) {
	for {
		var looped, needEnter bool
		var flags, nrAvail uint32

		nrAvail, cqe, err = u.peekCQE()

		if err != nil {
			break
		}
		if cqe == nil && data.waitNr == 0 && data.submit == 0 {
			// If we already looped once, we already entererd
			// the kernel. Since there's nothing to submit or
			// wait for, don't keep retrying.
			if looped || u.caRingNeedEnter() {
				err = syscall.EAGAIN
				break
			}
			needEnter = true
		}

		if data.waitNr > nrAvail || nrAvail != 0 {
			flags = IORING_ENTER_GETEVENTS | data.getFlags
			needEnter = true
		}

		if data.submit != 0 && u.sqRingNeedEnter(&flags) {
			needEnter = true
		}
		if !needEnter {
			break
		}

		if u.Params.flags&INT_FLAG_REG_RING != 0 {
			flags |= IORING_ENTER_REGISTERED_RING
		}

		var ret uint
		ret, err = SysEnter(u.fd, data.submit, data.waitNr, flags, data.arg, data.sz)

		if err != nil {
			break
		}

		data.submit -= uint32(ret)
		if cqe != nil {
			break
		}
		looped = true

	}
	return
}

// getEventsArg implements URing
func getEventsArg(sigMask uintptr, sigMaskSz uint32, ts uintptr) *eventsArg {
	return &eventsArg{sigMask: sigMask, sigMaskSz: sigMaskSz, ts: ts}
}

// submitTimeout implements URing
func (u *URing) submitTimeout(timeout time.Duration) (uint32, error) {
	sqe, err := u.nextSQE()
	if err != nil {
		_, err = u.Submit()
		if err != nil {
			return 0, err
		}
		sqe, err = u.nextSQE()
		if err != nil {
			return uint32(syscall.EAGAIN), err
		}
	}
	Timeout(timeout).Prep(sqe)
	sqe.setData(LIBURING_UDATA_TIMEOUT)
	return u.flushSQ(), nil
}

// peekCQE implements URing
func (u *URing) peekCQE() (avail uint32, cqe *URingCQE, err error) {
	mask := *u.cqRing.kRingMask

	var shift int
	if u.Params.flags&IORING_SETUP_CQE32 != 0 {
		shift = 1
	}

	for {
		tail := SMP_LOAD_ACQUIRE_U32(u.cqRing.kTail)
		head := SMP_LOAD_ACQUIRE_U32(u.cqRing.kHead)

		avail = tail - head
		if avail == 0 {
			break
		}
		cqe = (*URingCQE)(unsafe.Add(unsafe.Pointer(u.cqRing.cqes), uintptr((head&mask)<<shift)*_sizeUR))

		if !(u.Params.features&IORING_FEAT_EXT_ARG == 0) && cqe.UserData == LIBURING_UDATA_TIMEOUT {
			if cqe.Res < 0 {
				err = cqe.Error()
			}
			u.Advance(1)
			if err != nil {
				continue
			}
			err = nil
		}
	}

	return
}

// peekCQE implements URing
func (u *URing) peekBatchCQE(cqes []*URingCQE, shift int) int {
	ready := u.cqRing.ready()
	lenCQEs := uint32(len(cqes))
	var count uint32
	if lenCQEs > ready {
		count = ready
	} else {
		count = lenCQEs
	}
	if ready != 0 {
		head := SMP_LOAD_ACQUIRE_U32(u.cqRing.kHead)
		mask := SMP_LOAD_ACQUIRE_U32(u.cqRing.kRingMask)

		last := head + count
		for i := 0; head != last; head, i = head+1, i+1 {
			cqes[i] = (*URingCQE)(unsafe.Add(unsafe.Pointer(u.cqRing.cqes), uintptr((head&mask)<<shift)*_sizeCQE))
		}
	}
	return int(count)
}

// nextSQE implements URing
func (u *URing) nextSQE() (sqe *URingSQE, err error) {
	head := SMP_LOAD_ACQUIRE_U32(u.sqRing.kHead)
	next := u.sqRing.sqeTail + 1

	if *u.sqRing.kRingEntries >= next-head {
		idx := u.sqRing.sqeTail & *u.sqRing.kRingMask * uint32(_sizeSQE)
		sqe = (*URingSQE)(unsafe.Pointer(&u.sqRing.sqeBuff[idx]))
		u.sqRing.sqeTail = next
	} else {
		err = errors.New("sq ring overflow")
	}
	return
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
