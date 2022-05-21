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
	"syscall"
	"testing"
	"unsafe"
)

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

	// benchmark
	buffer := make([]byte, 128)
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		syscall.RawSyscall(syscall.SYS_WRITE, uintptr(w), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		syscall.RawSyscall(syscall.SYS_READ, uintptr(r), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	}
}

func BenchmarkWritev(b *testing.B) {
	b.StopTimer()
	r, w := GetSysFdPairs()
	buffer := make([]byte, 128)

	// benchmark
	var br = barrier{}
	br.bs = make([][]byte, 2)
	br.bs[0] = make([]byte, 128)
	br.bs[1] = make([]byte, 0, 128)
	br.ivs = make([]syscall.Iovec, len(br.bs))
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		writev(w, br.bs, br.ivs)
		syscall.RawSyscall(syscall.SYS_READ, uintptr(r), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	}
}

func BenchmarkSendmsg(b *testing.B) {
	b.StopTimer()
	r, w := GetSysFdPairs()
	buffer := make([]byte, 128)

	// benchmark
	var br = barrier{}
	br.bs = make([][]byte, 2)
	br.bs[0] = make([]byte, 128)
	br.bs[1] = make([]byte, 0, 128)
	br.ivs = make([]syscall.Iovec, len(br.bs))
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		sendmsg(w, br.bs, br.ivs, false)
		syscall.RawSyscall(syscall.SYS_READ, uintptr(r), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	}
}

func BenchmarkRead(b *testing.B) {
	b.StopTimer()
	r, w := GetSysFdPairs()
	go func() {
		wmsg := make([]byte, 128*1024)
		for {
			syscall.Write(w, wmsg)
		}
	}()

	// benchmark
	buffer := make([]byte, 128)
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		syscall.RawSyscall(syscall.SYS_READ, uintptr(r), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	}
}

func BenchmarkReadv(b *testing.B) {
	b.StopTimer()
	r, w := GetSysFdPairs()
	go func() {
		wmsg := make([]byte, 128*1024)
		for {
			syscall.Write(w, wmsg)
		}
	}()

	// benchmark
	var br = barrier{}
	br.bs = make([][]byte, 2)
	br.bs[0] = make([]byte, 128)
	br.bs[1] = make([]byte, 0, 128)
	br.ivs = make([]syscall.Iovec, len(br.bs))
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		readv(r, br.bs, br.ivs)
	}
}
