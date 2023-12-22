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

package netpoll

import (
	"context"
	"errors"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func MustNil(t *testing.T, val interface{}) {
	t.Helper()
	Assert(t, val == nil, val)
	if val != nil {
		t.Fatal("assertion nil failed, val=", val)
	}
}

func MustTrue(t *testing.T, cond bool) {
	t.Helper()
	if !cond {
		t.Fatal("assertion true failed.")
	}
}

func Equal(t *testing.T, got, expect interface{}) {
	t.Helper()
	if got != expect {
		t.Fatalf("assertion equal failed, got=[%v], expect=[%v]", got, expect)
	}
}

func Assert(t *testing.T, cond bool, val ...interface{}) {
	t.Helper()
	if !cond {
		if len(val) > 0 {
			val = append([]interface{}{"assertion failed:"}, val...)
			t.Fatal(val...)
		} else {
			t.Fatal("assertion failed")
		}
	}
}

func TestEqual(t *testing.T) {
	var err error
	MustNil(t, err)
	MustTrue(t, err == nil)
	Equal(t, err, nil)
	Assert(t, err == nil, err)
}

func TestOnConnect(t *testing.T) {
	var network, address = "tcp", ":8888"
	req, resp := "ping", "pong"
	var loop = newTestEventLoop(network, address,
		func(ctx context.Context, connection Connection) error {
			return nil
		},
		WithOnConnect(func(ctx context.Context, conn Connection) context.Context {
			for {
				input, err := conn.Reader().Next(len(req))
				if errors.Is(err, ErrEOF) || errors.Is(err, ErrConnClosed) {
					return ctx
				}
				MustNil(t, err)
				Equal(t, string(input), req)

				_, err = conn.Writer().WriteString(resp)
				MustNil(t, err)
				err = conn.Writer().Flush()
				MustNil(t, err)
			}
		}),
	)
	var conn, err = DialConnection(network, address, time.Second)
	MustNil(t, err)

	for i := 0; i < 1024; i++ {
		_, err = conn.Writer().WriteString(req)
		MustNil(t, err)
		err = conn.Writer().Flush()
		MustNil(t, err)

		input, err := conn.Reader().Next(len(resp))
		MustNil(t, err)
		Equal(t, string(input), resp)
	}

	err = conn.Close()
	MustNil(t, err)

	err = loop.Shutdown(context.Background())
	MustNil(t, err)
}

func TestOnConnectWrite(t *testing.T) {
	var network, address = "tcp", ":8888"
	var loop = newTestEventLoop(network, address,
		func(ctx context.Context, connection Connection) error {
			return nil
		},
		WithOnConnect(func(ctx context.Context, connection Connection) context.Context {
			_, err := connection.Write([]byte("hello"))
			MustNil(t, err)
			return ctx
		}),
	)
	var conn, err = DialConnection(network, address, time.Second)
	MustNil(t, err)
	s, err := conn.Reader().ReadString(5)
	MustNil(t, err)
	MustTrue(t, s == "hello")

	err = loop.Shutdown(context.Background())
	MustNil(t, err)
}

func TestGracefulExit(t *testing.T) {
	var network, address = "tcp", ":8888"

	// exit without processing connections
	var eventLoop1 = newTestEventLoop(network, address,
		func(ctx context.Context, connection Connection) error {
			return nil
		})
	var _, err = DialConnection(network, address, time.Second)
	MustNil(t, err)
	err = eventLoop1.Shutdown(context.Background())
	MustNil(t, err)

	// exit with processing connections
	var eventLoop2 = newTestEventLoop(network, address,
		func(ctx context.Context, connection Connection) error {
			time.Sleep(10 * time.Second)
			return nil
		})
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			var conn, err = DialConnection(network, address, time.Second)
			MustNil(t, err)
			_, err = conn.Write(make([]byte, 16))
			MustNil(t, err)
		}
	}
	var ctx2, cancel2 = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	err = eventLoop2.Shutdown(ctx2)
	MustTrue(t, err != nil)
	Equal(t, err.Error(), ctx2.Err().Error())

	// exit with some processing connections
	var eventLoop3 = newTestEventLoop(network, address,
		func(ctx context.Context, connection Connection) error {
			time.Sleep(time.Duration(rand.Intn(3)) * time.Second)
			if l := connection.Reader().Len(); l > 0 {
				var _, err = connection.Reader().Next(l)
				MustNil(t, err)
			}
			return nil
		})
	for i := 0; i < 10; i++ {
		var conn, err = DialConnection(network, address, time.Second)
		MustNil(t, err)
		if i%2 == 0 {
			_, err = conn.Write(make([]byte, 16))
			MustNil(t, err)
		}
	}
	var ctx3, cancel3 = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel3()
	err = eventLoop3.Shutdown(ctx3)
	MustNil(t, err)
}

