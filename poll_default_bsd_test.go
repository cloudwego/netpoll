// Copyright 2024 CloudWeGo Authors
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

//go:build darwin
// +build darwin

package netpoll

import (
	"syscall"
	"testing"
)

func TestKqueueEvent(t *testing.T) {
	kqfd, err := syscall.Kqueue()
	defer syscall.Close(kqfd)
	_, err = syscall.Kevent(kqfd, []syscall.Kevent_t{{
		Ident:  0,
		Filter: syscall.EVFILT_USER,
		Flags:  syscall.EV_ADD | syscall.EV_CLEAR,
	}}, nil, nil)
	MustNil(t, err)

	rfd, wfd := GetSysFdPairs()
	defer syscall.Close(rfd)
	defer syscall.Close(wfd)

	// add read event
	changes := make([]syscall.Kevent_t, 1)
	changes[0].Ident = uint64(rfd)
	changes[0].Filter = syscall.EVFILT_READ
	changes[0].Flags = syscall.EV_ADD
	_, err = syscall.Kevent(kqfd, changes, nil, nil)
	MustNil(t, err)

	// write
	send := []byte("hello")
	recv := make([]byte, 5)
	_, err = syscall.Write(wfd, send)
	MustNil(t, err)

	// check readable
	events := make([]syscall.Kevent_t, 128)
	n, err := syscall.Kevent(kqfd, nil, events, nil)
	MustNil(t, err)
	Equal(t, n, 1)
	Assert(t, events[0].Filter == syscall.EVFILT_READ)
	// read
	_, err = syscall.Read(rfd, recv)
	MustNil(t, err)
	Equal(t, string(recv), string(send))

	// delete read
	changes[0].Ident = uint64(rfd)
	changes[0].Filter = syscall.EVFILT_READ
	changes[0].Flags = syscall.EV_DELETE
	_, err = syscall.Kevent(kqfd, changes, nil, nil)
	MustNil(t, err)

	// write
	_, err = syscall.Write(wfd, send)
	MustNil(t, err)

	// check readable
	n, err = syscall.Kevent(kqfd, nil, events, &syscall.Timespec{Sec: 1})
	MustNil(t, err)
	Equal(t, n, 0)
	// read
	_, err = syscall.Read(rfd, recv)
	MustNil(t, err)
	Equal(t, string(recv), string(send))
}
