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
	"math"
	"syscall"
	"testing"
)

func TestIovecs(t *testing.T) {
	var got int
	var bs [][]byte
	var ivs = make([]syscall.Iovec, 4)

	// case 1
	bs = [][]byte{
		make([]byte, 10),
		make([]byte, 20),
		make([]byte, 30),
		make([]byte, 40),
	}
	got = iovecs(bs, ivs)
	Equal(t, got, 4)
	Equal(t, int(ivs[0].Len), 10)
	Equal(t, int(ivs[1].Len), 20)
	Equal(t, int(ivs[2].Len), 30)
	Equal(t, int(ivs[3].Len), 40)

	// case 2
	resetIovecs(bs, ivs)
	bs = [][]byte{
		make([]byte, math.MaxInt32+100),
		make([]byte, 20),
		make([]byte, 30),
		make([]byte, 40),
	}
	got = iovecs(bs, ivs)
	Equal(t, got, 1)
	Equal(t, int(ivs[0].Len), math.MaxInt32)
	Assert(t, ivs[1].Base == nil)
	Assert(t, ivs[2].Base == nil)
	Assert(t, ivs[3].Base == nil)

	// case 3
	resetIovecs(bs, ivs)
	bs = [][]byte{
		make([]byte, 10),
		make([]byte, 20),
		make([]byte, math.MaxInt32+100),
		make([]byte, 40),
	}
	got = iovecs(bs, ivs)
	Equal(t, got, 3)
	Equal(t, int(ivs[0].Len), 10)
	Equal(t, int(ivs[1].Len), 20)
	Equal(t, int(ivs[2].Len), math.MaxInt32-30)
	Assert(t, ivs[3].Base == nil)
}

func TestWritev(t *testing.T) {
	r, w := GetSysFdPairs()
	var barrier = barrier{}
	barrier.bs = [][]byte{
		[]byte(""),            // len=0
		[]byte("first line"),  // len=10
		[]byte("second line"), // len=11
		[]byte("third line"),  // len=10
	}
	barrier.ivs = make([]syscall.Iovec, len(barrier.bs))
	wn, err := writev(w, barrier.bs, barrier.ivs)
	MustNil(t, err)
	Equal(t, wn, 31)
	var p = make([]byte, 50)
	rn, err := syscall.Read(r, p)
	MustNil(t, err)
	Equal(t, rn, 31)
	t.Logf("READ %s", p[:rn])
}

func TestReadv(t *testing.T) {
	r, w := GetSysFdPairs()
	vs := [][]byte{
		[]byte("first line"),  // len=10
		[]byte("second line"), // len=11
		[]byte("third line"),  // len=10
	}
	w1, _ := syscall.Write(w, vs[0])
	w2, _ := syscall.Write(w, vs[1])
	w3, _ := syscall.Write(w, vs[2])
	Equal(t, w1+w2+w3, 31)

	var barrier = barrier{}
	barrier.bs = [][]byte{
		make([]byte, 0),
		make([]byte, 10),
		make([]byte, 11),
		make([]byte, 10),
	}
	barrier.ivs = make([]syscall.Iovec, len(barrier.bs))
	rn, err := readv(r, barrier.bs, barrier.ivs)
	MustNil(t, err)
	Equal(t, rn, 31)
	for i, v := range barrier.bs {
		t.Logf("READ [%d] %s", i, v)
	}
}

func TestSendmsg(t *testing.T) {
	r, w := GetSysFdPairs()
	var barrier = barrier{}
	barrier.bs = [][]byte{
		[]byte(""),            // len=0
		[]byte("first line"),  // len=10
		[]byte("second line"), // len=11
		[]byte("third line"),  // len=10
	}
	barrier.ivs = make([]syscall.Iovec, len(barrier.bs))
	wn, err := sendmsg(w, barrier.bs, barrier.ivs, false)
	MustNil(t, err)
	Equal(t, wn, 31)
	var p = make([]byte, 50)
	rn, err := syscall.Read(r, p)
	MustNil(t, err)
	Equal(t, rn, 31)
	t.Logf("READ %s", p[:rn])
}

func BenchmarkWrite(b *testing.B) {
	b.StopTimer()
	r, w := GetSysFdPairs()
	message := "hello, world!"
	size := 5

	go func() {
		buffer := make([]byte, 13)
		for {
			syscall.Read(r, buffer)
		}

	}()

	// benchmark
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		var wmsg = make([]byte, len(message)*5)
		var n int
		for j := 0; j < size; j++ {
			n += copy(wmsg[n:], message)
		}
		syscall.Write(w, wmsg)
	}
}

func BenchmarkWritev(b *testing.B) {
	b.StopTimer()
	r, w := GetSysFdPairs()
	message := "hello, world!"
	size := 5
	var barrier = barrier{}
	barrier.bs = make([][]byte, size)
	barrier.ivs = make([]syscall.Iovec, len(barrier.bs))
	for i := range barrier.bs {
		barrier.bs[i] = make([]byte, len(message))
	}

	go func() {
		buffer := make([]byte, 13)
		for {
			syscall.Read(r, buffer)
		}

	}()

	// benchmark
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		writev(w, barrier.bs, barrier.ivs)
	}
}

func BenchmarkSendmsg(b *testing.B) {
	b.StopTimer()
	r, w := GetSysFdPairs()
	message := "hello, world!"
	size := 5
	var barrier = barrier{}
	barrier.bs = make([][]byte, size)
	barrier.ivs = make([]syscall.Iovec, len(barrier.bs))
	for i := range barrier.bs {
		barrier.bs[i] = make([]byte, len(message))
	}

	go func() {
		buffer := make([]byte, 13)
		for {
			syscall.Read(r, buffer)
		}

	}()

	// benchmark
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		sendmsg(w, barrier.bs, barrier.ivs, false)
	}
}

func BenchmarkRead(b *testing.B) {
	b.StopTimer()
	r, w := GetSysFdPairs()
	message := "hello, world!"
	size := 5
	wmsg := make([]byte, size*len(message))
	var n int
	for j := 0; j < size; j++ {
		n += copy(wmsg[n:], message)
	}

	go func() {
		for {
			syscall.Write(w, wmsg)
		}

	}()

	// benchmark
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		var buffer = make([]byte, size*len(message))
		syscall.Read(r, buffer)
	}
}

func BenchmarkReadv(b *testing.B) {
	b.StopTimer()
	r, w := GetSysFdPairs()
	message := "hello, world!"
	size := 5
	var barrier = barrier{}
	barrier.bs = make([][]byte, size)
	barrier.ivs = make([]syscall.Iovec, len(barrier.bs))
	for i := range barrier.bs {
		barrier.bs[i] = make([]byte, len(message))
	}

	go func() {
		for {
			writeAll(w, []byte(message))
		}

	}()

	// benchmark
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		readv(r, barrier.bs, barrier.ivs)
	}
}
