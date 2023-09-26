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
	"errors"
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
	n, err := epollWaitUntil(epollfd, events, -1)
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
	n, err = epollWaitUntil(epollfd, events, -1)
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
	_, err = epollWaitUntil(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN == 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)

	// EPOLL: readable
	_, err = syscall.Write(wfd, send)
	MustNil(t, err)
	_, err = epollWaitUntil(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN != 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)
	_, err = syscall.Read(rfd, recv)
	MustTrue(t, err == nil && string(recv) == string(send))

	// EPOLL: read finished
	_, err = epollWaitUntil(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN == 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)

	// EPOLL: close peer fd
	err = syscall.Close(wfd)
	MustNil(t, err)
	_, err = epollWaitUntil(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN != 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)
	Assert(t, events[0].events&syscall.EPOLLRDHUP != 0)
	Assert(t, events[0].events&syscall.EPOLLERR == 0)

	// EPOLL: close current fd
	rfd2, wfd2 := GetSysFdPairs()
	defer syscall.Close(wfd2)
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, rfd2, event)
	err = syscall.Close(rfd2)
	MustNil(t, err)
	_, err = epollWaitUntil(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN != 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)
	Assert(t, events[0].events&syscall.EPOLLRDHUP != 0)
	Assert(t, events[0].events&syscall.EPOLLERR == 0)

	err = EpollCtl(epollfd, unix.EPOLL_CTL_DEL, rfd, event)
	MustNil(t, err)
}

func TestEpollETClose(t *testing.T) {
	var epollfd, err = EpollCreate(0)
	MustNil(t, err)
	defer syscall.Close(epollfd)
	rfd, wfd := GetSysFdPairs()
	events := make([]epollevent, 128)
	eventdata := [8]byte{0, 0, 0, 0, 0, 0, 0, 1}
	event := &epollevent{
		events: EPOLLET | syscall.EPOLLIN | syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR,
		data:   eventdata,
	}

	// EPOLL: init state
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, rfd, event)
	_, err = epollWaitUntil(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN == 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)
	Assert(t, events[0].events&syscall.EPOLLRDHUP == 0)
	Assert(t, events[0].events&syscall.EPOLLERR == 0)

	// EPOLL: close current fd
	// nothing will happen
	err = syscall.Close(rfd)
	MustNil(t, err)
	n, err := epollWaitUntil(epollfd, events, 100)
	MustNil(t, err)
	Assert(t, n == 0, n)
	err = syscall.Close(wfd)
	MustNil(t, err)

	// EPOLL: close peer fd
	// EPOLLIN and EPOLLOUT
	rfd, wfd = GetSysFdPairs()
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, rfd, event)
	err = syscall.Close(wfd)
	MustNil(t, err)
	n, err = epollWaitUntil(epollfd, events, 100)
	MustNil(t, err)
	Assert(t, n == 1, n)
	Assert(t, events[0].events&syscall.EPOLLIN != 0)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)
	Assert(t, events[0].events&syscall.EPOLLRDHUP != 0)
	Assert(t, events[0].events&syscall.EPOLLERR == 0)
	buf := make([]byte, 1024)
	ivs := make([]syscall.Iovec, 1)
	n, err = ioread(rfd, [][]byte{buf}, ivs) // EOF
	Assert(t, n == 0 && errors.Is(err, ErrEOF), n, err)
}

func TestEpollETDel(t *testing.T) {
	var epollfd, err = EpollCreate(0)
	MustNil(t, err)
	defer syscall.Close(epollfd)
	rfd, wfd := GetSysFdPairs()
	send := []byte("hello")
	events := make([]epollevent, 128)
	eventdata := [8]byte{0, 0, 0, 0, 0, 0, 0, 1}
	event := &epollevent{
		events: EPOLLET | syscall.EPOLLIN | syscall.EPOLLRDHUP | syscall.EPOLLERR,
		data:   eventdata,
	}

	// EPOLL: del partly
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, rfd, event)
	MustNil(t, err)
	event.events = syscall.EPOLLIN | syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR
	err = EpollCtl(epollfd, unix.EPOLL_CTL_DEL, rfd, event)
	MustNil(t, err)
	_, err = syscall.Write(wfd, send)
	MustNil(t, err)
	_, err = epollWaitUntil(epollfd, events, 100)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLIN == 0)
	Assert(t, events[0].events&syscall.EPOLLRDHUP == 0)
	Assert(t, events[0].events&syscall.EPOLLERR == 0)
}

