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

package netpoll

import (
	"sync"
	"sync/atomic"
	"syscall"
)

func openMainPoll() *mainPoll {
	var poll = &mainPoll{}
	poll.InitEpoll(poll)
	return poll
}

type mainPoll struct {
	baseEpoll
	m sync.Map
}

// Control implements Poll.
func (p *mainPoll) Control(operator *FDOperator, event PollEvent) error {
	var op int
	var evt syscall.EpollEvent
	evt.Fd = int32(operator.FD)
	switch event {
	case PollReadable:
		p.m.Store(operator.FD, operator)
		op, evt.Events = syscall.EPOLL_CTL_ADD, syscall.EPOLLIN|syscall.EPOLLRDHUP|syscall.EPOLLERR
	case PollWritable:
		p.m.Store(operator.FD, operator)
		op, evt.Events = syscall.EPOLL_CTL_ADD, syscall.EPOLLONESHOT|syscall.EPOLLOUT|syscall.EPOLLRDHUP|syscall.EPOLLERR
	case PollDetach:
		p.m.Delete(operator.FD)
		op, evt.Events = syscall.EPOLL_CTL_DEL, syscall.EPOLLIN|syscall.EPOLLOUT|syscall.EPOLLRDHUP|syscall.EPOLLERR
	case PollR2RW, PollRW2R, PollModReadable:
		// TODO: nothing here
	}
	return syscall.EpollCtl(p.fd, op, operator.FD, &evt)
}

// Wait implements Poll.
func (p *mainPoll) Wait() (err error) {
	// init
	var size, msec, n = 128, -1, 0
	var events = make([]syscall.EpollEvent, size)

	// wait
	for {
		if n == size && size < 128*1024 {
			size = size << 1
			events = make([]syscall.EpollEvent, size)
		}
		n, err = syscall.EpollWait(p.fd, events, msec)
		if err != nil && err != syscall.EINTR {
			return err
		}
		if n <= 0 {
			continue
		}
		if p.handler(events[:n]) {
			return nil
		}
	}
}

func (p *mainPoll) handler(events []syscall.EpollEvent) (closed bool) {
	var hups []*FDOperator
	for i := range events {
		var operator *FDOperator
		if tmp, ok := p.m.Load(int(events[i].Fd)); ok {
			operator = tmp.(*FDOperator)
		} else {
			continue
		}
		// trigger or exit gracefully
		if operator.FD == p.wop.FD {
			// must clean trigger first
			syscall.Read(p.wop.FD, p.buf)
			atomic.StoreUint32(&p.trigger, 0)
			// if closed & exit
			if p.buf[0] > 0 {
				syscall.Close(p.wop.FD)
				syscall.Close(p.fd)
				return true
			}
			continue
		}
		evt := events[i].Events
		switch {
		// check hup first
		case evt&(syscall.EPOLLHUP|syscall.EPOLLRDHUP) != 0:
			hups = append(hups, operator)
		case evt&syscall.EPOLLERR != 0:
			// Under block-zerocopy, the kernel may give an error callback, which is not a real error, just an EAGAIN.
			// So here we need to check this error, if it is EAGAIN then do nothing, otherwise still mark as hup.
			if _, _, _, _, err := syscall.Recvmsg(operator.FD, nil, nil, syscall.MSG_ERRQUEUE); err != syscall.EAGAIN {
				hups = append(hups, operator)
			}
		case evt&syscall.EPOLLIN != 0:
			operator.OnRead(p)
		case evt&syscall.EPOLLOUT != 0:
			operator.OnWrite(p)
		}
	}
	// hup conns together to avoid blocking the poll.
	if len(hups) > 0 {
		p.detaches(p, hups)
	}
	return false
}
