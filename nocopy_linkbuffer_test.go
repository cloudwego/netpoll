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
	"bytes"
	"fmt"
	"sync/atomic"
	"testing"
)

func TestLinkBuffer(t *testing.T) {
	// clean & new
	LinkBufferCap = 128

	buf := NewLinkBuffer()
	Equal(t, buf.Len(), 0)
	MustTrue(t, buf.IsEmpty())

	head := buf.head

	p, err := buf.Next(10)
	Equal(t, len(p), 0)
	MustTrue(t, err != nil)

	buf.Malloc(128)
	MustTrue(t, buf.IsEmpty())

	p, err = buf.Peek(10)
	Equal(t, len(p), 0)
	MustTrue(t, err != nil)

	buf.Flush()
	Equal(t, buf.Len(), 128)
	MustTrue(t, !buf.IsEmpty())

	p, err = buf.Next(28)
	Equal(t, len(p), 28)
	Equal(t, buf.Len(), 100)
	MustNil(t, err)

	p, err = buf.Peek(90)
	Equal(t, len(p), 90)
	Equal(t, buf.Len(), 100)
	MustNil(t, err)

	read := buf.read
	Equal(t, buf.head, head)
	err = buf.Release()
	MustNil(t, err)
	Equal(t, buf.head, read)

	inputs := buf.book(block1k, block8k)
	Equal(t, len(inputs), block1k)
	Equal(t, buf.Len(), 100)

	buf.MallocAck(block1k)
	Equal(t, buf.Len(), 100)
	Equal(t, buf.MallocLen(), block1k)
	buf.Flush()
	Equal(t, buf.Len(), 100+block1k)
	Equal(t, buf.MallocLen(), 0)

	outputs := buf.GetBytes(make([][]byte, 16))
	Equal(t, len(outputs), 2)

	err = buf.Skip(block1k)
	MustNil(t, err)
	Equal(t, buf.Len(), 100)
}

// TestLinkBufferWithZero test more case with n is invalid.
func TestLinkBufferWithInvalid(t *testing.T) {
	// clean & new
	LinkBufferCap = 128

	buf := NewLinkBuffer()
	Equal(t, buf.Len(), 0)
	MustTrue(t, buf.IsEmpty())

	for n := 0; n > -5; n-- {
		// test writer
		p, err := buf.Malloc(n)
		Equal(t, len(p), 0)
		Equal(t, buf.MallocLen(), 0)
		Equal(t, buf.Len(), 0)
		MustNil(t, err)

		var wn int
		wn, err = buf.WriteString("")
		Equal(t, wn, 0)
		Equal(t, buf.MallocLen(), 0)
		Equal(t, buf.Len(), 0)
		MustNil(t, err)

		wn, err = buf.WriteBinary(nil)
		Equal(t, wn, 0)
		Equal(t, buf.MallocLen(), 0)
		Equal(t, buf.Len(), 0)
		MustNil(t, err)

		err = buf.WriteDirect(nil, n)
		Equal(t, buf.MallocLen(), 0)
		Equal(t, buf.Len(), 0)
		MustNil(t, err)

		var w *LinkBuffer
		err = buf.Append(w)
		Equal(t, buf.MallocLen(), 0)
		Equal(t, buf.Len(), 0)
		MustNil(t, err)

		err = buf.MallocAck(n)
		Equal(t, buf.MallocLen(), 0)
		Equal(t, buf.Len(), 0)
		if n == 0 {
			MustNil(t, err)
		} else {
			MustTrue(t, err != nil)
		}

		err = buf.Flush()
		MustNil(t, err)

		// test reader
		p, err = buf.Next(n)
		Equal(t, len(p), 0)
		MustNil(t, err)

		p, err = buf.Peek(n)
		Equal(t, len(p), 0)
		MustNil(t, err)

		err = buf.Skip(n)
		Equal(t, len(p), 0)
		MustNil(t, err)

		var s string
		s, err = buf.ReadString(n)
		Equal(t, len(s), 0)
		MustNil(t, err)

		p, err = buf.ReadBinary(n)
		Equal(t, len(p), 0)
		MustNil(t, err)

		var r Reader
		r, err = buf.Slice(n)
		Equal(t, r.Len(), 0)
		MustNil(t, err)

		err = buf.Release()
		MustNil(t, err)
	}
}

