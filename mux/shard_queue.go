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

package mux

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/bytedance/gopkg/util/gopool"

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
var ShardSize int

func init() {
	ShardSize = runtime.GOMAXPROCS(0)
}

// NewShardQueue .
func NewShardQueue(size int, conn netpoll.Connection) (queue *ShardQueue) {
	queue = &ShardQueue{
		conn:       conn,
		size:       int32(size),
		getters:    make([][]WriterGetter, size),
		swap:       make([]WriterGetter, 0, 64),
		locks:      make([]int32, size),
		closeNotif: make(chan struct{}),
	}
	for i := range queue.getters {
		queue.getters[i] = make([]WriterGetter, 0, 64)
	}
	// To avoid w equals to r when loop writing, make list larger than size.
	queue.list = make([]int32, size+1)
	return queue
}

// WriterGetter is used to get a netpoll.Writer.
type WriterGetter func() (buf netpoll.Writer, isNil bool)

// ShardQueue uses the netpoll's nocopy API to merge and send data.
// The Data Flush is passively triggered by ShardQueue.Add and does not require user operations.
// If there is an error in the data transmission, the connection will be closed.
// ShardQueue.Add: add the data to be sent.
type ShardQueue struct {
	conn      netpoll.Connection
	idx, size int32
	getters   [][]WriterGetter // len(getters) = size
	swap      []WriterGetter   // use for swap
	locks     []int32          // len(locks) = size

	closeNotif chan struct{}
	queueTrigger
}

const (
	// queueTrigger state
	active  = 0
	closing = 1
	closed  = 2
)

// here for trigger
type queueTrigger struct {
	bufNum int32
	state  int32 // 0: active, 1: closing, 2: closed
	runNum int32
	w, r   int32   // ptr of list
	list   []int32 // record the triggered shard
}

func (q *queueTrigger) length() int {
	w := int(atomic.LoadInt32(&q.w))
	r := int(atomic.LoadInt32(&q.r))
	if w < r {
		w += len(q.list)
	}
	return w - r
}

// Add adds to q.getters[shard]
func (q *ShardQueue) Add(gts ...WriterGetter) {
	atomic.AddInt32(&q.bufNum, 1)
	if atomic.LoadInt32(&q.state) != active {
		if atomic.AddInt32(&q.bufNum, -1) <= 0 {
			close(q.closeNotif)
		}
		return
	}
	shard := atomic.AddInt32(&q.idx, 1) % q.size
	q.lock(shard)
	trigger := len(q.getters[shard]) == 0
	q.getters[shard] = append(q.getters[shard], gts...)
	q.unlock(shard)
	if trigger {
		q.triggering(shard)
	}
}

func (q *ShardQueue) Close() error {
	if !atomic.CompareAndSwapInt32(&q.state, active, closing) {
		return fmt.Errorf("shardQueue has been closed")
	}
	// wait for all tasks finished
	if atomic.LoadInt32(&q.bufNum) == 0 {
		atomic.StoreInt32(&q.state, closed)
	} else {
		timeout := time.NewTimer(3 * time.Second)
		select {
		case <-q.closeNotif:
			atomic.StoreInt32(&q.state, closed)
			timeout.Stop()
		case <-timeout.C:
			atomic.StoreInt32(&q.state, closed)
			return fmt.Errorf("shardQueue close timeout")
		}
	}
	return nil
}

// triggering shard.
func (q *ShardQueue) triggering(shard int32) {
	for {
		ow := atomic.LoadInt32(&q.w)
		nw := (ow + 1) % int32(len(q.list))
		if atomic.CompareAndSwapInt32(&q.w, ow, nw) {
			q.list[nw] = shard
			break
		}
	}
	q.foreach()
}

// foreach swap r & w.
func (q *ShardQueue) foreach() {
	if atomic.AddInt32(&q.runNum, 1) > 1 {
		return
	}
	gopool.CtxGo(nil, func() {
		var negBufNum int32 // is negative number of bufNum
		for q.length() > 0 {
			nr := (atomic.LoadInt32(&q.r) + 1) % int32(len(q.list))
			atomic.StoreInt32(&q.r, nr)
			shard := q.list[nr]

			// lock & swap
			q.lock(shard)
			tmp := q.getters[shard]
			q.getters[shard] = q.swap[:0]
			q.swap = tmp
			q.unlock(shard)

			// deal
			if err := q.deal(q.swap); err != nil {
				close(q.closeNotif)
				return
			}
			negBufNum -= int32(len(q.swap))
		}
		if negBufNum < 0 {
			if err := q.flush(); err != nil {
				close(q.closeNotif)
				return
			}
		}

		// MUST decrease bufNum first.
		if atomic.AddInt32(&q.bufNum, negBufNum) <= 0 && atomic.LoadInt32(&q.state) != active {
			close(q.closeNotif)
			return
		}

		// quit & check again
		atomic.StoreInt32(&q.runNum, 0)
		if q.length() > 0 {
			q.foreach()
			return
		}
	})
}

// deal is used to get deal of netpoll.Writer.
func (q *ShardQueue) deal(gts []WriterGetter) error {
	writer := q.conn.Writer()
	for _, gt := range gts {
		buf, isNil := gt()
		if !isNil {
			err := writer.Append(buf)
			if err != nil {
				q.conn.Close()
				return err
			}
		}
	}
	return nil
}

// flush is used to flush netpoll.Writer.
func (q *ShardQueue) flush() error {
	err := q.conn.Writer().Flush()
	if err != nil {
		q.conn.Close()
	}
	return err
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
