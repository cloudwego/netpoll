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
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	defaultZeroCopyTimeoutSec = 60
)

// connection is the implement of Connection
type connection struct {
	netFD
	onEvent
	locker
	operator        *FDOperator
	readTimeout     time.Duration
	readTimer       *time.Timer
	readTrigger     chan error
	waitReadSize    int64
	writeTimeout    time.Duration
	writeTimer      *time.Timer
	writeTrigger    chan error
	inputBuffer     *LinkBuffer
	outputBuffer    *LinkBuffer
	inputBarrier    *barrier
	outputBarrier   *barrier
	supportZeroCopy bool
	maxSize         int // The maximum size of data between two Release().
	bookSize        int // The size of data that can be read at once.
}

var (
	_ Connection = &connection{}
	_ Reader     = &connection{}
	_ Writer     = &connection{}
)

// Reader implements Connection.
func (c *connection) Reader() Reader {
	return c
}

// Writer implements Connection.
func (c *connection) Writer() Writer {
	return c
}

// IsActive implements Connection.
func (c *connection) IsActive() bool {
	return c.isCloseBy(none)
}

// SetIdleTimeout implements Connection.
func (c *connection) SetIdleTimeout(timeout time.Duration) error {
	if timeout > 0 {
		return c.SetKeepAlive(int(timeout.Seconds()))
	}
	return nil
}

// SetReadTimeout implements Connection.
func (c *connection) SetReadTimeout(timeout time.Duration) error {
	if timeout >= 0 {
		c.readTimeout = timeout
	}
	return nil
}

// SetWriteTimeout implements Connection.
func (c *connection) SetWriteTimeout(timeout time.Duration) error {
	if timeout >= 0 {
		c.writeTimeout = timeout
	}
	return nil
}

// ------------------------------------------ implement zero-copy reader ------------------------------------------

// Next implements Connection.
func (c *connection) Next(n int) (p []byte, err error) {
	if err = c.waitRead(n); err != nil {
		return p, err
	}
	return c.inputBuffer.Next(n)
}

// Peek implements Connection.
func (c *connection) Peek(n int) (buf []byte, err error) {
	if err = c.waitRead(n); err != nil {
		return buf, err
	}
	return c.inputBuffer.Peek(n)
}

// Skip implements Connection.
func (c *connection) Skip(n int) (err error) {
	if err = c.waitRead(n); err != nil {
		return err
	}
	return c.inputBuffer.Skip(n)
}

// Release implements Connection.
func (c *connection) Release() (err error) {
	// Check inputBuffer length first to reduce contention in mux situation.
	// c.operator.do competes with c.inputs/c.inputAck
	if c.inputBuffer.Len() == 0 && c.operator.do() {
		maxSize := c.inputBuffer.calcMaxSize()
		// Set the maximum value of maxsize equal to mallocMax to prevent GC pressure.
		if maxSize > mallocMax {
			maxSize = mallocMax
		}

		if maxSize > c.maxSize {
			c.maxSize = maxSize
		}
		// Double check length to reset tail node
		if c.inputBuffer.Len() == 0 {
			c.inputBuffer.resetTail(c.maxSize)
		}
		c.operator.done()
	}
	return c.inputBuffer.Release()
}

// Slice implements Connection.
func (c *connection) Slice(n int) (r Reader, err error) {
	if err = c.waitRead(n); err != nil {
		return nil, err
	}
	return c.inputBuffer.Slice(n)
}

// Len implements Connection.
func (c *connection) Len() (length int) {
	return c.inputBuffer.Len()
}

// Until implements Connection.
func (c *connection) Until(delim byte) (line []byte, err error) {
	var n, l int
	for {
		if err = c.waitRead(n + 1); err != nil {
			// return all the data in the buffer
			line, _ = c.inputBuffer.Next(c.inputBuffer.Len())
			return
		}

		l = c.inputBuffer.Len()
		i := c.inputBuffer.indexByte(delim, n)
		if i < 0 {
			n = l // skip all exists bytes
			continue
		}
		return c.Next(i + 1)
	}
}

// ReadString implements Connection.
func (c *connection) ReadString(n int) (s string, err error) {
	if err = c.waitRead(n); err != nil {
		return s, err
	}
	return c.inputBuffer.ReadString(n)
}

// ReadBinary implements Connection.
func (c *connection) ReadBinary(n int) (p []byte, err error) {
	if err = c.waitRead(n); err != nil {
		return p, err
	}
	return c.inputBuffer.ReadBinary(n)
}

