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

package netpoll

import (
	"sync/atomic"
)

// ------------------------------------------ implement FDOperator ------------------------------------------

// onHup means close by poller.
func (c *connection) onHup(p Poll) error {
	if !c.closeBy(poller) {
		return nil
	}
	c.triggerRead(Exception(ErrEOF, "peer close"))
	c.triggerWrite(Exception(ErrConnClosed, "peer close"))

	// call Disconnect callback first
	c.onDisconnect()

	// It depends on closing by user if OnConnect and OnRequest is nil, otherwise it needs to be released actively.
	// It can be confirmed that the OnRequest goroutine has been exited before closeCallback executing,
	// and it is safe to close the buffer at this time.
	onConnect := c.onConnectCallback.Load()
	onRequest := c.onRequestCallback.Load()
	needCloseByUser := onConnect == nil && onRequest == nil
	if !needCloseByUser {
		// already PollDetach when call OnHup
		c.closeCallback(true, false)
	}
	return nil
}

// onClose means close by user.
func (c *connection) onClose() error {
	// user code close the connection
	if c.closeBy(user) {
		c.triggerRead(Exception(ErrConnClosed, "self close"))
		c.triggerWrite(Exception(ErrConnClosed, "self close"))
		// Detach from poller when processing finished, otherwise it will cause race
		c.closeCallback(true, true)
		return nil
	}

	// closed by poller
	// still need to change closing status to `user` since OnProcess should not be processed again
	c.force(closing, user)

	// user code should actively close the connection to recycle resources.
	// poller already detached operator
	return c.closeCallback(true, false)
}

// closeBuffer recycle input & output LinkBuffer.
func (c *connection) closeBuffer() {
	onConnect, _ := c.onConnectCallback.Load().(OnConnect)
	onRequest, _ := c.onRequestCallback.Load().(OnRequest)
	// if client close the connection, we cannot ensure that the poller is not process the buffer,
	// so we need to check the buffer length, and if it's an "unclean" close operation, let's give up to reuse the buffer
	if c.inputBuffer.Len() == 0 || onConnect != nil || onRequest != nil {
		c.inputBuffer.Close()
	}
	if c.outputBuffer.Len() == 0 || onConnect != nil || onRequest != nil {
		c.outputBuffer.Close()
		barrierPool.Put(c.outputBarrier)
	}
}

// inputs implements FDOperator.
func (c *connection) inputs(vs [][]byte) (rs [][]byte) {
	vs[0] = c.inputBuffer.book(c.bookSize, c.maxSize)
	return vs[:1]
}

// inputAck implements FDOperator.
func (c *connection) inputAck(n int) (err error) {
	if n <= 0 {
		c.inputBuffer.bookAck(0)
		return nil
	}

	// Auto size bookSize.
	if n == c.bookSize && c.bookSize < mallocMax {
		c.bookSize <<= 1
	}

	length, _ := c.inputBuffer.bookAck(n)
	if c.maxSize < length {
		c.maxSize = length
	}
	if c.maxSize > mallocMax {
		c.maxSize = mallocMax
	}

	needTrigger := true
	if length == n { // first start onRequest
		needTrigger = c.onRequest()
	}
	if needTrigger && length >= int(atomic.LoadInt64(&c.waitReadSize)) {
		c.triggerRead(nil)
	}
	return nil
}

// outputs implements FDOperator.
func (c *connection) outputs(vs [][]byte) (rs [][]byte, _ bool) {
	if c.outputBuffer.IsEmpty() {
		c.rw2r()
		return rs, false
	}
	rs = c.outputBuffer.GetBytes(vs)
	return rs, false
}

// outputAck implements FDOperator.
func (c *connection) outputAck(n int) (err error) {
	if n > 0 {
		c.outputBuffer.Skip(n)
		c.outputBuffer.Release()
	}
	if c.outputBuffer.IsEmpty() {
		c.rw2r()
	}
	return nil
}

// rw2r removed the monitoring of write events.
func (c *connection) rw2r() {
	c.pollEventMu.Lock()
	c.writeWatching = false
	if c.readPaused {
		c.operator.Control(PollW2N)
	} else {
		c.operator.Control(PollRW2R)
	}
	c.pollEventMu.Unlock()
	c.triggerWrite(nil)
}

// PauseRead removes readable event monitoring for this connection.
// It is idempotent and preserves writable event monitoring if it is currently enabled.
func (c *connection) PauseRead() error {
	c.pollEventMu.Lock()
	defer c.pollEventMu.Unlock()

	if !c.IsActive() {
		return Exception(ErrConnClosed, "when pause read")
	}
	if c.readPaused {
		return nil
	}
	c.readPaused = true

	var err error
	if c.writeWatching {
		err = c.operator.Control(PollRW2W)
	} else {
		err = c.operator.Control(PollR2N)
	}
	if err != nil {
		c.readPaused = false
		return err
	}
	return nil
}

// ResumeRead adds readable event monitoring for this connection.
// It is idempotent and preserves writable event monitoring if it is currently enabled.
func (c *connection) ResumeRead() error {
	c.pollEventMu.Lock()
	defer c.pollEventMu.Unlock()

	if !c.IsActive() {
		return Exception(ErrConnClosed, "when resume read")
	}
	if !c.readPaused {
		return nil
	}
	c.readPaused = false

	var err error
	if c.writeWatching {
		err = c.operator.Control(PollW2RW)
	} else {
		err = c.operator.Control(PollN2R)
	}
	if err != nil {
		c.readPaused = true
		return err
	}
	return nil
}

// IsReadPaused checks whether readable event monitoring is paused.
func (c *connection) IsReadPaused() bool {
	c.pollEventMu.Lock()
	defer c.pollEventMu.Unlock()
	return c.readPaused
}
