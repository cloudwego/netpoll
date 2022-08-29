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
	"math"
	"runtime"
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

type getEventsArg struct {
	sigMask   uintptr
	sigMaskSz uint32
	_pad      uint32
	ts        uintptr
}

// WaitCQE implements URing, it returns an I/O CQE, waiting for it if necessary
func (u *URing) WaitCQE() (cqe *URingCQE, err error) {
	return u.WaitCQENr(1)
}

// WaitCQENr implements URing, it returns an I/O CQE, waiting for nr completions if one isn’t readily
func (u *URing) WaitCQENr(nr uint32) (cqe *URingCQE, err error) {
	return u.getCQE(getData{
		submit: 0,
		waitNr: nr,
		arg:    unsafe.Pointer(nil),
	})
}

// WaitCQEs implements URing, like WaitCQE() except it accepts a timeout value as well.
// Note that an SQE is used internally to handle the timeout. Applications using this function
// must never set sqe->user_data to LIBURING_UDATA_TIMEOUT.
func (u *URing) WaitCQEs(nr uint32, timeout time.Duration) (*URingCQE, error) {
	var toSubmit uint32

	if u.Params.flags&IORING_FEAT_EXT_ARG != 0 {
		return u.WaitCQEsNew(nr, timeout)
	}
	toSubmit, err := u.submitTimeout(timeout)
	if toSubmit == 0 {
		return nil, err
	}
	return u.getCQE(getData{
		submit: toSubmit,
		waitNr: nr,
		arg:    unsafe.Pointer(nil),
		sz:     NSIG / 8,
	})
}

// WaitCQETimeout implements URing, returns an I/O completion,
// if one is readily available. Doesn’t wait.
func (u *URing) WaitCQETimeout(timeout time.Duration) (cqe *URingCQE, err error) {
	return u.WaitCQEs(1, timeout)
}

// PeekBatchCQE implements URing, it fills in an array of I/O CQE up to count,
// if they are available, returning the count of completions filled.
// Does not wait for completions. They have to be already available for them to be returned by this function.
func (u *URing) PeekBatchCQE(cqes []*URingCQE) int {
	var shift int
	if u.Params.flags&IORING_SETUP_CQE32 != 0 {
		shift = 1
	}

	n := u.peekBatchCQE(cqes, shift)

	if n == 0 && u.cqRingNeedFlush() {
		sysEnter(u.fd, 0, 0, IORING_ENTER_GETEVENTS, nil)
		n = u.peekBatchCQE(cqes, shift)
	}

	return n
}

// CQESeen implements URing, it must be called after PeekCQE() or WaitCQE()
// and after the cqe has been processed by the application.
func (u *URing) CQESeen() {
	if u.cqRing.cqes != nil {
		u.Advance(1)
	}
}

// GetEventsArg implements URing
func GetEventsArg(sigMask uintptr, sigMaskSz uint32, ts uintptr) *getEventsArg {
	return &getEventsArg{sigMask: sigMask, sigMaskSz: sigMaskSz, ts: ts}
}

// WaitCQEsNew implements URing
func (u *URing) WaitCQEsNew(nr uint32, timeout time.Duration) (cqe *URingCQE, err error) {
	ts := syscall.NsecToTimespec(timeout.Nanoseconds())
	arg := GetEventsArg(uintptr(unsafe.Pointer(nil)), NSIG/8, uintptr(unsafe.Pointer(&ts)))

	cqe, err = u.getCQE(getData{
		submit:   0,
		waitNr:   nr,
		getFlags: IORING_ENTER_EXT_ARG,
		arg:      unsafe.Pointer(arg),
		sz:       int(unsafe.Sizeof(getEventsArg{})),
	})

	runtime.KeepAlive(arg)
	runtime.KeepAlive(ts)
	return
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
		ret, err = sysEnter6(u.fd, data.submit, data.waitNr, flags, data.arg, data.sz)

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
		cqe = (*URingCQE)(unsafe.Add(unsafe.Pointer(u.cqRing.cqes), uintptr((head&mask)<<shift)*unsafe.Sizeof(URing{})))

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
	count := min(uint32(len(cqes)), ready)
	if ready != 0 {
		head := SMP_LOAD_ACQUIRE_U32(u.cqRing.kHead)
		mask := SMP_LOAD_ACQUIRE_U32(u.cqRing.kRingMask)

		last := head + count
		for i := 0; head != last; head, i = head+1, i+1 {
			cqes[i] = (*URingCQE)(unsafe.Add(unsafe.Pointer(u.cqRing.cqes), uintptr((head&mask)<<shift)*unsafe.Sizeof(URingCQE{})))
		}
	}
	return int(count)
}

const INT_FLAG_REG_RING = 1
const LIBURING_UDATA_TIMEOUT = math.MaxUint64
