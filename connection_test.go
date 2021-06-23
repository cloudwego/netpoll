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
	"errors"
	"fmt"
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
	onRequest := func(ctx context.Context, connection Connection) error {
		n, err := connection.Read(buf)
		MustNil(t, err)
		if atomic.AddInt32(&count, int32(n)) >= expect {
			wg.Done()
		}
		return nil
	}
	var prepare = func(connection Connection) context.Context {
		connection.SetOnRequest(onRequest)
		return context.Background()
	}

	r, w := GetSysFdPairs()
	var rconn, wconn = &connection{}, &connection{}
	rconn.init(&netFD{fd: r}, prepare)
	wconn.init(&netFD{fd: w}, prepare)

	for i := 0; i < cycle; i++ {
		n, err := wconn.Write(msg)
		MustNil(t, err)
		Equal(t, n, len(msg))
	}
	wg.Wait()
	Equal(t, atomic.LoadInt32(&count), expect)
	rconn.Close()
}

func TestConnectionRead(t *testing.T) {
	r, w := GetSysFdPairs()
	var rconn, wconn = &connection{}, &connection{}
	rconn.init(&netFD{fd: r}, nil)
	wconn.init(&netFD{fd: w}, nil)

	var size = 256
	var msg = make([]byte, size)
	go func() {
		for {
			buf, err := rconn.Reader().Next(size)
			if err != nil && errors.Is(err, ErrConnClosed) {
				return
			}
			rconn.Reader().Release()
			MustNil(t, err)
			Equal(t, len(buf), size)
		}
	}()
	for i := 0; i < 100000; i++ {
		n, err := wconn.Write(msg)
		MustNil(t, err)
		Equal(t, n, len(msg))
	}
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

	conn.Writer().Malloc(2 * 1024 * 1024)
	conn.Writer().Flush()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 128; i++ {
			_, err := conn.Writer().Malloc(1024)
			MustNil(t, err)
			err = conn.Writer().Flush()
			MustNil(t, err)
		}
	}()
	buf := make([]byte, 1024)
	for i := 0; i < 128; i++ {
		_, err := syscall.Read(rfd, buf)
		MustNil(t, err)
	}
	wg.Wait()
	// close success
	err = conn.Close()
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
