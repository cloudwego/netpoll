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
	"sync/atomic"
	"syscall"
)

// Includes defaultPoll/multiPoll/uringPoll...
func openPoll() Poll {
	return openDefaultPoll()
}

type baseEpoll struct {
	fd      int         // epoll fd
	wop     *FDOperator // eventfd, wake epoll_wait
	buf     []byte      // read wfd trigger msg
	trigger uint32      // trigger flag
}

func (b *baseEpoll) InitEpoll(poll Poll) {
	b.buf = make([]byte, 8)
	var p, err = syscall.EpollCreate1(0)
	if err != nil {
		panic(err)
	}
	b.fd = p
	var r0, _, e0 = syscall.Syscall(syscall.SYS_EVENTFD2, 0, 0, 0)
	if e0 != 0 {
		syscall.Close(p)
		panic(err)
	}

	b.wop = &FDOperator{FD: int(r0)}
	poll.Control(b.wop, PollReadable)
}

// Close will write 10000000
func (b *baseEpoll) Close() error {
	_, err := syscall.Write(b.wop.FD, []byte{1, 0, 0, 0, 0, 0, 0, 0})
	return err
}

// Trigger implements Poll.
func (b *baseEpoll) Trigger() error {
	if atomic.AddUint32(&b.trigger, 1) > 1 {
		return nil
	}
	// MAX(eventfd) = 0xfffffffffffffffe
	_, err := syscall.Write(b.wop.FD, []byte{0, 0, 0, 0, 0, 0, 0, 1})
	return err
}

func (b *baseEpoll) detaches(poll Poll, hups []*FDOperator) error {
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
