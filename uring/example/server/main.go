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

package main

import (
	"errors"
	"fmt"
	"log"
	"syscall"

	. "github.com/cloudwego/netpoll/uring"
)

const (
	ENTRIES             = 4096
	DEFAULT_SERVER_PORT = 8000
	QUEUE_DEPTH         = 256
	READ_SZ             = 8192
)

type eventType int

const (
	EVENT_TYPE_ACCEPT eventType = iota
	EVENT_TYPE_READ
	EVENT_TYPE_WRITE
)

type request struct {
	fd        int
	eventType eventType
	recvOp    *RecvOp
	sendOp    *SendOp
}

var requests [4096]request
var bufs [][]byte

func init() {
	for fd := range requests {
		requests[fd].recvOp = Recv(uintptr(fd), nil, 0)
		requests[fd].sendOp = Send(uintptr(fd), nil, 0)
	}
	bufs = make([][]byte, ENTRIES)
	for idx := range bufs {
		bufs[idx] = make([]byte, READ_SZ)
	}
}

func main() {
	serverSockFd, err := setupListeningSocket(DEFAULT_SERVER_PORT)
	MustNil(err)
	defer syscall.Close(serverSockFd)

	fmt.Printf("ZeroHTTPd listening on port: %d\n", DEFAULT_SERVER_PORT)

	u, err := IOURing(ENTRIES)
	MustNil(err)
	defer u.Close()

	serverLoop(u, serverSockFd)
}

func setupListeningSocket(port int) (serverSockFd int, err error) {
	serverSockFd, err = syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	MustNil(err)

	err = syscall.SetsockoptInt(serverSockFd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	MustNil(err)

	err = syscall.Bind(serverSockFd, &syscall.SockaddrInet4{Port: port})
	MustNil(err)

	err = syscall.Listen(serverSockFd, QUEUE_DEPTH)
	MustNil(err)
	return
}

func serverLoop(u *URing, serverSockFd int) {
	accept := Accept(uintptr(serverSockFd), 0)
	addAcceptRequest(u, accept)

	cqes := make([]*URingCQE, QUEUE_DEPTH)

	for {
		_, err := u.Submit()
		MustNil(err)

		_, err = u.WaitCQE()
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
			continue
		}
		MustNil(err)

		for i, n := 0, u.PeekBatchCQE(cqes); i < n; i++ {
			cqe := cqes[i]

			userData := requests[cqe.UserData]
			eventType := userData.eventType
			res := cqe.Res

			u.CQESeen()

			switch eventType {
			case EVENT_TYPE_ACCEPT:
				addReadRequest(u, int(res))
				addAcceptRequest(u, accept)
			case EVENT_TYPE_READ:
				addReadRequest(u, userData.fd)
			case EVENT_TYPE_WRITE:
				if res <= 0 {
					syscall.Shutdown(userData.fd, syscall.SHUT_RDWR)
				} else {
					addWriteRequest(u, userData.fd, res)
				}
			}
		}
	}
}

func addAcceptRequest(u *URing, accept *AcceptOp) {
	requests[accept.Fd()].fd = accept.Fd()
	requests[accept.Fd()].eventType = EVENT_TYPE_ACCEPT

	err := u.Queue(accept, 0, uint64(accept.Fd()))
	MustNil(err)
}

func addReadRequest(u *URing, fd int) {
	requests[fd].fd = fd
	requests[fd].eventType = EVENT_TYPE_READ
	requests[fd].recvOp.SetBuff(bufs[fd])

	err := u.Queue(requests[fd].recvOp, 0, uint64(fd))
	MustNil(err)
}

func addWriteRequest(u *URing, fd int, bytes int32) {
	requests[fd].fd = fd
	requests[fd].eventType = EVENT_TYPE_WRITE
	requests[fd].sendOp.SetBuff(bufs[fd][:bytes])

	err := u.Queue(requests[fd].sendOp, 0, uint64(fd))
	MustNil(err)
}

func MustNil(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
