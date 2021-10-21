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
	"sync/atomic"
	"syscall"
)

func openPoll() Poll {
	return openDefaultPoll()
}

type baseKevent struct {
	fd      int    // kevent fd
	trigger uint32 // trigger flag
}

func (b *baseKevent) InitKevent() {
	p, err := syscall.Kqueue()
	if err != nil {
		panic(err)
	}
	b.fd = p
	_, err = syscall.Kevent(b.fd, []syscall.Kevent_t{{
		Ident:  0,
		Filter: syscall.EVFILT_USER,
		Flags:  syscall.EV_ADD | syscall.EV_CLEAR,
	}}, nil, nil)
	if err != nil {
		panic(err)
	}
}

// TODO: Close will bad file descriptor here
func (p *baseKevent) Close() error {
	var err = syscall.Close(p.fd)
	return err
}

// Trigger implements Poll.
func (p *baseKevent) Trigger() error {
	if atomic.AddUint32(&p.trigger, 1) > 1 {
		return nil
	}
	_, err := syscall.Kevent(p.fd, []syscall.Kevent_t{{
		Ident:  0,
		Filter: syscall.EVFILT_USER,
		Fflags: syscall.NOTE_TRIGGER,
	}}, nil, nil)
	return err
}

func (p *baseKevent) detaches(poll Poll, hups []*FDOperator) error {
	var onhups = make([]func(p Poll) error, len(hups))
	for i := range hups {
		onhups[i] = hups[i].OnHup
		poll.Control(hups[i], PollDetach)
	}
	go func(onhups []func(p Poll) error) {
		for i := range onhups {
			if onhups[i] != nil {
				onhups[i](poll)
			}
		}
	}(onhups)
	return nil
}
