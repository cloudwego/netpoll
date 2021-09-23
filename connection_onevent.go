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
	"log"
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
	ctx       context.Context
	process   atomic.Value // value is OnRequest
	callbacks atomic.Value // value is latest *callbackNode
}

type callbackNode struct {
	fn  CloseCallback
	pre *callbackNode
}

// SetOnRequest initialize ctx when setting OnRequest.
func (on *onEvent) SetOnRequest(onReq OnRequest) error {
	if onReq != nil {
		on.process.Store(onReq)
	}
	return nil
}

// AddCloseCallback adds a CloseCallback to this connection.
func (on *onEvent) AddCloseCallback(callback CloseCallback) error {
	if callback == nil {
		return nil
	}
	var cb = &callbackNode{}
	cb.fn = callback
	if pre := on.callbacks.Load(); pre != nil {
		cb.pre = pre.(*callbackNode)
	}
	on.callbacks.Store(cb)
	return nil
}

// OnPrepare supports close connection, but not read/write data.
// connection will be register by this call after preparing.
func (c *connection) onPrepare(prepare OnPrepare) (err error) {
	// calling prepare first and then register.
	if prepare != nil {
		c.ctx = prepare(c)
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

// onRequest is also responsible for executing the callbacks after the connection has been closed.
func (c *connection) onRequest() (err error) {
	var process = c.process.Load()
	if process == nil {
		return nil
	}
	// Buffer has been fully processed, or task already exists
	if !c.lock(processing) {
		return nil
	}
	// async: add new task
	var task = func() {
		var handler = process.(OnRequest)
	START:
		// NOTE: loop processing, which is useful for streaming.
		for c.Reader().Len() > 0 && c.IsActive() {
			// Single request processing, blocking allowed.
			handler(c.ctx, c)
		}
		// Handling callback if connection has been closed.
		if !c.IsActive() {
			c.closeCallback(false)
			return
		}
		// Double check when exiting.
		c.unlock(processing)
		if c.Reader().Len() > 0 {
			if !c.lock(processing) {
				return
			}
			goto START
		}
	}
	runTask(c.ctx, task)
	return nil
}

// closeCallback .
// It can be confirmed that closeCallback and onRequest will not be executed concurrently.
// If onRequest is still running, it will trigger closeCallback on exit.
func (c *connection) closeCallback(needLock bool) (err error) {
	if needLock && !c.lock(processing) {
		return nil
	}
	var latest = c.callbacks.Load()
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
	if c.operator.poll != nil {
		err = c.operator.Control(PollModReadable)
	} else {
		c.operator.poll = pollmanager.Pick()
		err = c.operator.Control(PollReadable)
	}
	if err != nil {
		log.Println("connection register failed:", err.Error())
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