func TestCloseCallbackWhenOnRequest(t *testing.T) {
	var network, address = "tcp", ":8888"
	var requested, closed = make(chan struct{}), make(chan struct{})
	var loop = newTestEventLoop(network, address,
		func(ctx context.Context, connection Connection) error {
			_, err := connection.Reader().Next(connection.Reader().Len())
			MustNil(t, err)
			err = connection.AddCloseCallback(func(connection Connection) error {
				closed <- struct{}{}
				return nil
			})
			MustNil(t, err)
			requested <- struct{}{}
			return nil
		},
	)
	var conn, err = DialConnection(network, address, time.Second)
	MustNil(t, err)
	_, err = conn.Writer().WriteString("hello")
	MustNil(t, err)
	err = conn.Writer().Flush()
	MustNil(t, err)
	<-requested
	err = conn.Close()
	MustNil(t, err)
	<-closed

	err = loop.Shutdown(context.Background())
	MustNil(t, err)
}

func TestCloseCallbackWhenOnConnect(t *testing.T) {
	var network, address = "tcp", ":8888"
	var connected, closed = make(chan struct{}), make(chan struct{})
	var loop = newTestEventLoop(network, address,
		nil,
		WithOnConnect(func(ctx context.Context, connection Connection) context.Context {
			err := connection.AddCloseCallback(func(connection Connection) error {
				closed <- struct{}{}
				return nil
			})
			MustNil(t, err)
			connected <- struct{}{}
			return ctx
		}),
	)
	var conn, err = DialConnection(network, address, time.Second)
	MustNil(t, err)
	err = conn.Close()
	MustNil(t, err)

	<-connected
	<-closed

	err = loop.Shutdown(context.Background())
	MustNil(t, err)
}

func TestCloseConnWhenOnConnect(t *testing.T) {
	var network, address = "tcp", ":8888"
	conns := 10
	var wg sync.WaitGroup
	wg.Add(conns)
	var loop = newTestEventLoop(network, address,
		nil,
		WithOnConnect(func(ctx context.Context, connection Connection) context.Context {
			defer wg.Done()
			err := connection.Close()
			MustNil(t, err)
			return ctx
		}),
	)

	for i := 0; i < conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var conn, err = DialConnection(network, address, time.Second)
			if err != nil {
				return
			}
			_, err = conn.Reader().Next(1)
			Assert(t, errors.Is(err, ErrEOF))
			err = conn.Close()
			MustNil(t, err)
		}()
	}

	wg.Wait()
	err := loop.Shutdown(context.Background())
	MustNil(t, err)
}

func TestServerReadAndClose(t *testing.T) {
	var network, address = "tcp", ":18888"
	var sendMsg = []byte("hello")
	var closed int32
	var loop = newTestEventLoop(network, address,
		func(ctx context.Context, connection Connection) error {
			_, err := connection.Reader().Next(len(sendMsg))
			MustNil(t, err)

			err = connection.Close()
			MustNil(t, err)
			atomic.AddInt32(&closed, 1)
			return nil
		},
	)

	var conn, err = DialConnection(network, address, time.Second)
	MustNil(t, err)
	_, err = conn.Writer().WriteBinary(sendMsg)
	MustNil(t, err)
	err = conn.Writer().Flush()
	MustNil(t, err)

	for atomic.LoadInt32(&closed) == 0 {
		runtime.Gosched() // wait for poller close connection
	}
	time.Sleep(time.Millisecond * 50)
	_, err = conn.Writer().WriteBinary(sendMsg)
	MustNil(t, err)
	err = conn.Writer().Flush()
	MustTrue(t, errors.Is(err, ErrConnClosed))

	err = loop.Shutdown(context.Background())
	MustNil(t, err)
}

