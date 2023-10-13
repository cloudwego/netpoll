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
	"fmt"
	"math/rand"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestConnectionWrite(t *testing.T) {
	var cycle, caps = 10000, 256
	var msg, buf = make([]byte, caps), make([]byte, caps)
	var wg sync.WaitGroup
	wg.Add(1)
	var count int32
	var expect = int32(cycle * caps)
	var opts = &options{}
	opts.onRequest = func(ctx context.Context, connection Connection) error {
		n, err := connection.Read(buf)
		MustNil(t, err)
		if atomic.AddInt32(&count, int32(n)) >= expect {
			wg.Done()
		}
		return nil
	}

	r, w := GetSysFdPairs()
	var rconn, wconn = &connection{}, &connection{}
	rconn.init(&netFD{fd: r}, opts)
	wconn.init(&netFD{fd: w}, opts)

	for i := 0; i < cycle; i++ {
		n, err := wconn.Write(msg)
		MustNil(t, err)
		Equal(t, n, len(msg))
	}
	wg.Wait()
	Equal(t, atomic.LoadInt32(&count), expect)
	rconn.Close()
}

func TestConnectionLargeWrite(t *testing.T) {
	// ci machine don't have 4GB memory, so skip test
	t.Skipf("skip large write test for ci job")
	var totalSize = 1024 * 1024 * 1024 * 4
	var wg sync.WaitGroup
	wg.Add(1)
	var opts = &options{}
	opts.onRequest = func(ctx context.Context, connection Connection) error {
		if connection.Reader().Len() < totalSize {
			return nil
		}
		_, err := connection.Reader().Next(totalSize)
		MustNil(t, err)
		err = connection.Reader().Release()
		MustNil(t, err)
		wg.Done()
		return nil
	}

	r, w := GetSysFdPairs()
	var rconn, wconn = &connection{}, &connection{}
	rconn.init(&netFD{fd: r}, opts)
	wconn.init(&netFD{fd: w}, opts)

	msg := make([]byte, totalSize/4)
	for i := 0; i < 4; i++ {
		_, err := wconn.Writer().WriteBinary(msg)
		MustNil(t, err)
	}
	wg.Wait()

	rconn.Close()
}

func TestConnectionRead(t *testing.T) {
	r, w := GetSysFdPairs()
	var rconn, wconn = &connection{}, &connection{}
	rconn.init(&netFD{fd: r}, nil)
	wconn.init(&netFD{fd: w}, nil)

	var size = 256
	var cycleTime = 100000
	var msg = make([]byte, size)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < cycleTime; i++ {
			buf, err := rconn.Reader().Next(size)
			MustNil(t, err)
			Equal(t, len(buf), size)
			rconn.Reader().Release()
		}
	}()
	for i := 0; i < cycleTime; i++ {
		n, err := wconn.Write(msg)
		MustNil(t, err)
		Equal(t, n, len(msg))
	}
	wg.Wait()
	rconn.Close()
}

func TestConnectionReadAfterClosed(t *testing.T) {
	r, w := GetSysFdPairs()
	var rconn = &connection{}
	rconn.init(&netFD{fd: r}, nil)
	var size = 256
	var msg = make([]byte, size)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var buf, err = rconn.Reader().Next(size)
		MustNil(t, err)
		Equal(t, len(buf), size)
	}()
	time.Sleep(time.Millisecond)
	syscall.Write(w, msg)
	syscall.Close(w)
	wg.Wait()
}

func TestConnectionWaitReadHalfPacket(t *testing.T) {
	r, w := GetSysFdPairs()
	var rconn = &connection{}
	rconn.init(&netFD{fd: r}, nil)
	var size = pagesize * 2
	var msg = make([]byte, size)

	// write half packet
	syscall.Write(w, msg[:size/2])
	// wait poller reads buffer
	for rconn.inputBuffer.Len() <= 0 {
		runtime.Gosched()
	}

	// wait read full packet
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var buf, err = rconn.Reader().Next(size)
		Equal(t, atomic.LoadInt64(&rconn.waitReadSize), int64(0))
		MustNil(t, err)
		Equal(t, len(buf), size)
	}()

	// write left half packet
	for atomic.LoadInt64(&rconn.waitReadSize) <= 0 {
		runtime.Gosched()
	}
	Equal(t, atomic.LoadInt64(&rconn.waitReadSize), int64(size))
	syscall.Write(w, msg[size/2:])
	wg.Wait()
}

func TestReadTimer(t *testing.T) {
	read := time.NewTimer(time.Second)
	MustTrue(t, read.Stop())
	time.Sleep(time.Millisecond)
	Equal(t, len(read.C), 0)
}

