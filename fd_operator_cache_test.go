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
	"runtime"
	"testing"
)

// go test -v -gcflags=-d=checkptr -run=TestPersistFDOperator
func TestPersistFDOperator(t *testing.T) {
	opcache := newOperatorCache()
	// init
	size := 2048
	var ops = make([]*FDOperator, size)
	for i := 0; i < size; i++ {
		op := opcache.alloc()
		op.FD = i
		ops[i] = op
	}
	Equal(t, len(opcache.freelist), 0)
	// gc
	for i := 0; i < 4; i++ {
		runtime.GC()
	}
	// check alloc
	for i := range ops {
		Equal(t, ops[i].FD, i)
		opcache.freeable(ops[i])
		Equal(t, len(opcache.freelist), i+1)
	}
	Equal(t, len(opcache.freelist), size)
	opcache.free()
	Equal(t, len(opcache.freelist), 0)
	Assert(t, len(opcache.cache) >= size)
}

func BenchmarkPersistFDOperator1(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	opcache := newOperatorCache()
	for i := 0; i < b.N; i++ {
		op := opcache.alloc()
		opcache.freeable(op)
		opcache.free()
	}
}

func BenchmarkPersistFDOperator2(b *testing.B) {
	// benchmark
	b.ReportAllocs()
	b.SetParallelism(128)
	b.ResetTimer()
	opcache := newOperatorCache()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			op := opcache.alloc()
			opcache.freeable(op)
			opcache.free()
		}
	})
}
