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

//go:build !race
// +build !race

package netpoll

import (
	"fmt"
	"log"
	"sync/atomic"
	"syscall"
	"unsafe"

	. "github.com/cloudwego/netpoll/uring"
)

func openURingPoll() Poll {
	poll := &uringPoll{}
	uring, err := IOURing(128)
	if err != nil {
		panic(err)
	}
	poll.uring = uring
	poll.closed.Store(false)

	return poll
}

type uringPoll struct {
	size    int
	caps    int
	trigger uint32
	closed  atomic.Value

	uring    *URing
	cqes     []*URingCQE
	barriers []barrier
	hups     []func(p Poll) error
}

// Wait implements Poll.
func (p *uringPoll) Wait() error {
	// init
	var caps, n = barriercap, 0
	p.reset(128, caps)
	// wait
	for {
		if n == p.size && p.size < 128*1024 {
			p.reset(p.size<<1, caps)
		}
		if p.closed.Load().(bool) {
			p.uring.Close()
			return nil
		}
		n := p.uring.PeekBatchCQE(p.cqes)
		if n == 0 {
			continue
		}
		fmt.Println(n)
		if p.handler(p.cqes[:n]) {
			return nil
		}
	}
}

// Close implements Poll.
func (p *uringPoll) Close() error {
	p.closed.Store(true)
	return nil
}

// Trigger implements Poll.
func (p *uringPoll) Trigger() error {
	if atomic.AddUint32(&p.trigger, 1) > 1 {
		return nil
	}
	// MAX(eventfd) = 0xfffffffffffffffe
	_, err := syscall.Write(p.uring.Fd(), []byte{0, 0, 0, 0, 0, 0, 0, 1})
	return err
}

// Control implements Poll.
func (p *uringPoll) Control(operator *FDOperator, event PollEvent) error {
	var op Op
	var flags uint8
	var userData uint64
	*(**FDOperator)(unsafe.Pointer(&userData)) = operator
	switch event {
	case PollReadable, PollModReadable:
		operator.inuse()
		var mask uint32 = syscall.EPOLLIN | syscall.EPOLLRDHUP | syscall.EPOLLERR
		op, flags = PollAdd(uintptr(operator.FD), mask), IORING_OP_POLL_ADD
	case PollDetach:
		op, flags = PollRemove(userData), IORING_OP_POLL_REMOVE
	case PollWritable:
		operator.inuse()
		var mask uint32 = EPOLLET | syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR
		op, flags = PollAdd(uintptr(operator.FD), mask), IORING_OP_POLL_ADD
	case PollR2RW:
		var mask uint32 = syscall.EPOLLIN | syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR
		op, flags = PollAdd(uintptr(operator.FD), mask), IORING_OP_POLL_ADD
	case PollRW2R:
		var mask uint32 = syscall.EPOLLIN | syscall.EPOLLRDHUP | syscall.EPOLLERR
		op, flags = PollAdd(uintptr(operator.FD), mask), IORING_OP_POLL_ADD
	}
	err := p.uring.Queue(op, flags, userData)
	if err != nil {
		panic(err)
	}
	_, err = p.uring.Submit()
	return err
}

func (p *uringPoll) reset(size, caps int) {
	p.size, p.caps = size, caps
	p.cqes, p.barriers = make([]*URingCQE, size), make([]barrier, size)
	for i := range p.barriers {
		p.barriers[i].bs = make([][]byte, caps)
		p.barriers[i].ivs = make([]syscall.Iovec, caps)
	}
}

func (p *uringPoll) handler(cqes []*URingCQE) (closed bool) {
	for i := range cqes {
		// trigger
		if cqes[i].Res == 0 {
			// clean trigger
			atomic.StoreUint32(&p.trigger, 0)
			continue
		}

		var operator = *(**FDOperator)(unsafe.Pointer(&cqes[i].UserData))
		if !operator.do() {
			continue
		}

		flags := cqes[i].Flags
		// check poll in
		if flags&syscall.EPOLLIN != 0 {
			if operator.OnRead != nil {
				// for non-connection
				operator.OnRead(p)
			} else {
				// only for connection
				var bs = operator.Inputs(p.barriers[i].bs)
				if len(bs) > 0 {
					var n, err = readv(operator.FD, bs, p.barriers[i].ivs)
					operator.InputAck(n)
					if err != nil && err != syscall.EAGAIN && err != syscall.EINTR {
						log.Printf("readv(fd=%d) failed: %s", operator.FD, err.Error())
						p.appendHup(operator)
						continue
					}
				}
			}
		}

		// check hup
		if flags&(syscall.EPOLLHUP|syscall.EPOLLRDHUP) != 0 {
			p.appendHup(operator)
			continue
		}
		if flags&syscall.EPOLLERR != 0 {
			// Under block-zerocopy, the kernel may give an error callback, which is not a real error, just an EAGAIN.
			// So here we need to check this error, if it is EAGAIN then do nothing, otherwise still mark as hup.
			if _, _, _, _, err := syscall.Recvmsg(operator.FD, nil, nil, syscall.MSG_ERRQUEUE); err != syscall.EAGAIN {
				p.appendHup(operator)
			} else {
				operator.done()
			}
			continue
		}
		// check poll out
		if flags&syscall.EPOLLOUT != 0 {
			if operator.OnWrite != nil {
				// for non-connection
				operator.OnWrite(p)
			} else {
				// only for connection
				var bs, supportZeroCopy = operator.Outputs(p.barriers[i].bs)
				if len(bs) > 0 {
					// TODO: Let the upper layer pass in whether to use ZeroCopy.
					var n, err = sendmsg(operator.FD, bs, p.barriers[i].ivs, false && supportZeroCopy)
					operator.OutputAck(n)
					if err != nil && err != syscall.EAGAIN {
						log.Printf("sendmsg(fd=%d) failed: %s", operator.FD, err.Error())
						p.appendHup(operator)
						continue
					}
				}
			}
		}
		operator.done()
	}
	// hup conns together to avoid blocking the poll.
	p.detaches()
	return false
}

func (p *uringPoll) appendHup(operator *FDOperator) {
	p.hups = append(p.hups, operator.OnHup)
	operator.Control(PollDetach)
	operator.done()
}

func (p *uringPoll) detaches() {
	if len(p.hups) == 0 {
		return
	}
	hups := p.hups
	p.hups = nil
	go func(onhups []func(p Poll) error) {
		for i := range onhups {
			if onhups[i] != nil {
				onhups[i](p)
			}
		}
	}(hups)
}
