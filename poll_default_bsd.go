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

//go:build darwin || netbsd || freebsd || openbsd || dragonfly
// +build darwin netbsd freebsd openbsd dragonfly

package netpoll

import (
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

func openPoll() (Poll, error) {
	return openDefaultPoll()
}

func openDefaultPoll() (*defaultPoll, error) {
	l := new(defaultPoll)
	p, err := syscall.Kqueue()
	if err != nil {
		return nil, err
	}
	l.fd = p
	_, err = syscall.Kevent(l.fd, []syscall.Kevent_t{{
		Ident:  0,
		Filter: syscall.EVFILT_USER,
		Flags:  syscall.EV_ADD | syscall.EV_CLEAR,
	}}, nil, nil)
	if err != nil {
		syscall.Close(l.fd)
		return nil, err
	}
	l.opcache = newOperatorCache()
	return l, nil
}

type defaultPoll struct {
	fd      int
	trigger uint32
	m       sync.Map       // only used in go:race
	opcache *operatorCache // operator cache
	hups    []func(p Poll) error
}

// Wait implements Poll.
func (p *defaultPoll) Wait() error {
	// init
	var size, caps = 1024, barriercap
	var events, barriers = make([]syscall.Kevent_t, size), make([]barrier, size)
	for i := range barriers {
		barriers[i].bs = make([][]byte, caps)
		barriers[i].ivs = make([]syscall.Iovec, caps)
	}
	// wait
	var triggerRead, triggerWrite, triggerHup bool
	for {
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
			var operator = p.getOperator(fd, unsafe.Pointer(&events[i].Udata))
			if operator == nil || !operator.do() {
				continue
			}

			var totalRead int
			evt := events[i]
			triggerRead = evt.Filter == syscall.EVFILT_READ && evt.Flags&syscall.EV_ENABLE != 0
			triggerWrite = evt.Filter == syscall.EVFILT_WRITE && evt.Flags&syscall.EV_ENABLE != 0
			triggerHup = evt.Flags&syscall.EV_EOF != 0

			if triggerRead {
				if operator.OnRead != nil {
					// for non-connection
					operator.OnRead(p)
				} else {
					// only for connection
					var bs = operator.Inputs(barriers[i].bs)
					if len(bs) > 0 {
						var n, err = ioread(operator.FD, bs, barriers[i].ivs)
						operator.InputAck(n)
						totalRead += n
						if err != nil {
							p.appendHup(operator)
							continue
						}
					}
				}
			}
			if triggerHup {
				if triggerRead && operator.Inputs != nil {
					var leftRead int
					// read all left data if peer send and close
					if leftRead, err = readall(operator, barriers[i]); err != nil && !errors.Is(err, ErrEOF) {
						logger.Printf("NETPOLL: readall(fd=%d)=%d before close: %s", operator.FD, total, err.Error())
					}
					totalRead += leftRead
				}
				// only close connection if no further read bytes
				if totalRead == 0 {
					p.appendHup(operator)
					continue
				}
			}
			if triggerWrite {
				if operator.OnWrite != nil {
					// for non-connection
					operator.OnWrite(p)
				} else {
					// only for connection
					var bs, supportZeroCopy = operator.Outputs(barriers[i].bs)
					if len(bs) > 0 {
						// TODO: Let the upper layer pass in whether to use ZeroCopy.
						var n, err = iosend(operator.FD, bs, barriers[i].ivs, false && supportZeroCopy)
						operator.OutputAck(n)
						if err != nil {
							p.appendHup(operator)
							continue
						}
					}
				}
			}
			operator.done()
		}
		// hup conns together to avoid blocking the poll.
		p.onhups()
		p.opcache.free()
	}
}

// TODO: Close will bad file descriptor here
func (p *defaultPoll) Close() error {
	var err = syscall.Close(p.fd)
	return err
}

// Trigger implements Poll.
func (p *defaultPoll) Trigger() error {
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

// Control implements Poll.
func (p *defaultPoll) Control(operator *FDOperator, event PollEvent) error {
	var evs = make([]syscall.Kevent_t, 1)
	evs[0].Ident = uint64(operator.FD)
	p.setOperator(unsafe.Pointer(&evs[0].Udata), operator)
	switch event {
	case PollReadable:
		operator.inuse()
		evs[0].Filter, evs[0].Flags = syscall.EVFILT_READ, syscall.EV_ADD|syscall.EV_ENABLE
	case PollWritable:
		operator.inuse()
		evs[0].Filter, evs[0].Flags = syscall.EVFILT_WRITE, syscall.EV_ADD|syscall.EV_ENABLE
	case PollDetach:
		if operator.OnWrite != nil { // means WaitWrite finished
			evs[0].Filter, evs[0].Flags = syscall.EVFILT_WRITE, syscall.EV_DELETE
		} else {
			evs[0].Filter, evs[0].Flags = syscall.EVFILT_READ, syscall.EV_DELETE
		}
		p.delOperator(operator)
	case PollR2RW:
		evs[0].Filter, evs[0].Flags = syscall.EVFILT_WRITE, syscall.EV_ADD|syscall.EV_ENABLE
	case PollRW2R:
		evs[0].Filter, evs[0].Flags = syscall.EVFILT_WRITE, syscall.EV_DELETE
	}
	_, err := syscall.Kevent(p.fd, evs, nil, nil)
	return err
}