// cross-block operation test
func TestLinkBufferIndex(t *testing.T) {
	// clean & new
	LinkBufferCap = 8

	buf := NewLinkBuffer()
	Equal(t, buf.Len(), 0)
	MustTrue(t, buf.IsEmpty())
	var p []byte

	p, _ = buf.Malloc(15)
	Equal(t, len(p), 15)
	MustTrue(t, buf.read == buf.flush)
	Equal(t, buf.read.off, 0)
	Equal(t, buf.read.malloc, 0)
	Equal(t, buf.write.off, 0)
	Equal(t, buf.write.malloc, 15)
	Equal(t, cap(buf.write.buf), 16) // mcache up-aligned to the power of 2

	p, _ = buf.Malloc(7)
	Equal(t, len(p), 7)
	MustTrue(t, buf.read == buf.flush)
	Equal(t, buf.read.off, 0)
	Equal(t, buf.read.malloc, 0)
	Equal(t, buf.write.off, 0)
	Equal(t, buf.write.malloc, 7)
	Equal(t, cap(buf.write.buf), LinkBufferCap)

	buf.Flush()
	MustTrue(t, buf.read != buf.flush)
	MustTrue(t, buf.flush == buf.write)
	Equal(t, buf.read.off, 0)
	Equal(t, len(buf.read.buf), 0)
	Equal(t, buf.read.next.off, 0)
	Equal(t, len(buf.read.next.buf), 15)
	Equal(t, buf.flush.off, 0)
	Equal(t, buf.flush.malloc, 7)
	Equal(t, len(buf.flush.buf), 7)

	p, _ = buf.Next(13)
	Equal(t, len(p), 13)
	MustTrue(t, buf.read != buf.flush)
	Equal(t, buf.read.off, 13)
	Equal(t, buf.read.Len(), 2)
	Equal(t, buf.flush.off, 0)
	Equal(t, buf.flush.malloc, 7)

	p, _ = buf.Peek(4)
	Equal(t, len(p), 4)
	MustTrue(t, buf.read != buf.flush)
	Equal(t, buf.read.off, 13)
	Equal(t, buf.read.Len(), 2)
	Equal(t, buf.flush.off, 0)
	Equal(t, buf.flush.malloc, 7)

	buf.book(block8k, block8k)
	MustTrue(t, buf.flush == buf.write)
	Equal(t, buf.flush.off, 0)
	Equal(t, buf.flush.malloc, 8)
	Equal(t, buf.flush.Len(), 7)
	Equal(t, buf.write.off, 0)
	Equal(t, buf.write.malloc, 8)
	Equal(t, buf.write.Len(), 7)

	buf.book(block8k, block8k)
	MustTrue(t, buf.flush != buf.write)
	Equal(t, buf.flush.off, 0)
	Equal(t, buf.flush.malloc, 8)
	Equal(t, buf.flush.Len(), 7)
	Equal(t, buf.write.off, 0)
	Equal(t, buf.write.malloc, 8192)
	Equal(t, buf.write.Len(), 0)

	buf.MallocAck(5)
	MustTrue(t, buf.flush != buf.write)
	Equal(t, buf.write.off, 0)
	Equal(t, buf.write.malloc, 4)
	Equal(t, buf.write.Len(), 0)
	MustTrue(t, buf.write.next == nil)
	buf.Flush()

	p, _ = buf.Next(8)
	Equal(t, len(p), 8)
	MustTrue(t, buf.read != buf.flush)
	Equal(t, buf.read.off, 6)
	Equal(t, buf.read.Len(), 2)
	Equal(t, buf.flush.off, 0)
	Equal(t, buf.flush.malloc, 4)
	Equal(t, buf.flush.Len(), 4)

	err := buf.Skip(3)
	MustNil(t, err)
	MustTrue(t, buf.read == buf.flush)
	Equal(t, buf.read.off, 1)
	Equal(t, buf.read.Len(), 3)
	Equal(t, buf.flush.malloc, 4)
}

