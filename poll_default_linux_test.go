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

//go:build linux
// +build linux

package netpoll

import (
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEpollEvent(t *testing.T) {
	var epollfd, err = EpollCreate(0)
	MustNil(t, err)
	defer syscall.Close(epollfd)

	rfd, wfd := GetSysFdPairs()
	defer syscall.Close(rfd)
	defer syscall.Close(wfd)

	send := []byte("hello")
	recv := make([]byte, 5)
	events := make([]epollevent, 128)
	eventdata1 := [8]byte{0, 0, 0, 0, 0, 0, 0, 1}
	eventdata2 := [8]byte{0, 0, 0, 0, 0, 0, 0, 2}
	eventdata3 := [8]byte{0, 0, 0, 0, 0, 0, 0, 3}
	event1 := &epollevent{
		events: syscall.EPOLLIN,
		data:   eventdata1,
	}
	event2 := &epollevent{
		events: syscall.EPOLLIN,
		data:   eventdata2,
	}
	event3 := &epollevent{
		events: syscall.EPOLLIN | syscall.EPOLLOUT,
		data:   eventdata3,
	}

	// EPOLL: add ,del and add
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, rfd, event1)
	MustNil(t, err)
	err = EpollCtl(epollfd, unix.EPOLL_CTL_DEL, rfd, event1)
	MustNil(t, err)
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, rfd, event2)
	MustNil(t, err)
	_, err = syscall.Write(wfd, send)
	MustNil(t, err)
	n, err := EpollWait(epollfd, events, -1)
	MustNil(t, err)
	Equal(t, n, 1)
	Equal(t, events[0].data, eventdata2)
	_, err = syscall.Read(rfd, recv)
	MustTrue(t, err == nil && string(recv) == string(send))
	err = EpollCtl(epollfd, unix.EPOLL_CTL_DEL, rfd, event2)
	MustNil(t, err)

	// EPOLL: add ,mod and mod
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, rfd, event1)
	MustNil(t, err)
	err = EpollCtl(epollfd, unix.EPOLL_CTL_MOD, rfd, event2)
	MustNil(t, err)
	err = EpollCtl(epollfd, unix.EPOLL_CTL_MOD, rfd, event3)
	MustNil(t, err)
	_, err = syscall.Write(wfd, send)
	MustNil(t, err)
	n, err = EpollWait(epollfd, events, -1)
	MustNil(t, err)
	Equal(t, events[0].data, eventdata3)
	_, err = syscall.Read(rfd, recv)
	MustTrue(t, err == nil && string(recv) == string(send))
	Assert(t, events[0].events&syscall.EPOLLIN != 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)

	err = EpollCtl(epollfd, unix.EPOLL_CTL_DEL, rfd, event2)
	MustNil(t, err)
}

func TestEpollWait(t *testing.T) {
	var epollfd, err = EpollCreate(0)
	MustNil(t, err)
	defer syscall.Close(epollfd)

	rfd, wfd := GetSysFdPairs()
	defer syscall.Close(wfd)

	send := []byte("hello")
	recv := make([]byte, 5)
	events := make([]epollevent, 128)
	eventdata := [8]byte{0, 0, 0, 0, 0, 0, 0, 1}

	// EPOLL: init state
	event := &epollevent{
		events: syscall.EPOLLIN | syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR,
		data:   eventdata,
	}
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, rfd, event)
	MustNil(t, err)
	_, err = EpollWait(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN == 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)

	// EPOLL: readable
	_, err = syscall.Write(wfd, send)
	MustNil(t, err)
	_, err = EpollWait(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN != 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)
	_, err = syscall.Read(rfd, recv)
	MustTrue(t, err == nil && string(recv) == string(send))

	// EPOLL: read finished
	_, err = EpollWait(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN == 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)

	// EPOLL: close peer fd
	err = syscall.Close(wfd)
	MustNil(t, err)
	_, err = EpollWait(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN != 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)
	Assert(t, events[0].events&syscall.EPOLLRDHUP != 0)
	Assert(t, events[0].events&syscall.EPOLLERR == 0)

	err = EpollCtl(epollfd, unix.EPOLL_CTL_DEL, rfd, event)
	MustNil(t, err)
}
