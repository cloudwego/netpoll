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

//go:build darwin || dragonfly || freebsd || netbsd || openbsd
// +build darwin dragonfly freebsd netbsd openbsd

package netpoll

import (
	"syscall"
	"unsafe"
)

var supportZeroCopySend bool

// sendmsg wraps the sendmsg system call.
// Must len(iovs) >= len(vs)
func sendmsg(fd int, bs [][]byte, ivs []syscall.Iovec, zerocopy bool) (n int, err error) {
	iovLen := iovecs(bs, ivs)
	if iovLen == 0 {
		return 0, nil
	}
	var msghdr = syscall.Msghdr{
		Iov:    &ivs[0],
		Iovlen: int32(iovLen),
	}
	// flags = syscall.MSG_DONTWAIT
	r, _, e := syscall.RawSyscall(syscall.SYS_SENDMSG, uintptr(fd), uintptr(unsafe.Pointer(&msghdr)), uintptr(0))
	resetIovecs(bs, ivs[:iovLen])
	if e != 0 {
		return int(r), syscall.Errno(e)
	}
	return int(r), nil
}

// pwritev wraps the sendmsg call.
// Since the size of bs may exceed 2GB and the maximum size data of pwritev can send is 2G, so
// we need to handle the offset in pwritev function.
func pwritev(fd int, bs [][]byte, ivs []syscall.Iovec, offset int, zerocopy bool) (n int, err error) {
	// skip offset first.
	if offset > 0 {
		for i := 0; i < len(bs); i++ {
			l := len(bs[i])
			if l <= offset {
				offset -= l
				continue
			}
			bs[i] = bs[i][offset:]
			bs = bs[i:]
			break
		}
	}

	iovLen := iovecs(bs, ivs)
	if iovLen == 0 {
		return 0, nil
	}
	var msghdr = syscall.Msghdr{
		Iov:    &ivs[0],
		Iovlen: int32(iovLen),
	}
	// flags = syscall.MSG_DONTWAIT
	r, _, e := syscall.RawSyscall(syscall.SYS_SENDMSG, uintptr(fd), uintptr(unsafe.Pointer(&msghdr)), uintptr(0))

	resetIovecs(nil, ivs[:iovLen])
	// If some errors happen, sendmsg returns -1 and sets errno.
	if e != 0 {
		return 0, syscall.Errno(e)
	}
	return int(r), nil
}
