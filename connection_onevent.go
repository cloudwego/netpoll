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
	"sync/atomic"

	"github.com/bytedance/gopkg/util/gopool"
)

var runTask = gopool.CtxGo

func disableGopool() error {
	runTask = func(ctx context.Context, f func()) {
		go f()
	}
	return nil
}

// ------------------------------------ implement OnPrepare, OnRequest, CloseCallback ------------------------------------

type gracefulExit interface {
	isIdle() (yes bool)
	Close() (err error)
}

// onEvent is the collection of event processing.
// OnPrepare, OnRequest, CloseCallback share the lock processing,
// which is a CAS lock and can only be cleared by OnRequest.
type onEvent struct {
	ctx               context.Context
	onConnectCallback atomic.Value
	onRequestCallback atomic.Value
	closeCallbacks    atomic.Value // value is latest *callbackNode
}

type callbackNode struct {
	fn  CloseCallback
	pre *callbackNode
}

// SetOnConnect set the OnConnect callback.
func (c *connection) SetOnConnect(onConnect OnConnect) error {
	if onConnect != nil {
		c.onConnectCallback.Store(onConnect)
	}
	return nil
}

// SetOnRequest initialize ctx when setting OnRequest.
func (c *connection) SetOnRequest(onRequest OnRequest) error {
	if onRequest == nil {
		return nil
	}
	c.onRequestCallback.Store(onRequest)
	// fix: trigger OnRequest if there is already input data.
	if !c.inputBuffer.IsEmpty() {
		c.onRequest()
	}
	return nil
}

// AddCloseCallback adds a CloseCallback to this connection.
func (c *connection) AddCloseCallback(callback CloseCallback) error {
	if callback == nil {
		return nil
	}
	var cb = &callbackNode{}
	cb.fn = callback
	if pre := c.closeCallbacks.Load(); pre != nil {
		cb.pre = pre.(*callbackNode)
	}
	c.closeCallbacks.Store(cb)
	return nil
}

// onPrepare supports close connection, but not read/write data.
// connection will be registered by this call after preparing.
func (c *connection) onPrepare(opts *options) (err error) {
	if opts != nil {
		c.SetOnConnect(opts.onConnect)
		c.SetOnRequest(opts.onRequest)
		c.SetReadTimeout(opts.readTimeout)
		c.SetWriteTimeout(opts.writeTimeout)
		c.SetIdleTimeout(opts.idleTimeout)

		// calling prepare first and then register.
		if opts.onPrepare != nil {
			c.ctx = opts.onPrepare(c)
		}
	}

	if c.ctx == nil {
		c.ctx = context.Background()
	}
	// prepare may close the connection.
	if c.IsActive() {
		return c.register()
	}
	return nil
}

// onConnect is responsible for executing onRequest if there is new data coming after onConnect callback finished.
func (c *connection) onConnect() {
	var onConnect, _ = c.onConnectCallback.Load().(OnConnect)
	if onConnect == nil {
		return
	}
	var onRequest, _ = c.onRequestCallback.Load().(OnRequest)
	var connected int32
	c.onProcess(
		// only process when conn active and have unread data
		func(c *connection) bool {
			// if onConnect not called
			if atomic.LoadInt32(&connected) == 0 {
				return true
			}
			// check for onRequest
			return onRequest != nil && c.Reader().Len() > 0
		},
		func(c *connection) {
			if atomic.CompareAndSwapInt32(&connected, 0, 1) {
				c.ctx = onConnect(c.ctx, c)
				return
			}
			if onRequest != nil {
				_ = onRequest(c.ctx, c)
			}
		},
	)
}

// onRequest is responsible for executing the closeCallbacks after the connection has been closed.
func (c *connection) onRequest() (needTrigger bool) {
	var onRequest, ok = c.onRequestCallback.Load().(OnRequest)
	if !ok {
		return true
	}
	processed := c.onProcess(
		// only process when conn active and have unread data
		func(c *connection) bool {
			return c.Reader().Len() > 0
		},
		func(c *connection) {
			_ = onRequest(c.ctx, c)
		},
	)
	// if not processed, should trigger read
	return !processed
}

// onProcess is responsible for executing the process function serially,
// and make sure the connection has been closed correctly if user call c.Close() in process function.
func (c *connection) onProcess(isProcessable func(c *connection) bool, process func(c *connection)) (processed bool) {
	if process == nil {
		return false
	}
	// task already exists
	if !c.lock(processing) {
		return false
	}
	// add new task
	var task = func() {
		panicked := true
		defer func() {
			// cannot use recover() here, since we don't want to break the panic stack
			if panicked {
				c.unlock(processing)
				if c.IsActive() {
					c.Close()
				} else {
					c.closeCallback(false, false)
				}
			}
		}()
	START:
		// `process` must be executed at least once if `isProcessable` in order to cover the `send & close by peer` case.
		// Then the loop processing must ensure that the connection `IsActive`.
		if isProcessable(c) {
			process(c)
		}
		// `process` must either eventually read all the input data or actively Close the connection,
		// otherwise the goroutine will fall into a dead loop.
		var closedBy who
		for {
			closedBy = c.status(closing)
			// close by user or no processable
			if closedBy == user || !isProcessable(c) {
				break
			}
			process(c)
		}
		// Handling callback if connection has been closed.
		if closedBy != none {
			//  if closed by user when processing, it "may" needs detach
			needDetach := closedBy == user
			// Here is a conor case that operator will be detached twice:
			//   If server closed the connection(client OnHup will detach op first and closeBy=poller),
			//   and then client's OnRequest function also closed the connection(closeBy=user).
			// But operator already prevent that detach twice will not cause any problem
			c.closeCallback(false, needDetach)
			panicked = false
			return
		}
		c.unlock(processing)
		// Double check when exiting.
		if isProcessable(c) && c.lock(processing) {
			goto START
		}
		// task exits
		panicked = false
		return
	}

	runTask(c.ctx, task)
	return true
}

// closeCallback .
// It can be confirmed that closeCallback and onRequest will not be executed concurrently.
// If onRequest is still running, it will trigger closeCallback on exit.
func (c *connection) closeCallback(needLock bool, needDetach bool) (err error) {
	if needLock && !c.lock(processing) {
		return nil
	}
	if needDetach && c.operator.poll != nil { // If Close is called during OnPrepare, poll is not registered.
		// PollDetach only happen when user call conn.Close() or poller detect error
		if err := c.operator.Control(PollDetach); err != nil {
			logger.Printf("NETPOLL: closeCallback[%v,%v] detach operator failed: %v", needLock, needDetach, err)
		}
	}
	var latest = c.closeCallbacks.Load()
	if latest == nil {
		return nil
	}
	for callback := latest.(*callbackNode); callback != nil; callback = callback.pre {
		callback.fn(c)
	}
	return nil
}

// register only use for connection register into poll.
func (c *connection) register() (err error) {
	err = c.operator.Control(PollReadable)
	if err != nil {
		logger.Printf("NETPOLL: connection register failed: %v", err)
		c.Close()
		return Exception(ErrConnClosed, err.Error())
	}
	return nil
}

// isIdle implements gracefulExit.
func (c *connection) isIdle() (yes bool) {
	return c.isUnlock(processing) &&
		c.inputBuffer.IsEmpty() &&
		c.outputBuffer.IsEmpty()
}
