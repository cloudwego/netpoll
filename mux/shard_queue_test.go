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

//go:build !windows
// +build !windows

package mux

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/netpoll"
)

func TestShardQueue(t *testing.T) {
	accepted := make(chan struct{})

	network, address := "tcp", ":18888"
	ln, err := net.Listen("tcp", ":18888")
	MustNil(t, err)
	count, pkgsize := 16, 11
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		svrConn, err := ln.Accept()
		MustNil(t, err)
		accepted <- struct{}{}

		total := count * pkgsize
		recv := make([]byte, total)
		rn, err := svrConn.Read(recv)
		MustNil(t, err)
		Equal(t, rn, total)
	}()

	conn, err := netpoll.DialConnection(network, address, time.Second)
	MustNil(t, err)
	<-accepted

	// test
	queue := NewShardQueue(4, conn)
	for i := 0; i < count; i++ {
		var getter WriterGetter = func() (buf netpoll.Writer, isNil bool) {
			buf = netpoll.NewLinkBuffer(pkgsize)
			buf.Malloc(pkgsize)
			return buf, false
		}
		queue.Add(getter)
	}

	err = queue.Close()
	MustNil(t, err)

	wg.Wait()
	err = ln.Close()
	MustNil(t, err)
}
