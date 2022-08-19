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
	"math/rand"
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

func TestCloseAndWrite(t *testing.T) {
	var network, address = "tcp", ":18888"
	var sendMsg = []byte("hello")
	var loop = newTestEventLoop(network, address,
		func(ctx context.Context, connection Connection) error {
			_, err := connection.Reader().Next(len(sendMsg))
			MustNil(t, err)

			err = connection.Close()
			MustNil(t, err)
			return nil
		},
	)

	var conn, err = DialConnection(network, address, time.Second)
	MustNil(t, err)
	_, err = conn.Writer().WriteBinary(sendMsg)
	MustNil(t, err)
	err = conn.Writer().Flush()
	MustNil(t, err)

	time.Sleep(time.Millisecond * 100) // wait for poller close connection
	_, err = conn.Writer().WriteBinary(sendMsg)
	MustNil(t, err)
	err = conn.Writer().Flush()
	MustTrue(t, errors.Is(err, ErrConnClosed))

	err = loop.Shutdown(context.Background())
	MustNil(t, err)
}

func newTestEventLoop(network, address string, onRequest OnRequest, opts ...Option) EventLoop {
	var listener, _ = CreateListener(network, address)
	var eventLoop, _ = NewEventLoop(onRequest, opts...)
	go eventLoop.Serve(listener)
	return eventLoop
}
