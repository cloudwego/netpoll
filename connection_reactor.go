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
	"sync/atomic"
)

// ------------------------------------------ implement FDOperator ------------------------------------------

// onHup means close by poller.
func (c *connection) onHup(p Poll) error {
	if c.closeBy(poller) {
		c.triggerRead()
		c.triggerWrite(ErrConnClosed)
		// It depends on closing by user if OnConnect and OnRequest is nil, otherwise it needs to be released actively.
		// It can be confirmed that the OnRequest goroutine has been exited before closecallback executing,
		// and it is safe to close the buffer at this time.
		var onConnect, _ = c.onConnectCallback.Load().(OnConnect)
		var onRequest, _ = c.onRequestCallback.Load().(OnRequest)
		if onConnect != nil || onRequest != nil {
			c.closeCallback(true)
		}
	}
	return nil
}

// onClose means close by user.
func (c *connection) onClose() error {
	if c.closeBy(user) {
		c.triggerRead()
		c.triggerWrite(ErrConnClosed)
		c.closeCallback(true)
		return nil
	}
	if c.isCloseBy(poller) {
		// Connection with OnRequest of nil
		// relies on the user to actively close the connection to recycle resources.
		c.closeCallback(true)
	}
	return nil
}

// closeBuffer recycle input & output LinkBuffer.
func (c *connection) closeBuffer() {
	var onConnect, _ = c.onConnectCallback.Load().(OnConnect)
	var onRequest, _ = c.onRequestCallback.Load().(OnRequest)
	// if client close the connection, we cannot ensure that the poller is not process the buffer,
	// so we need to check the buffer length, and if it's an "unclean" close operation, let's give up to reuse the buffer
	if c.inputBuffer.Len() == 0 || onConnect != nil || onRequest != nil {
		c.inputBuffer.Close()
		barrierPool.Put(c.inputBarrier)
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

	var needTrigger = true
	if length == n { // first start onRequest
		needTrigger = c.onRequest()
	}
	if needTrigger && length >= int(atomic.LoadInt64(&c.waitReadSize)) {
		c.triggerRead()
	}
	return nil
}

// outputs implements FDOperator.
func (c *connection) outputs(vs [][]byte) (rs [][]byte, supportZeroCopy bool) {
	if c.outputBuffer.IsEmpty() {
		c.rw2r()
		return rs, c.supportZeroCopy
	}
	rs = c.outputBuffer.GetBytes(vs)
	return rs, c.supportZeroCopy
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
	c.operator.Control(PollRW2R)
	c.triggerWrite(nil)
}
