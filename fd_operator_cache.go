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

package netpoll

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

func newOperatorCache() *operatorCache {
	return &operatorCache{
		cache:    make([]*FDOperator, 0, 1024),
		freelist: make([]int32, 0, 1024),
	}
}

type operatorCache struct {
	locked int32
	first  *FDOperator
	cache  []*FDOperator
	// freelist store the freeable operator
	// to reduce GC pressure, we only store op index here
	freelist   []int32
	freelocked int32
}

func (c *operatorCache) alloc() *FDOperator {
	lock(&c.locked)
	if c.first == nil {
		const opSize = unsafe.Sizeof(FDOperator{})
		n := block4k / opSize
		if n == 0 {
			n = 1
		}
		index := int32(len(c.cache))
		for i := uintptr(0); i < n; i++ {
			pd := &FDOperator{index: index}
			c.cache = append(c.cache, pd)
			pd.next = c.first
			c.first = pd
			index++
		}
	}
	op := c.first
	c.first = op.next
	unlock(&c.locked)
	return op
}

// freeable mark the operator that could be freed
// only poller could do the real free action
func (c *operatorCache) freeable(op *FDOperator) {
	// reset all state
	op.unused()
	op.reset()
	lock(&c.freelocked)
	c.freelist = append(c.freelist, op.index)
	unlock(&c.freelocked)
}

func (c *operatorCache) free() {
	lock(&c.freelocked)
	defer unlock(&c.freelocked)
	if len(c.freelist) == 0 {
		return
	}

	lock(&c.locked)
	for _, idx := range c.freelist {
		op := c.cache[idx]
		op.next = c.first
		c.first = op
	}
	c.freelist = c.freelist[:0]
	unlock(&c.locked)
}

func lock(locked *int32) {
	for !atomic.CompareAndSwapInt32(locked, 0, 1) {
		runtime.Gosched()
	}
}

func unlock(locked *int32) {
	atomic.StoreInt32(locked, 0)
}