func TestReadTrigger(t *testing.T) {
	trigger := make(chan int, 1)
	select {
	case trigger <- 0:
	default:
	}
	Equal(t, len(trigger), 1)
}

func writeAll(fd int, buf []byte) error {
	for len(buf) > 0 {
		n, err := syscall.Write(fd, buf)
		if n < 0 {
			return err
		}
		buf = buf[n:]
	}
	return nil
}

// Large packet write test. The socket buffer is 2MB by default, here to verify
// whether Connection.Close can be executed normally after socket output buffer is full.
func TestLargeBufferWrite(t *testing.T) {
	ln, err := createTestListener("tcp", ":12345")
	MustNil(t, err)

	trigger := make(chan int)
	defer close(trigger)
	go func() {
		for {
			conn, err := ln.Accept()
			if conn == nil && err == nil {
				continue
			}
			trigger <- conn.(*netFD).fd
			<-trigger
			err = ln.Close()
			MustNil(t, err)
			return
		}
	}()

	conn, err := DialConnection("tcp", ":12345", time.Second)
	MustNil(t, err)
	rfd := <-trigger

	var wg sync.WaitGroup
	wg.Add(1)
	bufferSize := 2 * 1024 * 1024 // 2MB
	round := 128
	//start large buffer writing
	go func() {
		defer wg.Done()
		for i := 1; i <= round+1; i++ {
			_, err := conn.Writer().Malloc(bufferSize)
			MustNil(t, err)
			err = conn.Writer().Flush()
			if i <= round {
				MustNil(t, err)
			}
		}
	}()

	// wait socket buffer full
	time.Sleep(time.Millisecond * 100)
	buf := make([]byte, 1024)
	for received := 0; received < round*bufferSize; {
		n, _ := syscall.Read(rfd, buf)
		received += n
	}
	// close success
	err = conn.Close()
	MustNil(t, err)
	wg.Wait()
	trigger <- 1
}

func TestWriteTimeout(t *testing.T) {
	ln, err := createTestListener("tcp", ":1234")
	MustNil(t, err)

	interval := time.Millisecond * 100
	go func() {
		for {
			conn, err := ln.Accept()
			if conn == nil && err == nil {
				continue
			}
			if err != nil {
				return
			}
			go func() {
				buf := make([]byte, 1024)
				// slow read
				for {
					_, err := conn.Read(buf)
					if err != nil {
						err = conn.Close()
						MustNil(t, err)
						return
					}
					time.Sleep(interval)
				}
			}()
		}
	}()

	conn, err := DialConnection("tcp", ":1234", time.Second)
	MustNil(t, err)

	_, err = conn.Writer().Malloc(1024)
	MustNil(t, err)
	err = conn.Writer().Flush()
	MustNil(t, err)

	_ = conn.SetWriteTimeout(time.Millisecond * 10)
	_, err = conn.Writer().Malloc(1024 * 1024 * 512)
	MustNil(t, err)
	err = conn.Writer().Flush()
	MustTrue(t, errors.Is(err, ErrWriteTimeout))

	// close success
	err = conn.Close()
	MustNil(t, err)

	err = ln.Close()
	MustNil(t, err)
}

// TestConnectionLargeMemory is used to verify the memory usage in the large package scenario.
func TestConnectionLargeMemory(t *testing.T) {
	var start, end runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&start)

	r, w := GetSysFdPairs()
	var rconn = &connection{}
	rconn.init(&netFD{fd: r}, nil)

	var wg sync.WaitGroup
	defer wg.Wait()

	var rn, wn = 1024, 1 * 1024 * 1024

	wg.Add(1)
	go func() {
		defer wg.Done()
		rconn.Reader().Next(wn)
	}()

	var msg = make([]byte, rn)
	for i := 0; i < wn/rn; i++ {
		n, err := syscall.Write(w, msg)
		if err != nil {
			panic(err)
		}
		if n != rn {
			panic(fmt.Sprintf("n[%d]!=rn[%d]", n, rn))
		}
		time.Sleep(time.Millisecond)
	}

	runtime.ReadMemStats(&end)
	alloc := end.TotalAlloc - start.TotalAlloc
	limit := uint64(4 * 1024 * 1024)
	if alloc > limit {
		panic(fmt.Sprintf("alloc[%d] out of memory %d", alloc, limit))
	}
}

