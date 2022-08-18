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
	"syscall"
	"unsafe"
)

var wsapollProc = ws2_32_mod.NewProc("WSAPoll")
var fdarrayMu sync.Mutex

type epollevent struct {
	fd      fdtype
	events  int16
	revents int16
}

const (
	EPOLL_CTL_ADD = 1
	EPOLL_CTL_DEL = 2
	EPOLL_CTL_MOD = 3
)

// EpollCtl implements epoll_ctl.
func EpollCtl(fdarray *[]epollevent, op int, fd fdtype, event *epollevent) (err error) {
	fdarrayMu.Lock()
	defer fdarrayMu.Unlock()
	e := *event
	e.fd = fd
	switch op {
	case EPOLL_CTL_ADD:
		flag := 0
		for i := 0; i < len(*fdarray); i++ {
			if (*fdarray)[i].fd == syscall.InvalidHandle {
				(*fdarray)[i] = e
				flag = 1
				break
			}
		}
		if flag == 0 {
			fdarray_tmp := append((*fdarray), e)
			*fdarray = fdarray_tmp
		}
	case EPOLL_CTL_DEL:
		for i := 0; i < len(*fdarray); i++ {
			if (*fdarray)[i].fd == fd {
				(*fdarray)[i].fd = syscall.InvalidHandle
				break
			}
		}
	case EPOLL_CTL_MOD:
		for i := 0; i < len(*fdarray); i++ {
			if (*fdarray)[i].fd == fd {
				(*fdarray)[i] = e
				break
			}
		}
	}
	return nil
}

// EpollWait implements epoll_wait.
func EpollWait(fdarray []epollevent, events []epollevent, msec int) (n int, err error) {
	fdarrayMu.Lock()
	defer fdarrayMu.Unlock()
	if len(fdarray) == 0 {
		return 0, nil
	}
	r, _, err := wsapollProc.Call(uintptr(unsafe.Pointer(&fdarray[0])), uintptr(len(fdarray)), uintptr(msec))
	vaildNum := int(r)
	if vaildNum != 0xffffffff {
		j := 0
		for i := 0; j < vaildNum; i++ {
			if fdarray[i].fd != syscall.InvalidHandle && fdarray[i].revents != 0 {
				events[j] = fdarray[i]
				fdarray[i].events &= ^fdarray[i].revents
				j++
			}
		}
	}
	return vaildNum, err
}
