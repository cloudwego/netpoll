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
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestListenerDialer(t *testing.T) {
	network := "tcp"
	addr := ":1234"
	ln, err := CreateListener(network, addr)
	MustNil(t, err)
	defer time.Sleep(10 * time.Millisecond)
	defer ln.Close()

	stop := make(chan int)
	trigger := make(chan int)
	defer close(stop)
	defer close(trigger)
	msg := []byte("0123456789")

	go func() {
		for {
			select {
			case <-stop:
				err := ln.Close()
				MustNil(t, err)
				return
			default:
			}
			conn, err := ln.Accept()
			if conn == nil && err == nil {
				continue
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
	var wg sync.WaitGroup

	dialer := NewDialer()
	callback := func(connection Connection) error {
		defer wg.Done()
		return nil
	}
	onRequest := func(ctx context.Context, connection Connection) error {
		defer wg.Done()
		err := connection.Close()
		MustNil(t, err)
		return err
	}
	for i := 0; i < 10; i++ {
		wg.Add(2)
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
		time.Sleep(10 * time.Millisecond) // wait for be written
		trigger <- 1
		wg.Wait() // wait for onRequest && callback both finished
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
