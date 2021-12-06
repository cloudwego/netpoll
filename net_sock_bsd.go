// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// This file may have been modified by CloudWeGo authors. (“CloudWeGo Modifications”).
// All CloudWeGo Modifications are Copyright 2021 CloudWeGo authors.

// +build darwin netbsd freebsd openbsd dragonfly

package netpoll

import "syscall"

// Wrapper around the accept system call that marks the returned file
// descriptor as nonblocking.
func accept(s int) (int, syscall.Sockaddr, error) {
	ns, sa, err := syscall.Accept(s)
	if err != nil {
		return -1, nil, err
	}
	if err = syscall.SetNonblock(ns, true); err != nil {
		syscall.Close(ns)
		return -1, nil, err
	}
	return ns, sa, nil
}
