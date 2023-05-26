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
	ln, err := CreateListener("tcp", ":1234")
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

	conn, err := DialConnection("tcp", ":1234", time.Second)
	MustNil(t, err)
	rfd := <-trigger

	var wg sync.WaitGroup
	wg.Add(1)
	bufferSize := 2 * 1024 * 1024
	//start large buffer writing
	go func() {
		defer wg.Done()
		for i := 0; i < 129; i++ {
			_, err := conn.Writer().Malloc(bufferSize)
			MustNil(t, err)
			err = conn.Writer().Flush()
			if i < 128 {
				MustNil(t, err)
			}
		}
	}()

	time.Sleep(time.Millisecond * 50)
	buf := make([]byte, 1024)
	for i := 0; i < 128*bufferSize/1024; i++ {
		_, err := syscall.Read(rfd, buf)
		MustNil(t, err)
	}
	// close success
	err = conn.Close()
	MustNil(t, err)
	wg.Wait()
}

func TestWriteTimeout(t *testing.T) {
	ln, err := CreateListener("tcp", ":1234")
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
	Assert(t, errors.Is(err, ErrConnClosed), err)
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
	ln, err := CreateListener("tcp", ":1234")
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
