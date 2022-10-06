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

//go:build !race
// +build !race

package netpoll

import (
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
)

var fd2FDOperator sync.Map

const (
	POLLRDNORM = 0x0100
	POLLRDBAND = 0x0200
	POLLIN     = (POLLRDNORM | POLLRDBAND)
	POLLPRI    = 0x0400

	POLLWRNORM = 0x0010
	POLLOUT    = (POLLWRNORM)
	POLLWRBAND = 0x0020

	POLLERR  = 0x0001
	POLLHUP  = 0x0002
	POLLNVAL = 0x0004
)

func openPoll() Poll {
	return openDefaultPoll()
}

func openDefaultPoll() *defaultPoll {
	var poll = defaultPoll{}
	poll.buf = make([]byte, 8)
	poll.fdarrayMux.Lock()
	poll.fdarray = make([]epollevent, 0)
	poll.fdmode = make([]int, 0)
	poll.fdarrayMux.Unlock()
	r, w := GetSysFdPairs()

	poll.Reset = poll.reset
	poll.Handler = poll.handler

	poll.wopr = &FDOperator{FD: fdtype(r)}
	poll.wopw = &FDOperator{FD: fdtype(w)}
	poll.Control(poll.wopr, PollReadable)
	return &poll
}

type defaultPoll struct {
	pollArgs
	fdarrayMux sync.Mutex
	fdarray    []epollevent // epoll fds
	fdmode     []int
	wopr       *FDOperator // eventfd, wake epoll_wait
	wopw       *FDOperator
	buf        []byte // read wfd trigger msg
	trigger    uint32 // trigger flag
	// fns for handle events
	Reset   func(size, caps int)
	Handler func(events []epollevent) (closed bool)
}

type pollArgs struct {
	size     int
	caps     int
	events   []epollevent
	barriers []barrier
	hups     []func(p Poll) error
}

func (a *pollArgs) reset(size, caps int) {
	a.size, a.caps = size, caps
	a.events, a.barriers = make([]epollevent, size), make([]barrier, size)
	for i := range a.barriers {
		a.barriers[i].bs = make([][]byte, a.caps)
		a.barriers[i].ivs = make([]iovec, a.caps)
	}
}

// Wait implements Poll.
func (p *defaultPoll) Wait() (err error) {
	// init
	// can not be blocked
	var caps, msec, n = barriercap, 0, 0
	p.Reset(128, caps)
	// wait
	for {
		if n == p.size && p.size < 128*1024 {
			p.Reset(p.size<<1, caps)
		}
		p.fdarrayMux.Lock()
		n, _ = EpollWait(p.fdarray, p.events, msec, p.fdmode)
		p.fdarrayMux.Unlock()

		if n == 0xffffffff {
			p.fdarrayMux.Lock()
			for i := 0; i < len(p.fdarray); i++ {
				if p.fdarray[i].fd == syscall.InvalidHandle {
					continue
				}
				_, err := syscall.Getsockname(p.fdarray[i].fd)
				if err != nil {
					p.fdarray[i].fd = syscall.InvalidHandle
					p.fdmode[i] = 0
				}
			}
			p.fdarrayMux.Unlock()
			continue
		}
		if n == 0 {
			//msec = -1
			runtime.Gosched()
			continue
		}
		if p.Handler(p.events[:n]) {
			return nil
		}
	}
}

func (p *defaultPoll) handler(events []epollevent) (closed bool) {
	for i := range events {
		v, ok := fd2FDOperator.Load(events[i].fd)
		if !ok {
			return false
		}
		operator := v.(*FDOperator)
		if !operator.do() {
			continue
		}
		// trigger or exit gracefully
		if operator.FD == p.wopr.FD {
			// must clean trigger first
			sysRead(p.wopr.FD, p.buf)
			atomic.StoreUint32(&p.trigger, 0)
			// if closed & exit
			if p.buf[0] > 0 {
				syscall.Close(p.wopr.FD)
				syscall.Close(p.wopw.FD)
				operator.done()
				return true
			}
			operator.done()
			continue
		}

		evt := events[i].revents
		// check poll in
		if evt&POLLIN != 0 || evt&POLLHUP != 0 {
			if operator.OnRead != nil {
				// for non-connection
				operator.OnRead(p)
			} else {
				// for connection
				var bs = operator.Inputs(p.barriers[i].bs)
				if len(bs) > 0 {
					var n, err = readv(operator.FD, bs, p.barriers[i].ivs)
					operator.InputAck(n)
					if err != nil && err != SEND_RECV_AGAIN && err != syscall.EINTR {
						log.Printf("readv(fd=%d) failed: %s", operator.FD, err.Error())
						p.appendHup(operator)
						continue
					}
				}
			}
		}

		// check hup
		if evt&POLLHUP != 0 {
			p.appendHup(operator)
			continue
		}
		if evt&POLLERR != 0 {
			p.appendHup(operator)
			continue
		}

		// check poll out
		if evt&POLLOUT != 0 {
			if operator.OnWrite != nil {
				// for non-connection
				operator.OnWrite(p)
			} else {
				// for connection
				var bs, supportZeroCopy = operator.Outputs(p.barriers[i].bs)
				if len(bs) > 0 {
					// TODO: Let the upper layer pass in whether to use ZeroCopy.
					var n, err = sendmsg(operator.FD, bs, p.barriers[i].ivs, false && supportZeroCopy)
					operator.OutputAck(n)
					if err != nil && err != SEND_RECV_AGAIN {
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

// Close will write 10000000
func (p *defaultPoll) Close() error {
	_, err := sysWrite(p.wopw.FD, []byte{1, 0, 0, 0, 0, 0, 0, 0})
	return err
}

// Trigger implements Poll.
func (p *defaultPoll) Trigger() error {
	if atomic.AddUint32(&p.trigger, 1) > 1 {
		return nil
	}
	// MAX(eventfd) = 0xfffffffffffffffe
	_, err := sysWrite(p.wopw.FD, []byte{0, 0, 0, 0, 0, 0, 0, 1})
	return err
}

// Control implements Poll.
func (p *defaultPoll) Control(operator *FDOperator, event PollEvent) error {
	var op int
	var evt epollevent
	evt.fd = operator.FD
	mode := 0
	fd2FDOperator.Store(operator.FD, operator)
	switch event {
	case PollReadable:
		operator.inuse()
		op, evt.events = EPOLL_CTL_ADD, POLLIN
	case PollModReadable:
		operator.inuse()
		op, evt.events = EPOLL_CTL_MOD, POLLIN
	case PollDetach:
		op, evt.events = EPOLL_CTL_DEL, POLLIN|POLLOUT
	case PollWritable:
		operator.inuse()
		op, evt.events, mode = EPOLL_CTL_ADD, POLLOUT, ET_MOD
	case PollR2RW:
		op, evt.events = EPOLL_CTL_MOD, POLLIN|POLLOUT
	case PollRW2R:
		op, evt.events = EPOLL_CTL_MOD, POLLIN
	}
	p.fdarrayMux.Lock()
	defer p.fdarrayMux.Unlock()
	return EpollCtl(&p.fdarray, op, operator.FD, &evt, mode, &p.fdmode)
}

func (p *defaultPoll) appendHup(operator *FDOperator) {
	p.hups = append(p.hups, operator.OnHup)
	operator.Control(PollDetach)
	operator.done()
}

func (p *defaultPoll) detaches() {
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