func TestLinkBufferRefer(t *testing.T) {
	// clean & new
	LinkBufferCap = 8

	wbuf := NewLinkBuffer()
	wbuf.book(block8k, block8k)
	wbuf.Malloc(7)
	wbuf.Flush()
	Equal(t, wbuf.Len(), block8k+7)

	buf := NewLinkBuffer()
	var p []byte

	// writev
	buf.WriteBuffer(wbuf)
	buf.Flush()
	Equal(t, buf.Len(), block8k+7)

	p, _ = buf.Next(5)
	Equal(t, len(p), 5)
	MustTrue(t, buf.read != buf.flush)
	Equal(t, buf.read.off, 5)
	Equal(t, buf.read.Len(), block8k-5)
	Equal(t, buf.flush.off, 0)
	Equal(t, buf.flush.malloc, 7)
	Equal(t, cap(buf.flush.buf), 8)

	// readv
	_rbuf, err := buf.Slice(4)
	rbuf, ok := _rbuf.(*LinkBuffer)
	MustNil(t, err)
	MustTrue(t, ok)
	Equal(t, rbuf.Len(), 4)
	MustTrue(t, rbuf.read != rbuf.flush)
	Equal(t, rbuf.read.off, 0)
	Equal(t, rbuf.read.Len(), 4)

	MustTrue(t, buf.head != buf.read) // Slice will Release
	MustTrue(t, rbuf.read != buf.read)
	Equal(t, buf.Len(), block8k-2)
	MustTrue(t, buf.read != buf.flush)
	Equal(t, buf.read.off, 9)
	Equal(t, buf.read.malloc, block8k)

	// release
	node1 := rbuf.head
	node2 := buf.head
	rbuf.Skip(rbuf.Len())
	err = rbuf.Release()
	MustNil(t, err)
	MustTrue(t, rbuf.head != node1)
	MustTrue(t, buf.head == node2)

	err = buf.Release()
	MustNil(t, err)
	MustTrue(t, buf.head != node2)
	MustTrue(t, buf.head == buf.read)
	Equal(t, buf.read.off, 9)
	Equal(t, buf.read.malloc, block8k)
	Equal(t, buf.read.refer, int32(1))
	Equal(t, buf.read.Len(), block8k-9)
}

func TestLinkBufferResetTail(t *testing.T) {
	except := byte(1)

	LinkBufferCap = 8
	buf := NewLinkBuffer()

	// 1. slice reader
	buf.WriteByte(except)
	buf.Flush()
	r1, _ := buf.Slice(1)
	fmt.Printf("1: %x\n", buf.flush.buf)
	// 2. release & reset tail
	buf.resetTail(LinkBufferCap)
	buf.WriteByte(byte(2))
	fmt.Printf("2: %x\n", buf.flush.buf)

	// check slice reader
	got, _ := r1.ReadByte()
	Equal(t, got, except)
}

func TestWriteBuffer(t *testing.T) {
	buf1 := NewLinkBuffer()
	buf2 := NewLinkBuffer()
	b2, _ := buf2.Malloc(1)
	b2[0] = 2
	buf2.Flush()
	buf3 := NewLinkBuffer()
	b3, _ := buf3.Malloc(1)
	b3[0] = 3
	buf3.Flush()
	buf1.WriteBuffer(buf2)
	buf1.WriteBuffer(buf3)
	buf1.Flush()
	MustTrue(t, bytes.Equal(buf1.Bytes(), []byte{2, 3}))
}

func TestLinkBufferCheckSingleNode(t *testing.T) {
	buf := NewLinkBuffer(block4k)
	_, err := buf.Malloc(block8k)
	MustNil(t, err)
	buf.Flush()
	MustTrue(t, buf.read.Len() == 0)
	is := buf.isSingleNode(block8k)
	MustTrue(t, is)
	MustTrue(t, buf.read.Len() == block8k)
	is = buf.isSingleNode(block8k + 1)
	MustTrue(t, !is)

	// cross node malloc, but b.read.Len() still == 0
	buf = NewLinkBuffer(block4k)
	_, err = buf.Malloc(block8k)
	MustNil(t, err)
	// not malloc ack yet
	// read function will call isSingleNode inside
	buf.isSingleNode(1)
}

func TestWriteMultiFlush(t *testing.T) {
	buf := NewLinkBuffer()
	b1, _ := buf.Malloc(4)
	b1[0] = 1
	b1[2] = 2
	err := buf.Flush()
	MustNil(t, err)
	err = buf.Flush()
	MustNil(t, err)
	MustTrue(t, buf.Bytes()[0] == 1)
	MustTrue(t, len(buf.Bytes()) == 4)

	err = buf.Skip(2)
	MustNil(t, err)
	MustTrue(t, buf.Bytes()[0] == 2)
	MustTrue(t, len(buf.Bytes()) == 2)
	err = buf.Flush()
	MustNil(t, err)
	MustTrue(t, buf.Bytes()[0] == 2)
	MustTrue(t, len(buf.Bytes()) == 2)

	b2, _ := buf.Malloc(2)
	b2[0] = 3
	err = buf.Flush()
	MustNil(t, err)
	MustTrue(t, buf.Bytes()[0] == 2)
	MustTrue(t, buf.Bytes()[2] == 3)
	MustTrue(t, len(buf.Bytes()) == 4)
}

func TestWriteBinary(t *testing.T) {
	// clean & new
	LinkBufferCap = 8

	// new b: cap=16, len=9
	var b = make([]byte, 16)
	var buf = NewLinkBuffer()
	buf.WriteBinary(b[:9])
	buf.Flush()

	// Currently, b[9:] should no longer be held.
	// WriteBinary/Malloc etc. cannot start from b[9:]
	buf.WriteBinary([]byte{1})
	Equal(t, b[9], byte(0))
	var bs, err = buf.Malloc(1)
	MustNil(t, err)
	bs[0] = 2
	buf.Flush()
	Equal(t, b[9], byte(0))
}

