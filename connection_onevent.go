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

	"github.com/cloudwego/netpoll/internal/runner"
)

// ------------------------------------ implement OnPrepare, OnRequest, CloseCallback ------------------------------------

type gracefulExit interface {
	isIdle() (yes bool)
	Close() (err error)
}

// onEvent is the collection of event processing.
// OnPrepare, OnRequest, CloseCallback share the lock processing,
// which is a CAS lock and can only be cleared by OnRequest.
type onEvent struct {
	ctx                  context.Context
	onConnectCallback    atomic.Value
	onDisconnectCallback atomic.Value
	onRequestCallback    atomic.Value
	closeCallbacks       atomic.Value // value is latest *callbackNode
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

// SetOnDisconnect set the OnDisconnect callback.
func (c *connection) SetOnDisconnect(onDisconnect OnDisconnect) error {
	if onDisconnect != nil {
		c.onDisconnectCallback.Store(onDisconnect)
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
	cb := &callbackNode{}
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
		c.SetOnDisconnect(opts.onDisconnect)
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
	onConnect, _ := c.onConnectCallback.Load().(OnConnect)
	if onConnect == nil {
		c.changeState(connStateNone, connStateConnected)
		return
	}
	if !c.lock(connecting) {
		// it never happens because onDisconnect will not lock connecting if c.connected == 0
		return
	}
	onRequest, _ := c.onRequestCallback.Load().(OnRequest)
	c.onProcess(onConnect, onRequest)
}

// when onDisconnect called, c.IsActive() must return false
func (c *connection) onDisconnect() {
	onDisconnect, _ := c.onDisconnectCallback.Load().(OnDisconnect)
	if onDisconnect == nil {
		return
	}
	onConnect, _ := c.onConnectCallback.Load().(OnConnect)
	if onConnect == nil {
		// no need lock if onConnect is nil
		// it's ok to force set state to disconnected since onConnect is nil
		c.setState(connStateDisconnected)
		onDisconnect(c.ctx, c)
		return
	}
	// check if OnConnect finished when onConnect != nil && onDisconnect != nil
	if c.getState() != connStateNone && c.lock(connecting) { // means OnConnect already finished
		// protect onDisconnect run once
		// if CAS return false, means OnConnect already helps to run onDisconnect
		if c.changeState(connStateConnected, connStateDisconnected) {
			onDisconnect(c.ctx, c)
		}
		c.unlock(connecting)
		return
	}
	// OnConnect is not finished yet, return and let onConnect helps to call onDisconnect
}

// onRequest is responsible for executing the closeCallbacks after the connection has been closed.
func (c *connection) onRequest() (needTrigger bool) {
	onRequest, ok := c.onRequestCallback.Load().(OnRequest)
	if !ok {
		return true
	}
	// wait onConnect finished first
	if c.getState() == connStateNone && c.onConnectCallback.Load() != nil {
		// let onConnect to call onRequest
		return
	}
	processed := c.onProcess(nil, onRequest)
	// if not processed, should trigger read
	return !processed
}

// onProcess is responsible for executing the onConnect/onRequest function serially,
// and make sure the connection has been closed correctly if user call c.Close() in onConnect/onRequest function.
func (c *connection) onProcess(onConnect OnConnect, onRequest OnRequest) (processed bool) {
	// task already exists
	if !c.lock(processing) {
		return false
	}

	task := func() {
		panicked := true
		defer func() {
			if !panicked {
				return
			}
			// cannot use recover() here, since we don't want to break the panic stack
			c.unlock(processing)
			if c.IsActive() {
				c.Close()
			} else {
				c.closeCallback(false, false)
			}
		}()
		// trigger onConnect first
		if onConnect != nil && c.changeState(connStateNone, connStateConnected) {
			c.ctx = onConnect(c.ctx, c)
			if !c.IsActive() && c.changeState(connStateConnected, connStateDisconnected) {
				// since we hold connecting lock, so we should help to call onDisconnect here
				onDisconnect, _ := c.onDisconnectCallback.Load().(OnDisconnect)
				if onDisconnect != nil {
					onDisconnect(c.ctx, c)
				}
			}
			c.unlock(connecting)
		}
	START:
		// The `onRequest` must be executed at least once if conn have any readable data,
		// which is in order to cover the `send & close by peer` case.
		if onRequest != nil && c.Reader().Len() > 0 {
			_ = onRequest(c.ctx, c)
		}
		// The processing loop must ensure that the connection meets `IsActive`.
		// `onRequest` must either eventually read all the input data or actively Close the connection,
		// otherwise the goroutine will fall into a dead loop.
		var closedBy who
		for {
			closedBy = c.status(closing)
			// close by user or not processable
			if closedBy == user || onRequest == nil || c.Reader().Len() == 0 {
				break
			}
			_ = onRequest(c.ctx, c)
		}
		// handling callback if connection has been closed.
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
		// Note: Poller's closeCallback call will try to get processing lock failed but here already neer to unlock processing.
		//       So here we need to check connection state again, to avoid connection leak
		// double check close state
		if c.status(closing) != 0 && c.lock(processing) {
			// poller will get the processing lock failed, here help poller do closeCallback
			// fd must already detach by poller
			c.closeCallback(false, false)
			panicked = false
			return
		}
		// double check is processable
		if onRequest != nil && c.Reader().Len() > 0 && c.lock(processing) {
			goto START
		}
		// task exits
		panicked = false
	} // end of task closure func

	// add new task
	runner.RunTask(c.ctx, task)
	return true
}

// closeCallback .
// It can be confirmed that closeCallback and onRequest will not be executed concurrently.
// If onRequest is still running, it will trigger closeCallback on exit.
func (c *connection) closeCallback(needLock, needDetach bool) (err error) {
	if needLock && !c.lock(processing) {
		return nil
	}
	if needDetach && c.operator.poll != nil { // If Close is called during OnPrepare, poll is not registered.
		// PollDetach only happen when user call conn.Close() or poller detect error
		if err := c.operator.Control(PollDetach); err != nil {
			logger.Printf("NETPOLL: closeCallback[%v,%v] detach operator failed: %v", needLock, needDetach, err)
		}
	}
	latest := c.closeCallbacks.Load()
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
