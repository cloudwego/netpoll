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
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// IOURing create new io_uring instance with Setup Options
func IOURing(entries uint32, ops ...setupOp) (u *URing, err error) {
	params := &ringParams{}
	for _, op := range ops {
		op(params)
	}
	fd, err := SysSetUp(entries, params)
	if err != nil {
		return nil, err
	}
	u = &URing{Params: params, fd: fd, sqRing: &uringSQ{}, cqRing: &uringCQ{}}
	err = u.sysMmap(params)

	return
}

// Fd will return fd of URing
func (u *URing) Fd() int {
	return u.fd
}

// SQE will return a submission queue entry that can be used to submit an I/O operation.
func (u *URing) SQE() *URingSQE {
	return u.sqRing.sqes
}

// Queue add an operation to SQ queue
func (u *URing) Queue(op Op, flags OpFlag, userData uint64) error {
	sqe, err := u.nextSQE()
	if err != nil {
		return err
	}

	op.Prep(sqe)
	sqe.setFlags(flags)
	sqe.setUserData(userData)

	return nil
}

// Probe implements URing, it returns io_uring probe
func (u *URing) Probe() (probe *Probe, err error) {
	probe = &Probe{}
	err = SysRegister(u.fd, IORING_REGISTER_PROBE, unsafe.Pointer(probe), 256)
	SMP_SQRING.Store(u.sqRing)
	return
}

// RegisterProbe implements URing
func (u URing) RegisterProbe(p *Probe, nrOps int) error {
	err := SysRegister(u.fd, IORING_REGISTER_PROBE, unsafe.Pointer(p), nrOps)
	SMP_SQRING.Store(u.sqRing)
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

// Close implements URing
func (u *URing) Close() error {
	err := u.sysMunmap()
	return err
}

// ------------------------------------------ implement submission ------------------------------------------

// Submit will return the number of SQEs submitted.
func (u *URing) Submit() (uint, error) {
	return u.submitAndWait(0)
}

// SubmitAndWait is the same as Submit(), but takes an additional parameter
// nr that lets you specify how many completions to wait for.
// This call will block until nr submission requests are processed by the kernel
// and their details placed in the CQ.
func (u *URing) SubmitAndWait(nr uint32) (uint, error) {
	return u.submitAndWait(nr)
}

// ------------------------------------------ implement completion ------------------------------------------

// WaitCQE implements URing, it returns an I/O CQE, waiting for it if necessary
func (u *URing) WaitCQE() (cqe *URingCQE, err error) {
	return u.WaitCQENr(1)
}

// WaitCQENr implements URing, it returns an I/O CQE, waiting for nr completions if one isn’t readily
func (u *URing) WaitCQENr(nr uint32) (cqe *URingCQE, err error) {
	return u.getCQE(getData{
		submit:   0,
		waitNr:   nr,
		getFlags: 0,
		arg:      unsafe.Pointer(nil),
		sz:       NSIG / 8,
	})
}

// WaitCQEs implements URing, like WaitCQE() except it accepts a timeout value as well.
// Note that an SQE is used internally to handle the timeout. Applications using this function
// must never set sqe->user_data to LIBURING_UDATA_TIMEOUT.
func (u *URing) WaitCQEs(nr uint32, timeout time.Duration) (*URingCQE, error) {
	var toSubmit int64

	if timeout > 0 {
		if u.Params.flags&IORING_FEAT_EXT_ARG != 0 {
			return u.WaitCQEsNew(nr, timeout)
		}
		toSubmit, err := u.submitTimeout(timeout)

		if toSubmit < 0 {
			return nil, err
		}
	}

	return u.getCQE(getData{
		submit: uint32(toSubmit),
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

	if n == 0 {
		if u.cqRingNeedFlush() {
			SysEnter(u.fd, 0, 0, IORING_ENTER_GETEVENTS, nil, NSIG/8)
			SMP_SQRING.Store(u.sqRing)
			n = u.peekBatchCQE(cqes, shift)
		}
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

// WaitCQEsNew implements URing
func (u *URing) WaitCQEsNew(nr uint32, timeout time.Duration) (cqe *URingCQE, err error) {
	ts := syscall.NsecToTimespec(timeout.Nanoseconds())
	arg := getEventsArg(uintptr(unsafe.Pointer(nil)), NSIG/8, uintptr(unsafe.Pointer(&ts)))

	cqe, err = u.getCQE(getData{
		submit:   0,
		waitNr:   nr,
		getFlags: IORING_ENTER_EXT_ARG,
		arg:      unsafe.Pointer(arg),
		sz:       int(_sizeEventsArg),
	})

	runtime.KeepAlive(arg)
	runtime.KeepAlive(ts)
	return
}