// ReadByte implements Connection.
func (c *connection) ReadByte() (b byte, err error) {
	if err = c.waitRead(1); err != nil {
		return b, err
	}
	return c.inputBuffer.ReadByte()
}

// ------------------------------------------ implement zero-copy writer ------------------------------------------

// Malloc implements Connection.
func (c *connection) Malloc(n int) (buf []byte, err error) {
	return c.outputBuffer.Malloc(n)
}

// MallocLen implements Connection.
func (c *connection) MallocLen() (length int) {
	return c.outputBuffer.MallocLen()
}

// Flush will send all malloc data to the peer,
// so must confirm that the allocated bytes have been correctly assigned.
//
// Flush first checks whether the out buffer is empty.
// If empty, it will call syscall.Write to send data directly,
// otherwise the buffer will be sent asynchronously by the epoll trigger.
func (c *connection) Flush() error {
	if !c.IsActive() || !c.lock(flushing) {
		return Exception(ErrConnClosed, "when flush")
	}
	defer c.unlock(flushing)
	c.outputBuffer.Flush()
	return c.flush()
}

// MallocAck implements Connection.
func (c *connection) MallocAck(n int) (err error) {
	return c.outputBuffer.MallocAck(n)
}

// Append implements Connection.
func (c *connection) Append(w Writer) (err error) {
	return c.outputBuffer.Append(w)
}

// WriteString implements Connection.
func (c *connection) WriteString(s string) (n int, err error) {
	return c.outputBuffer.WriteString(s)
}

// WriteBinary implements Connection.
func (c *connection) WriteBinary(b []byte) (n int, err error) {
	return c.outputBuffer.WriteBinary(b)
}

// WriteDirect implements Connection.
func (c *connection) WriteDirect(p []byte, remainCap int) (err error) {
	return c.outputBuffer.WriteDirect(p, remainCap)
}

// WriteByte implements Connection.
func (c *connection) WriteByte(b byte) (err error) {
	return c.outputBuffer.WriteByte(b)
}

// ------------------------------------------ implement net.Conn ------------------------------------------

// Read behavior is the same as net.Conn, it will return io.EOF if buffer is empty.
func (c *connection) Read(p []byte) (n int, err error) {
	l := len(p)
	if l == 0 {
		return 0, nil
	}
	if err = c.waitRead(1); err != nil {
		return 0, err
	}
	if has := c.inputBuffer.Len(); has < l {
		l = has
	}
	src, err := c.inputBuffer.Next(l)
	n = copy(p, src)
	if err == nil {
		err = c.inputBuffer.Release()
	}
	return n, err
}

// Write will Flush soon.
func (c *connection) Write(p []byte) (n int, err error) {
	if !c.IsActive() || !c.lock(flushing) {
		return 0, Exception(ErrConnClosed, "when write")
	}
	defer c.unlock(flushing)

	dst, _ := c.outputBuffer.Malloc(len(p))
	n = copy(dst, p)
	c.outputBuffer.Flush()
	err = c.flush()
	return n, err
}

// Close implements Connection.
func (c *connection) Close() error {
	return c.onClose()
}

// Detach detaches the connection from poller but doesn't close it.
func (c *connection) Detach() error {
	c.detaching = true
	return c.onClose()
}

// ------------------------------------------ private ------------------------------------------

var barrierPool = sync.Pool{
	New: func() interface{} {
		return &barrier{
			bs:  make([][]byte, barriercap),
			ivs: make([]syscall.Iovec, barriercap),
		}
	},
}

// init initialize the connection with options
func (c *connection) init(conn Conn, opts *options) (err error) {
	// init buffer, barrier, finalizer
	c.readTrigger = make(chan error, 1)
	c.writeTrigger = make(chan error, 1)
	c.bookSize, c.maxSize = pagesize, pagesize
	c.inputBuffer, c.outputBuffer = NewLinkBuffer(pagesize), NewLinkBuffer()
	c.inputBarrier, c.outputBarrier = barrierPool.Get().(*barrier), barrierPool.Get().(*barrier)

	c.initNetFD(conn) // conn must be *netFD{}
	c.initFDOperator()
	c.initFinalizer()

	syscall.SetNonblock(c.fd, true)
	// enable TCP_NODELAY by default
	switch c.network {
	case "tcp", "tcp4", "tcp6":
		setTCPNoDelay(c.fd, true)
	}
	// check zero-copy
	if setZeroCopy(c.fd) == nil && setBlockZeroCopySend(c.fd, defaultZeroCopyTimeoutSec, 0) == nil {
		c.supportZeroCopy = true
	}

	// connection initialized and prepare options
	return c.onPrepare(opts)
}

