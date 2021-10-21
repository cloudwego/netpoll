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

// +build darwin netbsd freebsd openbsd dragonfly

package netpoll

import (
	"sync"
	"sync/atomic"
	"syscall"
)

func openMainPoll() *mainPoll {
	var poll = &mainPoll{}
	poll.InitKevent()
	return poll
}

type mainPoll struct {
	baseKevent
	m sync.Map
}

// Control implements Poll.
func (p *mainPoll) Control(operator *FDOperator, event PollEvent) error {
	var evs = make([]syscall.Kevent_t, 1)
	evs[0].Ident = uint64(operator.FD)
	switch event {
	case PollReadable:
		p.m.Store(operator.FD, operator)
		evs[0].Filter, evs[0].Flags = syscall.EVFILT_READ, syscall.EV_ADD|syscall.EV_ENABLE
	case PollWritable:
		p.m.Store(operator.FD, operator)
		evs[0].Filter, evs[0].Flags = syscall.EVFILT_WRITE, syscall.EV_ADD|syscall.EV_ENABLE|syscall.EV_ONESHOT
	case PollDetach:
		p.m.Delete(operator.FD)
		evs[0].Filter, evs[0].Flags = syscall.EVFILT_READ, syscall.EV_DELETE|syscall.EV_ONESHOT
	case PollR2RW, PollRW2R, PollModReadable:
		// TODO: nothing here
	}
	_, err := syscall.Kevent(p.fd, evs, nil, nil)
	return err
}

// Wait implements Poll.
func (p *mainPoll) Wait() error {
	// init
	var size = 1024
	var events = make([]syscall.Kevent_t, size)
	// wait
	for {
		var hups []*FDOperator
		n, err := syscall.Kevent(p.fd, nil, events, nil)
		if err != nil && err != syscall.EINTR {
			// exit gracefully
			if err == syscall.EBADF {
				return nil
			}
			return err
		}
		for i := 0; i < n; i++ {
			var fd = int(events[i].Ident)
			// trigger
			if fd == 0 {
				// clean trigger
				atomic.StoreUint32(&p.trigger, 0)
				continue
			}
			var operator *FDOperator
			if tmp, ok := p.m.Load(fd); ok {
				operator = tmp.(*FDOperator)
			} else {
				continue
			}
			switch {
			case events[i].Flags&syscall.EV_EOF != 0:
				hups = append(hups, operator)
			case events[i].Filter == syscall.EVFILT_READ && events[i].Flags&syscall.EV_ENABLE != 0:
				operator.OnRead(p)
			case events[i].Filter == syscall.EVFILT_WRITE && events[i].Flags&syscall.EV_ENABLE != 0:
				operator.OnWrite(p)
			}
		}
		// hup conns together to avoid blocking the poll.
		if len(hups) > 0 {
			p.detaches(p, hups)
		}
	}
}