func TestServerPanicAndClose(t *testing.T) {
	var network, address = "tcp", ":18888"
	var sendMsg = []byte("hello")
	var paniced int32
	var loop = newTestEventLoop(network, address,
		func(ctx context.Context, connection Connection) error {
			_, err := connection.Reader().Next(len(sendMsg))
			MustNil(t, err)
			atomic.StoreInt32(&paniced, 1)
			panic("test")
		},
	)

	var conn, err = DialConnection(network, address, time.Second)
	MustNil(t, err)
	_, err = conn.Writer().WriteBinary(sendMsg)
	MustNil(t, err)
	err = conn.Writer().Flush()
	MustNil(t, err)

	for atomic.LoadInt32(&paniced) == 0 {
		runtime.Gosched() // wait for poller close connection
	}
	for conn.IsActive() {
		runtime.Gosched() // wait for poller close connection
	}

	err = loop.Shutdown(context.Background())
	MustNil(t, err)
}

func TestClientWriteAndClose(t *testing.T) {
	var (
		network, address            = "tcp", ":18889"
		connnum                     = 10
		packetsize, packetnum       = 1000 * 5, 1
		recvbytes             int32 = 0
	)
	var loop = newTestEventLoop(network, address,
		func(ctx context.Context, connection Connection) error {
			buf, err := connection.Reader().Next(connection.Reader().Len())
			if errors.Is(err, ErrConnClosed) {
				return err
			}
			MustNil(t, err)
			atomic.AddInt32(&recvbytes, int32(len(buf)))
			return nil
		},
	)
	var wg sync.WaitGroup
	for i := 0; i < connnum; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var conn, err = DialConnection(network, address, time.Second)
			MustNil(t, err)
			sendMsg := make([]byte, packetsize)
			for j := 0; j < packetnum; j++ {
				_, err = conn.Write(sendMsg)
				MustNil(t, err)
			}
			err = conn.Close()
			MustNil(t, err)
		}()
	}
	wg.Wait()
	exceptbytes := int32(packetsize * packetnum * connnum)
	for atomic.LoadInt32(&recvbytes) != exceptbytes {
		t.Logf("left %d bytes not received", exceptbytes-atomic.LoadInt32(&recvbytes))
		runtime.Gosched()
	}
	err := loop.Shutdown(context.Background())
	MustNil(t, err)
}

func TestReadThresholdOption(t *testing.T) {
	/*
		client => server: 102400 bytes + 5 bytes
		server cached: 102400 bytes, and throttled
		server read: 102400 bytes, and unthrottled
		server cached: 5 bytes
		server read: 5 bytes
		server write: 102400 bytes + 5 bytes
		client cached: 102400 bytes, and throttled
		client read: 102400 bytes, and unthrottled
		client cached: 5 bytes
		client read: 5 bytes
	*/
	readThreshold := 1024 * 100
	trigger := make(chan struct{})
	msg1 := make([]byte, readThreshold)
	msg2 := []byte("hello")
	var wg sync.WaitGroup

	// server
	ln, err := CreateListener("tcp", ":12345")
	MustNil(t, err)
	wg.Add(3)
	svr, _ := NewEventLoop(func(ctx context.Context, connection Connection) error {
		if connection.Reader().Len() < readThreshold {
			return nil
		}
		go func() {
			defer wg.Done()
			// server write
			t.Logf("server writing msg1")
			_, err := connection.Writer().WriteBinary(msg1)
			MustNil(t, err)
			err = connection.Writer().Flush()
			MustNil(t, err)
			<-trigger
			time.Sleep(time.Millisecond * 100)
			t.Logf("server writing msg2")
			_, err = connection.Writer().WriteBinary(msg2)
			MustNil(t, err)
			err = connection.Writer().Flush()
			MustNil(t, err)
		}()

		// server read
		defer wg.Done()
		t.Logf("server reading msg1")
		trigger <- struct{}{}              // let client send msg2
		time.Sleep(time.Millisecond * 100) // ensure client send msg2
		Equal(t, connection.Reader().Len(), readThreshold)
		msg, err := connection.Reader().Next(readThreshold)
		MustNil(t, err)
		Equal(t, len(msg), readThreshold)
		t.Logf("server reading msg2")
		msg, err = connection.Reader().Next(5)
		MustNil(t, err)
		Equal(t, len(msg), 5)

		_, err = connection.Reader().Next(1)
		Assert(t, errors.Is(err, ErrEOF))
		t.Logf("server closed")
		return nil
	}, WithReadBufferThreshold(int64(readThreshold)))
	defer svr.Shutdown(context.Background())
	go func() {
		svr.Serve(ln)
	}()
	time.Sleep(time.Millisecond * 100)

	// client write
	dialer := NewDialer(WithReadBufferThreshold(int64(readThreshold)))
	cli, err := dialer.DialConnection("tcp", "127.0.0.1:12345", time.Second)
	MustNil(t, err)
	go func() {
		defer wg.Done()
		t.Logf("client writing msg1")
		_, err := cli.Writer().WriteBinary(msg1)
		MustNil(t, err)
		err = cli.Writer().Flush()
		MustNil(t, err)
		<-trigger
		time.Sleep(time.Millisecond * 100)
		t.Logf("client writing msg2")
		_, err = cli.Writer().WriteBinary(msg2)
		MustNil(t, err)
		err = cli.Writer().Flush()
		MustNil(t, err)
	}()

	// client read
	trigger <- struct{}{}              // let server send msg2
	time.Sleep(time.Millisecond * 100) // ensure server send msg2
	Equal(t, cli.Reader().Len(), readThreshold)
	t.Logf("client reading msg1")
	msg, err := cli.Reader().Next(readThreshold)
	MustNil(t, err)
	Equal(t, len(msg), readThreshold)
	t.Logf("client reading msg2")
	msg, err = cli.Reader().Next(5)
	MustNil(t, err)
	Equal(t, len(msg), 5)

	err = cli.Close()
	MustNil(t, err)
	t.Logf("client closed")
	wg.Wait()
}

