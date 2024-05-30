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
	"io"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/netpoll"
)

func TestShardQueue(t *testing.T) {
	var svrConn net.Conn
	var cliConn netpoll.Connection
	accepted := make(chan struct{})
	stopped := make(chan struct{})
	streams, framesize := 128, 1024
	totalsize := int32(streams * framesize)
	var send, read int32

	// create server connection
	network, address := "tcp", ":18888"
	ln, err := net.Listen("tcp", ":18888")
	MustNil(t, err)
	go func() {
		var err error
		for {
			select {
			case <-stopped:
				err = ln.Close()
				MustNil(t, err)
				return
			default:
			}
			svrConn, err = ln.Accept()
			MustNil(t, err)
			accepted <- struct{}{}
			go func() {
				recv := make([]byte, 10240)
				for {
					n, err := svrConn.Read(recv)
					atomic.AddInt32(&read, int32(n))
					for i := 0; i < n; i++ {
						MustTrue(t, recv[i] == 'a')
					}
					if err == io.EOF {
						return
					}
					MustNil(t, err)
				}
			}()
		}
	}()

	// create client connection
	cliConn, err = netpoll.DialConnection(network, address, time.Second)
	MustNil(t, err)
	<-accepted // wait svrConn accepted

	// cliConn flush packets to svrConn with ShardQueue
	queue := NewShardQueue(4, cliConn)
	for i := 0; i < streams; i++ {
		go func() {
			var getter WriterGetter = func() (buf netpoll.Writer, isNil bool) {
				buf = netpoll.NewLinkBuffer(framesize)
				data, err := buf.Malloc(framesize)
				MustNil(t, err)
				for b := 0; b < framesize; b++ {
					data[b] = 'a'
				}
				return buf, false
			}
			if queue.Add(getter) {
				atomic.AddInt32(&send, int32(framesize))
			}
		}()
	}

	//cliConn graceful close, shardQueue should flush all data correctly
	for atomic.LoadInt32(&send) < totalsize/2 {
		t.Logf("waiting send all packets: send=%d", atomic.LoadInt32(&send))
		runtime.Gosched()
	}
	err = queue.Close()
	MustNil(t, err)
	for atomic.LoadInt32(&read) != atomic.LoadInt32(&send) {
		t.Logf("waiting read all packets: read=%d", atomic.LoadInt32(&read))
		runtime.Gosched()
	}
}
