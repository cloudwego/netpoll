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
	return poll
}

type uringPoll struct {
	size    int
	caps    int
	trigger uint32

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
		n = p.uring.PeekBatchCQE(p.cqes)
		if n == 0 {
			continue
		}
		p.uring.Advance(uint32(n))
		if p.handler(p.cqes[:n]) {
			return nil
		}
	}
}

// Close implements Poll.
func (p *uringPoll) Close() error {
	var userData uint64
	*(**FDOperator)(unsafe.Pointer(&userData)) = &FDOperator{FD: p.uring.Fd(), state: -1}
	err := p.trig(userData)
	return err
}

// Trigger implements Poll.
func (p *uringPoll) Trigger() error {
	if atomic.AddUint32(&p.trigger, 1) > 1 {
		return nil
	}
	var userData uint64
	*(**FDOperator)(unsafe.Pointer(&userData)) = &FDOperator{FD: p.uring.Fd()}
	err := p.trig(userData)
	return err
}

// Control implements Poll.
func (p *uringPoll) Control(operator *FDOperator, event PollEvent) (err error) {
	var ctlOp, pollOp Op
	var userData uint64
	var evt epollevent
	*(**FDOperator)(unsafe.Pointer(&evt.data)) = operator
	switch event {
	case PollReadable:
		operator.inuse()
		evt.events = syscall.EPOLLIN | syscall.EPOLLRDHUP | syscall.EPOLLERR
		ctlOp = URingEpollCtl(uintptr(operator.FD), uintptr(p.uring.Fd()), syscall.EPOLL_CTL_ADD, unsafe.Pointer(&evt))
		pollOp = PollAdd(uintptr(operator.FD), evt.events)
	case PollModReadable:
		operator.inuse()
		evt.events = syscall.EPOLLIN | syscall.EPOLLRDHUP | syscall.EPOLLERR
		ctlOp = URingEpollCtl(uintptr(operator.FD), uintptr(p.uring.Fd()), syscall.EPOLL_CTL_MOD, unsafe.Pointer(&evt))
		pollOp = PollAdd(uintptr(operator.FD), evt.events)
	case PollDetach:
		evt.events = syscall.EPOLLIN | syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR
		ctlOp = URingEpollCtl(uintptr(operator.FD), uintptr(p.uring.Fd()), syscall.EPOLL_CTL_DEL, unsafe.Pointer(&evt))
		pollOp = PollAdd(uintptr(operator.FD), evt.events)
	case PollWritable:
		operator.inuse()
		evt.events = EPOLLET | syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR
		ctlOp = URingEpollCtl(uintptr(operator.FD), uintptr(p.uring.Fd()), syscall.EPOLL_CTL_ADD, unsafe.Pointer(&evt))
		pollOp = PollAdd(uintptr(operator.FD), evt.events)
	case PollR2RW:
		evt.events = syscall.EPOLLIN | syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR
		ctlOp = URingEpollCtl(uintptr(operator.FD), uintptr(p.uring.Fd()), syscall.EPOLL_CTL_MOD, unsafe.Pointer(&evt))
		pollOp = PollAdd(uintptr(operator.FD), evt.events)
	case PollRW2R:
		evt.events = syscall.EPOLLIN | syscall.EPOLLRDHUP | syscall.EPOLLERR
		ctlOp = URingEpollCtl(uintptr(operator.FD), uintptr(p.uring.Fd()), syscall.EPOLL_CTL_MOD, unsafe.Pointer(&evt))
		pollOp = PollAdd(uintptr(operator.FD), evt.events)
	}

	*(**FDOperator)(unsafe.Pointer(&userData)) = operator

	err = p.uring.Queue(pollOp, 0, userData)
	if err != nil {
		panic(err)
	}
	err = p.uring.Queue(ctlOp, 0, userData)
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
		var operator = *(**FDOperator)(unsafe.Pointer(&cqes[i].UserData))
		// trigger or exit gracefully
		if operator.FD == p.uring.Fd() {
			// must clean trigger first
			atomic.StoreUint32(&p.trigger, 0)
			// if closed & exit
			if operator.state == -1 {
				p.uring.Close()
				return true
			}
			operator.done()
			continue
		}

		if !operator.do() {
			continue
		}

		var events = cqes[i].Res
		// check poll in
		if events&syscall.EPOLLIN != 0 {
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
		if events&(syscall.EPOLLHUP|syscall.EPOLLRDHUP) != 0 {
			p.appendHup(operator)
			continue
		}
		if events&syscall.EPOLLERR != 0 {
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
		if events&syscall.EPOLLOUT != 0 {
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

func (p *uringPoll) trig(userData uint64) error {
	err := p.uring.Queue(Nop(), 0, userData)
	if err != nil {
		return err
	}
	_, err = p.uring.Submit()
	return err
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
