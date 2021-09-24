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

package mux

import (
	"runtime"
	"sync/atomic"

	"github.com/cloudwego/netpoll"
)

/* DOC:
 * SharedQueue uses the netpoll's nocopy API to merge and send data.
 * The Data Flush is passively triggered by SharedQueue.Add and does not require user operations.
 * If there is an error in the data transmission, the connection will be closed.
 *
 * SharedQueue.Add: add the data to be sent.
 * NewSharedQueue: create a queue with netpoll.Connection.
 * SharedSize: the recommended number of shards is 32.
 */
const SharedSize = 32

// NewSharedQueue .
func NewSharedQueue(size int32, conn netpoll.Connection) (queue *SharedQueue) {
	queue = &SharedQueue{
		conn:    conn,
		size:    size,
		getters: make([][]WriterGetter, size),
		swap:    make([]WriterGetter, 0, 64),
		locks:   make([]int32, size),
	}
	for i := range queue.getters {
		queue.getters[i] = make([]WriterGetter, 0, 64)
	}
	return queue
}

// WriterGetter is used to get a netpoll.Writer.
type WriterGetter func() (buf netpoll.Writer, isNil bool)

// SharedQueue uses the netpoll's nocopy API to merge and send data.
// The Data Flush is passively triggered by SharedQueue.Add and does not require user operations.
// If there is an error in the data transmission, the connection will be closed.
// SharedQueue.Add: add the data to be sent.
type SharedQueue struct {
	conn            netpoll.Connection
	idx, size       int32
	getters         [][]WriterGetter // len(getters) = size
	swap            []WriterGetter   // use for swap
	locks           []int32          // len(locks) = size
	trigger, runNum int32
}

// Add adds to q.getters[shared]
func (q *SharedQueue) Add(gts ...WriterGetter) {
	shared := atomic.AddInt32(&q.idx, 1) % q.size
	q.lock(shared)
	trigger := len(q.getters[shared]) == 0
	q.getters[shared] = append(q.getters[shared], gts...)
	q.unlock(shared)
	if trigger {
		q.triggering(shared)
	}
}

// triggering shared.
func (q *SharedQueue) triggering(shared int32) {
	if atomic.AddInt32(&q.trigger, 1) > 1 {
		return
	}
	q.foreach(shared)
}

// foreach swap r & w. It's not concurrency safe.
func (q *SharedQueue) foreach(shared int32) {
	if atomic.AddInt32(&q.runNum, 1) > 1 {
		return
	}
	go func() {
		var tmp []WriterGetter
		for ; atomic.LoadInt32(&q.trigger) > 0; shared = (shared + 1) % q.size {
			// lock & swap
			q.lock(shared)
			if len(q.getters[shared]) == 0 {
				q.unlock(shared)
				continue
			}
			// swap
			tmp = q.getters[shared]
			q.getters[shared] = q.swap[:0]
			q.swap = tmp
			q.unlock(shared)
			atomic.AddInt32(&q.trigger, -1)

			// deal
			q.deal(q.swap)
		}
		q.flush()

		// quit & check again
		atomic.StoreInt32(&q.runNum, 0)
		if atomic.LoadInt32(&q.trigger) > 0 {
			q.foreach(shared)
		}
	}()
}

// deal is used to get deal of netpoll.Writer.
func (q *SharedQueue) deal(gts []WriterGetter) {
	writer := q.conn.Writer()
	for _, gt := range gts {
		buf, isNil := gt()
		if !isNil {
			_, err := writer.Append(buf)
			if err != nil {
				q.conn.Close()
				return
			}
		}
	}
}

// flush is used to flush netpoll.Writer.
func (q *SharedQueue) flush() {
	err := q.conn.Writer().Flush()
	if err != nil {
		q.conn.Close()
		return
	}
}

// lock shared.
func (q *SharedQueue) lock(shared int32) {
	for !atomic.CompareAndSwapInt32(&q.locks[shared], 0, 1) {
		runtime.Gosched()
	}
}

// unlock shared.
func (q *SharedQueue) unlock(shared int32) {
	atomic.StoreInt32(&q.locks[shared], 0)
}
