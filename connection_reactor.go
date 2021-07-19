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
	"sync/atomic"
	"syscall"
)

// ------------------------------------------ implement FDOperator ------------------------------------------

// onHup means close by poller.
func (c *connection) onHup(p Poll) error {
	if c.closeBy(poller) {
		c.triggerRead()
		c.triggerWrite(ErrConnClosed)
		// It depends on closing by user if OnRequest is nil, otherwise it needs to be released actively.
		// It can be confirmed that the OnRequest goroutine has been exited before closecallback executing,
		// and it is safe to close the buffer at this time.
		if process, _ := c.process.Load().(OnRequest); process != nil {
			c.closeCallback(true)
		}
	}
	return nil
}

// onClose means close by user.
func (c *connection) onClose() error {
	if c.closeBy(user) {
		// If Close is called during OnPrepare, poll is not registered.
		if c.operator.poll != nil {
			c.operator.Control(PollDetach)
		}
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
	c.stop(writing)
	if c.lock(inputBuffer) {
		c.inputBuffer.Close()
		barrierPool.Put(c.inputBarrier)
	}
	c.outputBuffer.Close()
	barrierPool.Put(c.outputBarrier)
}

// inputs implements FDOperator.
func (c *connection) inputs(vs [][]byte) (rs [][]byte) {
	n := int(atomic.LoadInt32(&c.waitReadSize))
	if n <= pagesize {
		return c.inputBuffer.Book(pagesize, vs)
	}

	n -= c.inputBuffer.Len()
	if n < pagesize {
		n = pagesize
	}
	return c.inputBuffer.Book(n, vs)
}

// inputAck implements FDOperator.
func (c *connection) inputAck(n int) (err error) {
	if n < 0 {
		n = 0
	}
	lack := atomic.AddInt32(&c.waitReadSize, int32(-n))
	err = c.inputBuffer.BookAck(n, lack <= 0)
	c.triggerRead()
	c.onRequest()
	return err
}

// outputs implements FDOperator.
func (c *connection) outputs(vs [][]byte) (rs [][]byte, supportZeroCopy bool) {
	if !c.lock(writing) {
		return rs, c.supportZeroCopy
	}
	if c.outputBuffer.IsEmpty() {
		c.unlock(writing)
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
	// must unlock before check empty
	c.unlock(writing)
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

// flush write data directly.
func (c *connection) flush() error {
	if !c.lock(writing) {
		return nil
	}
	locked := true
	defer func() {
		if locked {
			c.unlock(writing)
		}
	}()
	if c.outputBuffer.IsEmpty() {
		return nil
	}
	// TODO: Let the upper layer pass in whether to use ZeroCopy.
	var bs = c.outputBuffer.GetBytes(c.outputBarrier.bs)
	var n, err = sendmsg(c.fd, bs, c.outputBarrier.ivs, false && c.supportZeroCopy)
	if err != nil && err != syscall.EAGAIN {
		return Exception(err, "when flush")
	}
	if n > 0 {
		err = c.outputBuffer.Skip(n)
		c.outputBuffer.Release()
		if err != nil {
			return Exception(err, "when flush")
		}
	}
	// return if write all buffer.
	if c.outputBuffer.IsEmpty() {
		return nil
	}
	err = c.operator.Control(PollR2RW)
	if err != nil {
		return Exception(err, "when flush")
	}

	locked = false
	c.unlock(writing)
	err = <-c.writeTrigger
	return err
}
