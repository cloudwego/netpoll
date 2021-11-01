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
 * ShardQueue uses the netpoll's nocopy API to merge and send data.
 * The Data Flush is passively triggered by ShardQueue.Add and does not require user operations.
 * If there is an error in the data transmission, the connection will be closed.
 *
 * ShardQueue.Add: add the data to be sent.
 * NewShardQueue: create a queue with netpoll.Connection.
 * ShardSize: the recommended number of shards is 32.
 */
const ShardSize = 32

// NewShardQueue .
func NewShardQueue(size int32, conn netpoll.Connection) (queue *ShardQueue) {
	queue = &ShardQueue{
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

// ShardQueue uses the netpoll's nocopy API to merge and send data.
// The Data Flush is passively triggered by ShardQueue.Add and does not require user operations.
// If there is an error in the data transmission, the connection will be closed.
// ShardQueue.Add: add the data to be sent.
type ShardQueue struct {
	conn            netpoll.Connection
	idx, size       int32
	getters         [][]WriterGetter // len(getters) = size
	swap            []WriterGetter   // use for swap
	locks           []int32          // len(locks) = size
	trigger, runNum int32
}

// Add adds to q.getters[shard]
func (q *ShardQueue) Add(gts ...WriterGetter) {
	shard := atomic.AddInt32(&q.idx, 1) % q.size
	q.lock(shard)
	trigger := len(q.getters[shard]) == 0
	q.getters[shard] = append(q.getters[shard], gts...)
	q.unlock(shard)
	if trigger {
		q.triggering(shard)
	}
}

// triggering shard.
func (q *ShardQueue) triggering(shard int32) {
	if atomic.AddInt32(&q.trigger, 1) > 1 {
		return
	}
	q.foreach(shard)
}

// foreach swap r & w. It's not concurrency safe.
func (q *ShardQueue) foreach(shard int32) {
	if atomic.AddInt32(&q.runNum, 1) > 1 {
		return
	}
	go func() {
		var tmp []WriterGetter
		for ; atomic.LoadInt32(&q.trigger) > 0; shard = (shard + 1) % q.size {
			// lock & swap
			q.lock(shard)
			if len(q.getters[shard]) == 0 {
				q.unlock(shard)
				continue
			}
			// swap
			tmp = q.getters[shard]
			q.getters[shard] = q.swap[:0]
			q.swap = tmp
			q.unlock(shard)
			atomic.AddInt32(&q.trigger, -1)

			// deal
			q.deal(q.swap)
		}
		q.flush()

		// quit & check again
		atomic.StoreInt32(&q.runNum, 0)
		if atomic.LoadInt32(&q.trigger) > 0 {
			q.foreach(shard)
		}
	}()
}

// deal is used to get deal of netpoll.Writer.
func (q *ShardQueue) deal(gts []WriterGetter) {
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
func (q *ShardQueue) flush() {
	err := q.conn.Writer().Flush()
	if err != nil {
		q.conn.Close()
		return
	}
}

// lock shard.
func (q *ShardQueue) lock(shard int32) {
	for !atomic.CompareAndSwapInt32(&q.locks[shard], 0, 1) {
		runtime.Gosched()
	}
}

// unlock shard.
func (q *ShardQueue) unlock(shard int32) {
	atomic.StoreInt32(&q.locks[shard], 0)
}
