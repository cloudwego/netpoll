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

package netpoll

import (
	"context"
	"net"
)

// A EventLoop is a network server.
type EventLoop interface {
	// Serve registers a listener and runs blockingly to provide services, including listening to ports,
	// accepting connections and processing trans data. When an exception occurs or Shutdown is invoked,
	// Serve will return an error which describes the specific reason.
	Serve(ln net.Listener) error

	// Shutdown is used to graceful exit.
	// It will close all idle connections on the server, but will not change the underlying pollers.
	//
	// Argument: ctx set the waiting deadline, after which an error will be returned,
	// but will not force the closing of connections in progress.
	Shutdown(ctx context.Context) error
}

// OnPrepare is used to inject custom preparation at connection initialization,
// which is optional but important in some scenarios. For example, a qps limiter
// can be set by closing overloaded connections directly in OnPrepare.
//
// Return:
// context will become the argument of OnRequest.
// Usually, custom resources can be initialized in OnPrepare and used in OnRequest.
//
// PLEASE NOTE:
// OnPrepare is executed without any data in the connection,
// so Reader() or Writer() cannot be used here, but may be supported in the future.
type OnPrepare func(connection Connection) context.Context

// OnConnect is called once connection created.
// It supports read/write/close connection, and could return a ctx which will be passed to OnRequest.
// OnConnect will not block the poller since it's executed asynchronously.
// Only after OnConnect finished the OnRequest could be executed.
//
// An example usage in TCP Proxy scenario:
//
//  func onConnect(ctx context.Context, upstream netpoll.Connection) context.Context {
//	  downstream, _ := netpoll.DialConnection("tcp", downstreamAddr, time.Second)
//	  return context.WithValue(ctx, downstreamKey, downstream)
//  }
//  func onRequest(ctx context.Context, upstream netpoll.Connection) error {
//    downstream := ctx.Value(downstreamKey).(netpoll.Connection)
//  }
type OnConnect func(ctx context.Context, connection Connection) context.Context

// OnRequest defines the function for handling connection. When data is sent from the connection peer,
// netpoll actively reads the data in LT mode and places it in the connection's input buffer.
// Generally, OnRequest starts handling the data in the following way:
//
//	func OnRequest(ctx context, connection Connection) error {
//		input := connection.Reader().Next(n)
//		handling input data...
//  	send, _ := connection.Writer().Malloc(l)
//		copy(send, output)
//		connection.Flush()
//		return nil
//	}
//
// OnRequest will run in a separate goroutine and
// it is guaranteed that there is one and only one OnRequest running at the same time.
// The underlying logic is similar to:
//
//	go func() {
//		for !connection.Reader().IsEmpty() {
//			OnRequest(ctx, connection)
//		}
//	}()
//
// PLEASE NOTE:
// OnRequest must either eventually read all the input data or actively Close the connection,
// otherwise the goroutine will fall into a dead loop.
//
// Return: error is unused which will be ignored directly.
type OnRequest func(ctx context.Context, connection Connection) error