func (c *connection) initNetFD(conn Conn) {
	if nfd, ok := conn.(*netFD); ok {
		c.netFD = *nfd
		return
	}
	c.netFD = netFD{
		fd:         conn.Fd(),
		localAddr:  conn.LocalAddr(),
		remoteAddr: conn.RemoteAddr(),
	}
}

func (c *connection) initFDOperator() {
	poll := pollmanager.Pick()
	op := poll.Alloc()
	op.FD = c.fd
	op.OnRead, op.OnWrite, op.OnHup = nil, nil, c.onHup
	op.Inputs, op.InputAck = c.inputs, c.inputAck
	op.Outputs, op.OutputAck = c.outputs, c.outputAck
	c.operator = op
}

func (c *connection) initFinalizer() {
	c.AddCloseCallback(func(connection Connection) (err error) {
		c.stop(flushing)
		c.operator.Free()
		if err = c.netFD.Close(); err != nil {
			logger.Printf("NETPOLL: netFD close failed: %v", err)
		}
		c.closeBuffer()
		return nil
	})
}

func (c *connection) triggerRead(err error) {
	select {
	case c.readTrigger <- err:
	default:
	}
}

func (c *connection) triggerWrite(err error) {
	select {
	case c.writeTrigger <- err:
	default:
	}
}

// waitRead will wait full n bytes.
func (c *connection) waitRead(n int) (err error) {
	if n <= c.inputBuffer.Len() {
		return nil
	}
	atomic.StoreInt64(&c.waitReadSize, int64(n))
	defer atomic.StoreInt64(&c.waitReadSize, 0)
	if c.readTimeout > 0 {
		return c.waitReadWithTimeout(n)
	}
	// wait full n
	for c.inputBuffer.Len() < n {
		switch c.status(closing) {
		case poller:
			return Exception(ErrEOF, "wait read")
		case user:
			return Exception(ErrConnClosed, "wait read")
		default:
			err = <-c.readTrigger
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// waitReadWithTimeout will wait full n bytes or until timeout.
func (c *connection) waitReadWithTimeout(n int) (err error) {
	// set read timeout
	if c.readTimer == nil {
		c.readTimer = time.NewTimer(c.readTimeout)
	} else {
		c.readTimer.Reset(c.readTimeout)
	}

	for c.inputBuffer.Len() < n {
		switch c.status(closing) {
		case poller:
			// cannot return directly, stop timer first!
			err = Exception(ErrEOF, "wait read")
			goto RET
		case user:
			// cannot return directly, stop timer first!
			err = Exception(ErrConnClosed, "wait read")
			goto RET
		default:
			select {
			case <-c.readTimer.C:
				// double check if there is enough data to be read
				if c.inputBuffer.Len() >= n {
					return nil
				}
				return Exception(ErrReadTimeout, c.remoteAddr.String())
			case err = <-c.readTrigger:
				if err != nil {
					return err
				}
				continue
			}
		}
	}
RET:
	// clean timer.C
	if !c.readTimer.Stop() {
		<-c.readTimer.C
	}
	return err
}

// flush write data directly.
func (c *connection) flush() error {
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

	return c.waitFlush()
}

func (c *connection) waitFlush() (err error) {
	if c.writeTimeout == 0 {
		select {
		case err = <-c.writeTrigger:
		}
		return err
	}

	// set write timeout
	if c.writeTimer == nil {
		c.writeTimer = time.NewTimer(c.writeTimeout)
	} else {
		c.writeTimer.Reset(c.writeTimeout)
	}

	select {
	case err = <-c.writeTrigger:
		if !c.writeTimer.Stop() { // clean timer
			<-c.writeTimer.C
		}
		return err
	case <-c.writeTimer.C:
		select {
		// try fetch writeTrigger if both cases fires
		case err = <-c.writeTrigger:
			return err
		default:
		}
		// if timeout, remove write event from poller
		// we cannot flush it again, since we don't if the poller is still process outputBuffer
		c.operator.Control(PollRW2R)
		return Exception(ErrWriteTimeout, c.remoteAddr.String())
	}
}