func TestEpollConnectSameFD(t *testing.T) {
	var epollfd, err = EpollCreate(0)
	MustNil(t, err)
	defer syscall.Close(epollfd)
	events := make([]epollevent, 128)
	eventdata1 := [8]byte{0, 0, 0, 0, 0, 0, 0, 1}
	eventdata2 := [8]byte{0, 0, 0, 0, 0, 0, 0, 2}
	event1 := &epollevent{
		events: EPOLLET | syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR,
		data:   eventdata1,
	}
	event2 := &epollevent{
		events: EPOLLET | syscall.EPOLLOUT | syscall.EPOLLRDHUP | syscall.EPOLLERR,
		data:   eventdata2,
	}
	eventin := &epollevent{
		events: syscall.EPOLLIN | syscall.EPOLLRDHUP | syscall.EPOLLERR,
		data:   eventdata1,
	}
	addr := syscall.SockaddrInet4{
		Port: 53,
		Addr: [4]byte{8, 8, 8, 8},
	}

	// connect non-block socket
	fd1, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	MustNil(t, err)
	t.Logf("create fd: %d", fd1)
	err = syscall.SetNonblock(fd1, true)
	MustNil(t, err)
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, fd1, event1)
	MustNil(t, err)
	err = syscall.Connect(fd1, &addr)
	t.Log(err)
	_, err = epollWaitUntil(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)
	//Assert(t, events[0].events&syscall.EPOLLRDHUP == 0)
	//Assert(t, events[0].events&syscall.EPOLLERR == 0)
	// forget to del fd
	//err = EpollCtl(epollfd, unix.EPOLL_CTL_DEL, fd1, event1)
	//MustNil(t, err)
	err = syscall.Close(fd1) // close fd1
	MustNil(t, err)

	// connect non-block socket with same fd
	fd2, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	MustNil(t, err)
	t.Logf("create fd: %d", fd2)
	err = syscall.SetNonblock(fd2, true)
	MustNil(t, err)
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, fd2, event2)
	MustNil(t, err)
	err = syscall.Connect(fd2, &addr)
	t.Log(err)
	_, err = epollWaitUntil(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)
	Assert(t, events[0].events&syscall.EPOLLRDHUP == 0)
	Assert(t, events[0].events&syscall.EPOLLERR == 0)
	err = EpollCtl(epollfd, unix.EPOLL_CTL_DEL, fd2, event2)
	MustNil(t, err)
	err = syscall.Close(fd2) // close fd2
	MustNil(t, err)
	Equal(t, events[0].data, eventdata2)

	// no event after close fd
	fd3, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	MustNil(t, err)
	t.Logf("create fd: %d", fd3)
	err = syscall.SetNonblock(fd3, true)
	MustNil(t, err)
	err = EpollCtl(epollfd, unix.EPOLL_CTL_ADD, fd3, event1)
	MustNil(t, err)
	err = syscall.Connect(fd3, &addr)
	t.Log(err)
	_, err = epollWaitUntil(epollfd, events, -1)
	MustNil(t, err)
	Assert(t, events[0].events&syscall.EPOLLOUT != 0)
	Assert(t, events[0].events&syscall.EPOLLRDHUP == 0)
	Assert(t, events[0].events&syscall.EPOLLERR == 0)
	MustNil(t, err)
	err = EpollCtl(epollfd, unix.EPOLL_CTL_MOD, fd3, eventin)
	MustNil(t, err)
	err = syscall.Close(fd3) // close fd3
	MustNil(t, err)
	n, err := epollWaitUntil(epollfd, events, 100)
	MustNil(t, err)
	Assert(t, n == 0)
}

func epollWaitUntil(epfd int, events []epollevent, msec int) (n int, err error) {
WAIT:
	n, err = EpollWait(epfd, events, msec)
	if err == syscall.EINTR {
		goto WAIT
	}
	return n, err
}