// TestSetTCPNoDelay is used to verify the connection initialization set the TCP_NODELAY correctly
func TestSetTCPNoDelay(t *testing.T) {
	fd, err := sysSocket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	conn := &connection{}
	conn.init(&netFD{network: "tcp", fd: fd}, nil)

	n, _ := syscall.GetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY)
	MustTrue(t, n > 0)
	err = setTCPNoDelay(fd, false)
	MustNil(t, err)
	n, _ = syscall.GetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY)
	MustTrue(t, n == 0)
}

func TestConnectionUntil(t *testing.T) {
	r, w := GetSysFdPairs()
	rconn, wconn := &connection{}, &connection{}
	rconn.init(&netFD{fd: r}, nil)
	wconn.init(&netFD{fd: w}, nil)
	loopSize := 100000

	msg := make([]byte, 1002)
	msg[500], msg[1001] = '\n', '\n'
	go func() {
		for i := 0; i < loopSize; i++ {
			n, err := wconn.Write(msg)
			MustNil(t, err)
			MustTrue(t, n == len(msg))
		}
		wconn.Write(msg[:100])
		wconn.Close()
	}()

	for i := 0; i < loopSize*2; i++ {
		buf, err := rconn.Reader().Until('\n')
		MustNil(t, err)
		Equal(t, len(buf), 501)
		rconn.Reader().Release()
	}

	buf, err := rconn.Reader().Until('\n')
	Equal(t, len(buf), 100)
	Assert(t, errors.Is(err, ErrEOF), err)
}

func TestBookSizeLargerThanMaxSize(t *testing.T) {
	r, w := GetSysFdPairs()
	var rconn, wconn = &connection{}, &connection{}
	rconn.init(&netFD{fd: r}, nil)
	wconn.init(&netFD{fd: w}, nil)

	var length = 25
	dataCollection := make([][]byte, length)
	for i := 0; i < length; i++ {
		dataCollection[i] = make([]byte, 2<<i)
		for j := 0; j < 2<<i; j++ {
			dataCollection[i][j] = byte(rand.Intn(256))
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < length; i++ {
			buf, err := rconn.Reader().Next(2 << i)
			MustNil(t, err)
			Equal(t, string(buf), string(dataCollection[i]))
			rconn.Reader().Release()
		}
	}()
	for i := 0; i < length; i++ {
		n, err := wconn.Write(dataCollection[i])
		MustNil(t, err)
		Equal(t, n, 2<<i)
	}
	wg.Wait()
	rconn.Close()
}

func TestConnDetach(t *testing.T) {
	ln, err := createTestListener("tcp", ":1234")
	MustNil(t, err)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if conn == nil {
				continue
			}
			go func() {
				buf := make([]byte, 1024)
				// slow read
				for {
					_, err := conn.Read(buf)
					if err != nil {
						return
					}
					time.Sleep(100 * time.Millisecond)
					_, err = conn.Write(buf)
					if err != nil {
						return
					}
				}
			}()
		}
	}()

	c, err := DialConnection("tcp", ":1234", time.Second)
	MustNil(t, err)

	conn := c.(*TCPConnection)

	err = conn.Detach()
	MustNil(t, err)

	f := os.NewFile(uintptr(conn.fd), "netpoll-connection")
	defer f.Close()

	gonetconn, err := net.FileConn(f)
	MustNil(t, err)
	buf := make([]byte, 1024)
	_, err = gonetconn.Write(buf)
	MustNil(t, err)
	_, err = gonetconn.Read(buf)
	MustNil(t, err)

	err = gonetconn.Close()
	MustNil(t, err)

	err = ln.Close()
	MustNil(t, err)
}

func TestParallelShortConnection(t *testing.T) {
	ln, err := createTestListener("tcp", ":12345")
	MustNil(t, err)
	defer ln.Close()

	var received int64
	el, err := NewEventLoop(func(ctx context.Context, connection Connection) error {
		data, err := connection.Reader().Next(connection.Reader().Len())
		if err != nil {
			return err
		}
		atomic.AddInt64(&received, int64(len(data)))
		//t.Logf("conn[%s] received: %d, active: %v", connection.RemoteAddr(), len(data), connection.IsActive())
		return nil
	})
	go func() {
		el.Serve(ln)
	}()

	conns := 100
	sizePerConn := 1024 * 100
	totalSize := conns * sizePerConn
	var wg sync.WaitGroup
	for i := 0; i < conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := DialConnection("tcp", ":12345", time.Second)
			MustNil(t, err)
			n, err := conn.Writer().WriteBinary(make([]byte, sizePerConn))
			MustNil(t, err)
			MustTrue(t, n == sizePerConn)
			err = conn.Writer().Flush()
			MustNil(t, err)
			err = conn.Close()
			MustNil(t, err)
		}()
	}
	wg.Wait()

	for atomic.LoadInt64(&received) < int64(totalSize) {
		t.Logf("received: %d, except: %d", atomic.LoadInt64(&received), totalSize)
		time.Sleep(time.Millisecond * 100)
	}
}

