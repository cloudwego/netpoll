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

	"github.com/cloudwego/netpoll"
)

// ShardQueue uses the netpoll nocopy Writer API to merge multi packets and send them at once.
// The Data Flush is passively triggered by ShardQueue.Add and does not require user operations.
// If there is an error in the data transmission, the connection will be closed.

// NewShardQueue create a queue with netpoll.Connection
func NewShardQueue(shardsize int, conn netpoll.Connection) (queue *ShardQueue) {
	queue = &ShardQueue{
		conn:      conn,
		shardsize: uint32(shardsize),
		shards:    make([][]WriterGetter, shardsize),
		locks:     make([]int32, shardsize),
	}
	for i := range queue.shards {
		queue.shards[i] = make([]WriterGetter, 0, 64)
	}
	queue.shard = make([]WriterGetter, 0, 64)
	return queue
}

// WriterGetter is used to get a netpoll.Writer.
type WriterGetter func() (buf netpoll.Writer, isNil bool)

// ShardQueue uses the netpoll s nocopy API to merge and send data.
// The Data Flush is passively triggered by ShardQueue.Add and does not require user operations.
// If there is an error in the data transmission, the connection will be closed.
// ShardQueue.Add: add the data to be sent.
type ShardQueue struct {
	// state definition:
	// active  : only active state can allow user Add new task
	// closing : ShardQueue.Close is called and try to close gracefully, cannot Add new data
	// closed  : Gracefully shutdown finished
	state int32

	conn      netpoll.Connection
	size      uint32           // the size of all getters in all shards
	shardsize uint32           // the size of shards
	shards    [][]WriterGetter // the shards of getters, len(shards) = shardsize
	shard     []WriterGetter   // the shard is dealing, use shard to swap
	locks     []int32          // the locks of shards, len(locks) = shardsize
	// trigger used to avoid triggering function re-enter twice.
	// trigger == 0: nothing to do
	// trigger == 1: we should start a new triggering()
	// trigger >= 2: triggering() already started
	trigger int32
}

const (
	// ShardQueue state
	active  = 0
	closing = 1
	closed  = 2
)

var idgen uint32

// Add adds gts to ShardQueue
func (q *ShardQueue) Add(gts ...WriterGetter) bool {
	size := uint32(len(gts))
	if size == 0 || atomic.LoadInt32(&q.state) != active {
		return false
	}

	// get current shard id
	shardid := atomic.AddUint32(&idgen, 1) % q.shardsize
	// add new shards into shard
	q.lock(shardid)
	q.shards[shardid] = append(q.shards[shardid], gts...)
	// size update should happen in lock, because we should make sure when q.shards unlock, worker can get the correct size
	_ = atomic.AddUint32(&q.size, size)
	q.unlock(shardid)

	if atomic.AddInt32(&q.trigger, 1) == 1 {
		go q.triggering(shardid)
	}
	return true
}

// Close graceful shutdown the ShardQueue and will flush all data added first
func (q *ShardQueue) Close() error {
	if !atomic.CompareAndSwapInt32(&q.state, active, closing) {
		return fmt.Errorf("shardQueue has been closed")
	}
	// wait for all tasks finished
	for atomic.LoadInt32(&q.state) != closed {
		if atomic.LoadInt32(&q.trigger) == 0 {
			atomic.StoreInt32(&q.state, closed)
			return nil
		}
		runtime.Gosched()
	}
	return nil
}

// triggering shard.
func (q *ShardQueue) triggering(shardid uint32) {
WORKER:
	for atomic.LoadUint32(&q.size) > 0 {
		// lock & shard
		q.lock(shardid)
		shard := q.shards[shardid]
		q.shards[shardid] = q.shard[:0]
		q.shard = shard[:0] // reuse current shard's space for next round
		q.unlock(shardid)

		if len(shard) > 0 {
			// flush shard
			q.deal(shard)
			// only decrease q.size when the shard dealt
			atomic.AddUint32(&q.size, -uint32(len(shard)))
		}
		// if there have any new data, the next shard must not be empty
		shardid = (shardid + 1) % q.shardsize
	}
	// flush connection
	q.flush()

	// [IMPORTANT] Atomic Double Check:
	// ShardQueue.Add will ensure it will always update 'size' and 'trigger'.
	// - If CAS(q.trigger, oldTrigger, 0) = true, it means there is no triggering() call during size check,
	// so it's safe to exit triggering(). And any new Add() call will start triggering() successfully.
	// - If CAS failed, there may have a failed triggering() call during Load(q.trigger) and CAS(q.trigger),
	// so we should re-check q.size again from beginning.
	oldTrigger := atomic.LoadInt32(&q.trigger)
	if atomic.LoadUint32(&q.size) > 0 {
		goto WORKER
	}
	if !atomic.CompareAndSwapInt32(&q.trigger, oldTrigger, 0) {
		goto WORKER
	}

	// if state is closing, change it to closed
	atomic.CompareAndSwapInt32(&q.state, closing, closed)
	return
}

// deal append all getters into connection
func (q *ShardQueue) deal(gts []WriterGetter) {
	writer := q.conn.Writer()
	for _, gt := range gts {
		buf, isNil := gt()
		if !isNil {
			err := writer.Append(buf)
			if err != nil { // never happen
				q.conn.Close()
				return
			}
		}
	}
}

// flush the connection and send all appended data
func (q *ShardQueue) flush() {
	err := q.conn.Writer().Flush()
	if err != nil {
		q.conn.Close()
		return
	}
}

// lock shard.
func (q *ShardQueue) lock(shard uint32) {
	for !atomic.CompareAndSwapInt32(&q.locks[shard], 0, 1) {
		runtime.Gosched()
	}
}

// unlock shard.
func (q *ShardQueue) unlock(shard uint32) {
	atomic.StoreInt32(&q.locks[shard], 0)
}
