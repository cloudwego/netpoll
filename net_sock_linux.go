// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file may have been modified by CloudWeGo authors. (“CloudWeGo Modifications”).
// All CloudWeGo Modifications are Copyright 2021 CloudWeGo authors.

package netpoll

import "syscall"

// Wrapper around the accept system call that marks the returned file
// descriptor as nonblocking.
func accept(s int) (int, syscall.Sockaddr, error) {
	ns, sa, err := syscall.Accept4(s, syscall.SOCK_NONBLOCK)
	// On Linux the accept4 system call was introduced in 2.6.28
	// kernel and on FreeBSD it was introduced in 10 kernel. If we
	// get an ENOSYS error on both Linux and FreeBSD, or EINVAL
	// error on Linux, fall back to using accept.
	switch err {
	case nil:
		return ns, sa, nil
	default: // errors other than the ones listed
		return -1, sa, err
	case syscall.ENOSYS: // syscall missing
	case syscall.EINVAL: // some Linux use this instead of ENOSYS
	case syscall.EACCES: // some Linux use this instead of ENOSYS
	case syscall.EFAULT: // some Linux use this instead of ENOSYS
	}

	ns, sa, err = syscall.Accept(s)
	if err != nil {
		return -1, nil, err
	}
	if err = syscall.SetNonblock(ns, true); err != nil {
		syscall.Close(ns)
		return -1, nil, err
	}
	return ns, sa, nil
}
