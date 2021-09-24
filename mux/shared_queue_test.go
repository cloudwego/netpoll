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

package mux

import (
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/cloudwego/netpoll"
)

func TestShareQueue(t *testing.T) {
	var svrConn net.Conn

	network, address := "tcp", ":1234"
	ln, err := net.Listen("tcp", ":1234")
	MustNil(t, err)
	stop := make(chan int, 1)
	defer close(stop)
	go func() {
		var err error
		for {
			select {
			case <-stop:
				err = ln.Close()
				MustNil(t, err)
				return
			default:
			}
			svrConn, err = ln.Accept()
			MustNil(t, err)
		}
	}()

	conn, err := netpoll.DialConnection(network, address, time.Second)
	MustNil(t, err)
	for svrConn == nil {
		runtime.Gosched()
	}

	// test
	queue := NewSharedQueue(4, conn)
	count, pkgsize := 16, 11
	for i := 0; i < int(count); i++ {
		var getter WriterGetter = func() (buf netpoll.Writer, isNil bool) {
			buf = netpoll.NewLinkBuffer(pkgsize)
			buf.Malloc(pkgsize)
			return buf, false
		}
		queue.Add(getter)
	}

	total := count * pkgsize
	recv := make([]byte, total)
	rn, err := svrConn.Read(recv)
	MustNil(t, err)
	Equal(t, rn, total)
}

// TODO: need mock flush
func BenchmarkShareQueue(b *testing.B) {
	b.Skip()
}
