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

package netpoll

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

const EPOLLET = unix.EPOLLET

type epollevent struct {
	unix.EpollEvent
}

// GetDataPtr returns a pointer to the 8-byte user data area of the epoll event.
// The data area is used to store an FDOperator pointer for event dispatching.
func (p *epollevent) GetDataPtr() unsafe.Pointer {
	return unsafe.Pointer(&p.Fd) // Fd+Pad together form the 8-byte data area
}

func convertEpollEventPtr(p *epollevent) *unix.EpollEvent {
	return (*unix.EpollEvent)(unsafe.Pointer(p))
}

// EpollCreate implements epoll_create1.
func EpollCreate(flag int) (fd int, err error) {
	return unix.EpollCreate1(flag)
}

// EpollCtl implements epoll_ctl.
func EpollCtl(epfd, op, fd int, event *epollevent) (err error) {
	return unix.EpollCtl(epfd, op, fd, convertEpollEventPtr(event))
}
