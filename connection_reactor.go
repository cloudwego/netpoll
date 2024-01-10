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
	if !c.closeBy(poller) {
		return nil
	}
	c.triggerRead(Exception(ErrEOF, "peer close"))
	c.triggerWrite(Exception(ErrConnClosed, "peer close"))
	// It depends on closing by user if OnConnect and OnRequest is nil, otherwise it needs to be released actively.
	// It can be confirmed that the OnRequest goroutine has been exited before closeCallback executing,
	// and it is safe to close the buffer at this time.
	var onConnect = c.onConnectCallback.Load()
	var onRequest = c.onRequestCallback.Load()
	var needCloseByUser = onConnect == nil && onRequest == nil
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
	var onConnect, _ = c.onConnectCallback.Load().(OnConnect)
	var onRequest, _ = c.onRequestCallback.Load().(OnRequest)
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
	// trigger throttle
	if c.readBufferThreshold > 0 && int64(c.inputBuffer.Len()) >= c.readBufferThreshold {
		c.pauseRead()
		return
	}

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

	// trigger throttle
	if c.readBufferThreshold > 0 && int64(length) >= c.readBufferThreshold {
		c.pauseRead()
	}

	var needTrigger = true
	if length == n { // first start onRequest
		needTrigger = c.onRequest()
	}
	if needTrigger && length >= int(atomic.LoadInt64(&c.waitReadSize)) {
		c.triggerRead(nil)
	}
	return nil
}

// outputs implements FDOperator.
func (c *connection) outputs(vs [][]byte) (rs [][]byte, supportZeroCopy bool) {
	if c.outputBuffer.IsEmpty() {
		c.pauseWrite()
		c.triggerWrite(nil)
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
		c.pauseWrite()
		c.triggerWrite(nil)
	}
	return nil
}

/* The race description of operator event monitoring
- Pause operation will remove old event monitor of operator
- Resume operation will add new event monitor of operator
- Only poller could use Pause to remove event monitor, and poller already hold the op.do() locker
- Only user could use Resume, and user's operation maybe compete with poller's operation
- If competition happen, because of all resume operation will monitor all events, it's safe to do that with a race condition.
    * If resume first and pause latter, poller will monitor the accurate events it needs.
    * If pause first and resume latter,  poller will monitor the duplicate events which will be removed after next poller triggered.
      And poller will ensure to remove the duplicate events.
- If there is no readBufferThreshold option, the code path will be more simple and efficient.
*/

// pauseWrite removed the monitoring of write events.
// pauseWrite used in poller
func (c *connection) pauseWrite() {
	c.operator.Control(PollRW2R)
}

// resumeWrite add the monitoring of write events.
// resumeWrite used by users
func (c *connection) resumeWrite() {
	c.operator.Control(PollR2RW)
}

// pauseRead removed the monitoring of read events.
// pauseRead used in poller
func (c *connection) pauseRead() {
	c.operator.Control(PollRW2W)
}

// resumeRead add the monitoring of read events.
// resumeRead used by users
func (c *connection) resumeRead() {
	c.operator.Control(PollW2RW)
}
