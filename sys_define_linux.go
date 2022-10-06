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

// +build !windows

package netpoll

import (
	"syscall"
)

type iovec = syscall.Iovec
type fdtype = int

const SEND_RECV_AGAIN = syscall.EAGAIN

const (
	SO_ERROR = syscall.SO_ERROR
)

func sysRead(fd fdtype, p []byte) (n int, err error) {
	n, err = syscall.Read(fd, p)
	return n, err
}

func sysWrite(fd fdtype, p []byte) (n int, err error) {
	n, err = syscall.Write(fd, p)
	return n, err
}

func sysSetNonblock(fd fdtype, is bool) error {
	return syscall.SetNonblock(fd, is)
}

func sysGetsockoptInt(fd fdtype, level int, optname int) (int, error) {
	return syscall.GetsockoptInt(fd, level, optname)
}
