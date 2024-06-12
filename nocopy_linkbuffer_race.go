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

//go:build race
// +build race

package netpoll

import (
	"sync"
)

type LinkBuffer = SafeLinkBuffer

// SafeLinkBuffer only used to in go tests with -race
type SafeLinkBuffer struct {
	sync.Mutex
	UnsafeLinkBuffer
}

// ------------------------------------------ implement zero-copy reader ------------------------------------------

// Next implements Reader.
func (b *SafeLinkBuffer) Next(n int) (p []byte, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Next(n)
}

// Peek implements Reader.
func (b *SafeLinkBuffer) Peek(n int) (p []byte, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Peek(n)
}

// Skip implements Reader.
func (b *SafeLinkBuffer) Skip(n int) (err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Skip(n)
}

// Until implements Reader.
func (b *SafeLinkBuffer) Until(delim byte) (line []byte, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Until(delim)
}

// Release implements Reader.
func (b *SafeLinkBuffer) Release() (err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Release()
}

// ReadString implements Reader.
func (b *SafeLinkBuffer) ReadString(n int) (s string, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.ReadString(n)
}

// ReadBinary implements Reader.
func (b *SafeLinkBuffer) ReadBinary(n int) (p []byte, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.ReadBinary(n)
}

// ReadByte implements Reader.
func (b *SafeLinkBuffer) ReadByte() (p byte, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.ReadByte()
}

// Slice implements Reader.
func (b *SafeLinkBuffer) Slice(n int) (r Reader, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Slice(n)
}

// ------------------------------------------ implement zero-copy writer ------------------------------------------

// Malloc implements Writer.
func (b *SafeLinkBuffer) Malloc(n int) (buf []byte, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Malloc(n)
}

// MallocLen implements Writer.
func (b *SafeLinkBuffer) MallocLen() (length int) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.MallocLen()
}

// MallocAck implements Writer.
func (b *SafeLinkBuffer) MallocAck(n int) (err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.MallocAck(n)
}

// Flush implements Writer.
func (b *SafeLinkBuffer) Flush() (err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Flush()
}

// Append implements Writer.
func (b *SafeLinkBuffer) Append(w Writer) (err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Append(w)
}

// WriteBuffer implements Writer.
func (b *SafeLinkBuffer) WriteBuffer(buf *LinkBuffer) (err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.WriteBuffer(buf)
}

// WriteString implements Writer.
func (b *SafeLinkBuffer) WriteString(s string) (n int, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.WriteString(s)
}

// WriteBinary implements Writer.
func (b *SafeLinkBuffer) WriteBinary(p []byte) (n int, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.WriteBinary(p)
}

// WriteDirect cannot be mixed with WriteString or WriteBinary functions.
func (b *SafeLinkBuffer) WriteDirect(p []byte, remainLen int) error {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.WriteDirect(p, remainLen)
}

// WriteByte implements Writer.
func (b *SafeLinkBuffer) WriteByte(p byte) (err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.WriteByte(p)
}

// Close will recycle all buffer.
func (b *SafeLinkBuffer) Close() (err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Close()
}

// ------------------------------------------ implement connection interface ------------------------------------------

// Bytes returns all the readable bytes of this SafeLinkBuffer.
func (b *SafeLinkBuffer) Bytes() []byte {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.Bytes()
}

// GetBytes will read and fill the slice p as much as possible.
func (b *SafeLinkBuffer) GetBytes(p [][]byte) (vs [][]byte) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.GetBytes(p)
}

// book will grow and malloc buffer to hold data.
//
// bookSize: The size of data that can be read at once.
// maxSize: The maximum size of data between two Release(). In some cases, this can
//
//	guarantee all data allocated in one node to reduce copy.
func (b *SafeLinkBuffer) book(bookSize, maxSize int) (p []byte) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.book(bookSize, maxSize)
}

// bookAck will ack the first n malloc bytes and discard the rest.
//
// length: The size of data in inputBuffer. It is used to calculate the maxSize
func (b *SafeLinkBuffer) bookAck(n int) (length int, err error) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.bookAck(n)
}

// calcMaxSize will calculate the data size between two Release()
func (b *SafeLinkBuffer) calcMaxSize() (sum int) {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.calcMaxSize()
}

func (b *SafeLinkBuffer) resetTail(maxSize int) {
	b.Lock()
	defer b.Unlock()
	b.UnsafeLinkBuffer.resetTail(maxSize)
}

func (b *SafeLinkBuffer) indexByte(c byte, skip int) int {
	b.Lock()
	defer b.Unlock()
	return b.UnsafeLinkBuffer.indexByte(c, skip)
}