func TestConnectionServerClose(t *testing.T) {
	ln, err := createTestListener("tcp", ":12345")
	MustNil(t, err)
	defer ln.Close()

	/*
		Client              Server
		- Client --- connect   --> Server
		- Client <-- [ping]   --- Server
		- Client --- [pong]   --> Server
		- Client <-- close     --- Server
		- Client --- close     --> Server
	*/
	const PING, PONG = "ping", "pong"
	var wg sync.WaitGroup
	el, err := NewEventLoop(
		func(ctx context.Context, connection Connection) error {
			t.Logf("server.OnRequest: addr=%s", connection.RemoteAddr())
			defer wg.Done()
			buf, err := connection.Reader().Next(len(PONG)) // pong
			Equal(t, string(buf), PONG)
			MustNil(t, err)
			err = connection.Reader().Release()
			MustNil(t, err)
			err = connection.Close()
			MustNil(t, err)
			return err
		},
		WithOnConnect(func(ctx context.Context, connection Connection) context.Context {
			t.Logf("server.OnConnect: addr=%s", connection.RemoteAddr())
			defer wg.Done()
			// check OnPrepare
			v := ctx.Value("prepare").(string)
			Equal(t, v, "true")

			_, err := connection.Writer().WriteBinary([]byte(PING))
			MustNil(t, err)
			err = connection.Writer().Flush()
			MustNil(t, err)
			connection.AddCloseCallback(func(connection Connection) error {
				t.Logf("server.CloseCallback: addr=%s", connection.RemoteAddr())
				wg.Done()
				return nil
			})
			return ctx
		}),
		WithOnPrepare(func(connection Connection) context.Context {
			t.Logf("server.OnPrepare: addr=%s", connection.RemoteAddr())
			defer wg.Done()
			return context.WithValue(context.Background(), "prepare", "true")
		}),
	)
	defer el.Shutdown(context.Background())
	go func() {
		err := el.Serve(ln)
		if err != nil {
			t.Logf("servce end with error: %v", err)
		}
	}()

	var clientOnRequest OnRequest = func(ctx context.Context, connection Connection) error {
		t.Logf("client.OnRequest: addr=%s", connection.LocalAddr())
		defer wg.Done()
		buf, err := connection.Reader().Next(len(PING))
		MustNil(t, err)
		Equal(t, string(buf), PING)

		_, err = connection.Writer().WriteBinary([]byte(PONG))
		MustNil(t, err)
		err = connection.Writer().Flush()
		MustNil(t, err)

		_, err = connection.Reader().Next(1) // server will not send any data, just wait for server close
		MustTrue(t, errors.Is(err, ErrEOF))  // should get EOF when server close

		return connection.Close()
	}
	conns := 100
	// server: OnPrepare, OnConnect, OnRequest, CloseCallback
	// client: OnRequest, CloseCallback
	wg.Add(conns * 6)
	for i := 0; i < conns; i++ {
		go func() {
			conn, err := DialConnection("tcp", ":12345", time.Second)
			MustNil(t, err)
			err = conn.SetOnRequest(clientOnRequest)
			MustNil(t, err)
			conn.AddCloseCallback(func(connection Connection) error {
				t.Logf("client.CloseCallback: addr=%s", connection.LocalAddr())
				defer wg.Done()
				return nil
			})
		}()
	}
	//time.Sleep(time.Second)
	wg.Wait()
}

func TestConnectionDailTimeoutAndClose(t *testing.T) {
	ln, err := createTestListener("tcp", ":12345")
	MustNil(t, err)
	defer ln.Close()

	el, err := NewEventLoop(
		func(ctx context.Context, connection Connection) error {
			_, err = connection.Reader().Next(connection.Reader().Len())
			return err
		},
	)
	defer el.Shutdown(context.Background())
	go func() {
		err := el.Serve(ln)
		if err != nil {
			t.Logf("servce end with error: %v", err)
		}
	}()

	loops := 100
	conns := 100
	for l := 0; l < loops; l++ {
		var wg sync.WaitGroup
		wg.Add(conns)
		for i := 0; i < conns; i++ {
			go func() {
				defer wg.Done()
				conn, err := DialConnection("tcp", ":12345", time.Nanosecond)
				Assert(t, err == nil || strings.Contains(err.Error(), "i/o timeout"))
				_ = conn
			}()
		}
		wg.Wait()
	}
}
