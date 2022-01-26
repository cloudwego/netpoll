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
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestDialerTCP(t *testing.T) {
	dialer := NewDialer()
	conn, err := dialer.DialTimeout("tcp", ":1234", time.Second)
	MustTrue(t, err != nil)
	MustTrue(t, conn.(*TCPConnection) == nil)

	ln, err := CreateListener("tcp", ":1234")
	MustNil(t, err)

	stop := make(chan int, 1)
	defer close(stop)

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
		}
	}()

	conn, err = dialer.DialTimeout("tcp", ":1234", time.Second)
	MustNil(t, err)
	MustTrue(t, strings.HasPrefix(conn.LocalAddr().String(), "127.0.0.1:"))
	Equal(t, conn.RemoteAddr().String(), "127.0.0.1:1234")
}

func TestDialerUnix(t *testing.T) {
	dialer := NewDialer()
	conn, err := dialer.DialTimeout("unix", "tmp.sock", time.Second)
	MustTrue(t, err != nil)
	MustTrue(t, conn.(*UnixConnection) == nil)

	ln, err := CreateListener("unix", "tmp.sock")
	MustNil(t, err)
	defer ln.Close()

	stop := make(chan int, 1)
	defer func() {
		close(stop)
		time.Sleep(time.Millisecond)
	}()

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
		}
	}()

	conn, err = dialer.DialTimeout("unix", "tmp.sock", time.Second)
	MustNil(t, err)
	if runtime.GOOS == "linux" {
		Equal(t, conn.LocalAddr().String(), "@")
	} else {
		Equal(t, conn.LocalAddr().String(), "")
	}
	Equal(t, conn.RemoteAddr().String(), "tmp.sock")
}

func TestDialerFdAlloc(t *testing.T) {
	ln, err := CreateListener("tcp", ":1234")
	MustNil(t, err)
	defer ln.Close()
	el1, _ := NewEventLoop(func(ctx context.Context, connection Connection) error {
		connection.Close()
		return nil
	})
	go func() {
		el1.Serve(ln)
	}()
	var ctx1, cancel1 = context.WithTimeout(context.Background(), time.Second)
	defer cancel1()
	defer el1.Shutdown(ctx1)

	for i := 0; i < 100; i++ {
		conn, err := DialConnection("tcp", ":1234", time.Second)
		MustNil(t, err)
		fd := conn.(*TCPConnection).fd
		conn.Write([]byte("hello world"))
		for conn.IsActive() {
			runtime.Gosched()
		}
		time.Sleep(time.Millisecond)
		syscall.SetNonblock(fd, true)
	}
}

func TestFDClose(t *testing.T) {
	ln, err := CreateListener("tcp", ":1234")
	MustNil(t, err)
	defer ln.Close()
	el1, _ := NewEventLoop(func(ctx context.Context, connection Connection) error {
		connection.Close()
		return nil
	})
	go func() {
		el1.Serve(ln)
	}()
	var ctx1, cancel1 = context.WithTimeout(context.Background(), time.Second)
	defer cancel1()
	defer el1.Shutdown(ctx1)

	var fd int
	var conn Connection
	conn, err = DialConnection("tcp", ":1234", time.Second)
	MustNil(t, err)
	fd = conn.(*TCPConnection).fd
	syscall.SetNonblock(fd, true)
	conn.Close()

	conn, err = DialConnection("tcp", ":1234", time.Second)
	MustNil(t, err)
	fd = conn.(*TCPConnection).fd
	syscall.SetNonblock(fd, true)
	time.Sleep(time.Second)
	conn.Close()
}

// fd data package race test, use two servers and two dialers.
func TestDialerThenClose(t *testing.T) {
	// server 1
	ln1, _ := CreateListener("tcp", ":1231")
	el1 := mockDialerEventLoop(1)
	go func() {
		el1.Serve(ln1)
	}()
	var ctx1, cancel1 = context.WithTimeout(context.Background(), time.Second)
	defer cancel1()
	defer el1.Shutdown(ctx1)

	// server 2
	ln2, _ := CreateListener("tcp", ":1232")
	el2 := mockDialerEventLoop(2)
	go func() {
		el2.Serve(ln2)
	}()
	var ctx2, cancel2 = context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	defer el2.Shutdown(ctx2)

	size := 20
	var wg sync.WaitGroup
	wg.Add(size)
	for i := 0; i < size; i++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				// send server 1
				conn, err := DialConnection("tcp", ":1231", time.Second)
				if err == nil {
					mockDialerSend(1, &conn.(*TCPConnection).connection)
				}
				// send server 2
				conn, err = DialConnection("tcp", ":1232", time.Second)
				if err == nil {
					mockDialerSend(2, &conn.(*TCPConnection).connection)
				}
			}
		}()
	}
	wg.Wait()
}

func mockDialerEventLoop(idx int) EventLoop {
	el, _ := NewEventLoop(func(ctx context.Context, conn Connection) (err error) {
		defer func() {
			if err != nil {
				fmt.Printf("Error: server%d conn closed: %s", idx, err.Error())
				conn.Close()
			}
		}()
		operator := conn.(*connection)
		fd := operator.fd
		msg := make([]byte, 15)
		n, err := operator.Read(msg)
		if err != nil {
			fmt.Printf("Error: conn[%d] server%d-read fail: %s", operator.fd, idx, err.Error())
			return err
		}
		if n < 1 {
			return nil
		}
		if string(msg[0]) != strconv.Itoa(idx) {
			panic(fmt.Sprintf("msg[%s] != [%d-xxx]", msg, idx))
		}

		ss := strings.Split(string(msg[:n]), "-")
		rfd, _ := strconv.Atoi(ss[1])
		_, err = operator.Write([]byte(fmt.Sprintf("%d-%d", idx, fd)))
		if err != nil {
			fmt.Printf("Error: conn[%d] rfd[%d] server%d-write fail: %s", operator.fd, rfd, idx, err.Error())
			return err
		}
		return nil
	})
	return el
}

func mockDialerSend(idx int, conn *connection) {
	defer func() {
		conn.Close()
	}()
	randID1 := []byte(fmt.Sprintf("%d-%d", idx, conn.fd))
	_, err := conn.Write(randID1)
	if err != nil {
		fmt.Printf("Error: conn[%d] client%d write fail: %s", conn.fd, idx, err.Error())
	}
	msg := make([]byte, 15)
	_, err = conn.Read(msg)
	if err != nil {
		fmt.Printf("Error: conn[%d] client%d Next fail: %s", conn.fd, idx, err.Error())
	}
}
