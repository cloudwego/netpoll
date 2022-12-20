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

//go:build darwin || netbsd || freebsd || openbsd || dragonfly || linux
// +build darwin netbsd freebsd openbsd dragonfly linux

package netpoll

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestListenerDialer(t *testing.T) {
	network := "tcp"
	addr := ":1234"
	ln, err := CreateListener(network, addr)
	MustNil(t, err)
	defer ln.Close()
	trigger := make(chan int)
	msg := []byte("0123456789")

	go func() {
		for {
			conn, err := ln.Accept()
			if conn == nil && err == nil {
				continue
			}
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				<-trigger
				buf := make([]byte, 10)
				n, err := conn.Read(buf)
				MustNil(t, err)
				Equal(t, n, len(msg))
				Equal(t, string(buf[:n]), string(msg))
				n, err = conn.Write(buf)
				MustNil(t, err)
				Equal(t, n, len(msg))
			}(conn)
		}
	}()

	// trigger
	var closed, read int32

	dialer := NewDialer()
	callback := func(connection Connection) error {
		atomic.StoreInt32(&closed, 1)
		return nil
	}
	onRequest := func(ctx context.Context, connection Connection) error {
		atomic.StoreInt32(&read, 1)
		err := connection.Close()
		MustNil(t, err)
		return err
	}
	for i := 0; i < 10; i++ {
		conn, err := dialer.DialConnection(network, addr, time.Second)
		if err != nil {
			continue
		}
		conn.AddCloseCallback(callback)
		conn.SetOnRequest(onRequest)

		MustNil(t, err)
		n, err := conn.Write([]byte(msg))
		MustNil(t, err)
		Equal(t, n, len(msg))
		time.Sleep(10 * time.Millisecond)
		trigger <- 1
		time.Sleep(10 * time.Millisecond)
		Equal(t, atomic.LoadInt32(&read), int32(1))
		Equal(t, atomic.LoadInt32(&closed), int32(1))
	}
}

func TestConvertListener(t *testing.T) {
	network, address := "unix", "mock.test.sock"
	ln, err := net.Listen(network, address)
	if err != nil {
		panic(err)
	}
	udsln, _ := ln.(*net.UnixListener)
	// udsln.SetUnlinkOnClose(false)

	nln, err := ConvertListener(udsln)
	if err != nil {
		panic(err)
	}
	err = nln.Close()
	if err != nil {
		panic(err)
	}
}