func TestWriteDirect(t *testing.T) {
	// clean & new
	LinkBufferCap = 32

	var buf = NewLinkBuffer()
	bt, _ := buf.Malloc(32)
	bt[0] = 'a'
	bt[1] = 'b'
	buf.WriteDirect([]byte("cdef"), 30)
	bt[2] = 'g'
	buf.WriteDirect([]byte("hijkl"), 29)
	bt[3] = 'm'
	buf.WriteDirect([]byte("nopqrst"), 28)
	bt[4] = 'u'
	buf.WriteDirect([]byte("vwxyz"), 27)
	buf.Flush()
	bs := buf.Bytes()
	str := "abcdefghijklmnopqrstuvwxyz"
	for i := 0; i < len(str); i++ {
		if bs[i] != str[i] {
			t.Error("not equal!")
		}
	}
}

func BenchmarkLinkBufferConcurrentReadWrite(b *testing.B) {
	b.StopTimer()

	buf := NewLinkBuffer()
	var rwTag uint32
	readMsg := []string{
		"0123456",
		"7890123",
		"4567890",
		"1234567",
		"8901234",
		"5678901",
		"2345678",
		"9012345",
		"6789012",
		"3456789",
	}
	writeMsg := []byte("0123456789")

	// benchmark
	b.ReportAllocs()
	b.StartTimer()
	b.SetParallelism(2) // one read one write
	b.RunParallel(func(pb *testing.PB) {
		switch atomic.AddUint32(&rwTag, 1) {
		case 1:
			// 1 is write
			for pb.Next() {
				p, err := buf.Malloc(80)
				if err != nil {
					panic(fmt.Sprintf("malloc error %s", err.Error()))
				}
				for i := 0; i < 7; i++ {
					copy(p[i*10:i*10+10], writeMsg)
				}
				buf.MallocAck(70)
				buf.Flush()
			}
		case 2:
			// 2 is read
			for pb.Next() {
				for i := 0; i < 10; {
					p, err := buf.Next(7)
					if err == nil {
						if string(p) != readMsg[i] {
							panic(fmt.Sprintf("NEXT p[%s] != msg[%s]", p, readMsg[i]))
						}
					} else {
						// No read data, wait for write
						continue
					}
					i++
				}
				buf.Release()
			}
		}

	})
}

func TestUnsafeStringToSlice(t *testing.T) {
	s := "hello world"
	bs := unsafeStringToSlice(s)
	s = "hi, boy"
	_ = s
	Equal(t, string(bs), "hello world")
}

func TestLinkBufferIndexByte(t *testing.T) {
	// clean & new
	LinkBufferCap = 128
	loopSize := 1000
	trigger := make(chan struct{}, 16)

	lb := NewLinkBuffer()
	go func() {
		for i := 0; i < loopSize; i++ {
			buf, err := lb.Malloc(1002)
			buf[500] = '\n'
			buf[1001] = '\n'
			MustNil(t, err)
			lb.Flush()
			trigger <- struct{}{}
		}
	}()

	for i := 0; i < loopSize; i++ {
		<-trigger
		last := i * 1002
		n := lb.indexByte('\n', 0+last)
		Equal(t, n, 500+last)
		n = lb.indexByte('\n', 500+last)
		Equal(t, n, 500+last)
		n = lb.indexByte('\n', 501+last)
		Equal(t, n, 1001+last)
	}
}

func BenchmarkStringToSliceByte(b *testing.B) {
	b.StopTimer()
	s := "hello world"
	var bs []byte
	if false {
		b.Logf("bs = %s", bs)
	}

	// benchmark
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs = unsafeStringToSlice(s)
	}
	_ = bs
}

func BenchmarkStringToCopy(b *testing.B) {
	b.StopTimer()
	s := "hello world"
	var bs []byte
	b.Logf("bs = %s", bs)

	// benchmark
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		bs = []byte(s)
	}
	_ = bs
}

func BenchmarkPoolGet(b *testing.B) {
	var v *linkBufferNode
	if false {
		b.Logf("bs = %v", v)
	}

	// benchmark
	b.ReportAllocs()
	b.SetParallelism(100)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			v = newLinkBufferNode(0)
			v.Release()
		}
	})
}

func BenchmarkCopyString(b *testing.B) {
	var s = make([]byte, 128*1024)

	// benchmark
	b.ReportAllocs()
	b.SetParallelism(100)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var v = make([]byte, 1024)
		for pb.Next() {
			copy(v, s)
		}
	})
}
