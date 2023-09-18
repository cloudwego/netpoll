// Copyright 2023 CloudWeGo Authors
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

//go:build darwin || netbsd || freebsd || openbsd || dragonfly || linux
// +build darwin netbsd freebsd openbsd dragonfly linux

package netpoll

import "syscall"

// return value:
// - n: n == 0 but err == nil, retry syscall
// - err: if not nil, connection should be closed.
func ioread(fd int, bs [][]byte, ivs []syscall.Iovec) (n int, err error) {
	n, err = readv(fd, bs, ivs)
	if n == 0 && err == nil { // means EOF
		return 0, Exception(ErrEOF, "")
	}
	if err == syscall.EINTR || err == syscall.EAGAIN {
		return 0, nil
	}
	return n, err
}

// return value:
// - n: n == 0 but err == nil, retry syscall
// - err: if not nil, connection should be closed.
func iosend(fd int, bs [][]byte, ivs []syscall.Iovec, zerocopy bool) (n int, err error) {
	n, err = sendmsg(fd, bs, ivs, zerocopy)
	if err == syscall.EAGAIN {
		return 0, nil
	}
	return n, err
}