func TestReadThresholdClosed(t *testing.T) {
	/*
		client => server: 102400 bytes + 5 bytes
		client => server: close connection
		server cached: 102400 bytes, and throttled
		server read: 102400 bytes, and unthrottled
		server cached: 5 bytes
		server read: 5 bytes
	*/
	readThreshold := 1024 * 100
	trigger := make(chan struct{})
	msg1 := make([]byte, readThreshold)
	msg2 := []byte("hello")

	// server
	ln, err := CreateListener("tcp", ":12345")
	MustNil(t, err)
	svr, _ := NewEventLoop(func(ctx context.Context, connection Connection) error {
		if connection.Reader().Len() < readThreshold {
			return nil
		}
		// server read
		t.Logf("server reading msg1")
		trigger <- struct{}{} // let client send msg2
		<-trigger             // ensure client send msg2 and closed
		total := 0
		for {
			msg, err := connection.Reader().Next(1)
			total += len(msg)
			if errors.Is(err, ErrEOF) {
				break
			}
			_ = msg
		}
		Equal(t, total, readThreshold+5)
		close(trigger)
		return nil
	}, WithReadBufferThreshold(int64(readThreshold)))
	defer svr.Shutdown(context.Background())
	go func() {
		svr.Serve(ln)
	}()
	time.Sleep(time.Millisecond * 100)

	// client write
	dialer := NewDialer(WithReadBufferThreshold(int64(readThreshold)))
	cli, err := dialer.DialConnection("tcp", "127.0.0.1:12345", time.Second)
	MustNil(t, err)
	t.Logf("client writing msg1")
	_, err = cli.Writer().WriteBinary(msg1)
	MustNil(t, err)
	err = cli.Writer().Flush()
	MustNil(t, err)
	<-trigger
	time.Sleep(time.Millisecond * 100)
	t.Logf("client writing msg2")
	_, err = cli.Writer().WriteBinary(msg2)
	MustNil(t, err)
	err = cli.Writer().Flush()
	MustNil(t, err)
	err = cli.Close()
	MustNil(t, err)
	t.Logf("client closed")
	trigger <- struct{}{}
	<-trigger
}

func createTestListener(network, address string) (Listener, error) {
	for {
		ln, err := CreateListener(network, address)
		if err == nil {
			return ln, nil
		}
		time.Sleep(time.Millisecond * 100)
	}
}

func newTestEventLoop(network, address string, onRequest OnRequest, opts ...Option) EventLoop {
	ln, err := createTestListener(network, address)
	if err != nil {
		panic(err)
	}
	elp, err := NewEventLoop(onRequest, opts...)
	if err != nil {
		panic(err)
	}
	go elp.Serve(ln)
	return elp
}
