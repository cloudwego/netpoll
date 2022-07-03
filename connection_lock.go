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
	"github.com/bytedance/gopkg/lang/fastrand"
	"golang.org/x/sys/cpu"
	"runtime"
	"sync/atomic"
	"unsafe"
)

type who int32

const (
	none who = iota
	user
	poller
)

type key int32

/* State Diagram
+--------------+         +--------------+
|  processing  |-------->|   flushing   |
+-------+------+         +-------+------+
        |
        |                +--------------+
        +--------------->|   closing    |
                         +--------------+

- "processing" locks onRequest handler, and doesn't exist in dialer.
- "flushing" locks outputBuffer
- "closing" should wait for flushing finished and call the closeCallback after that.
*/

const (
	closing key = iota
	processing
	flushing
	// total must be at the bottom.
	total
)

const (
	cacheLineSize = unsafe.Sizeof(cpu.CacheLinePad{})
)

type padKey struct {
	key int32
	_   [cacheLineSize - unsafe.Sizeof(int32(0))]byte
}

type locker struct {
	// keychain use for lock/unlock/stop operation by who.
	// 0 means unlock, 1 means locked, 2 means stop.
	keychain [total]padKey
}

func (l *locker) closeBy(w who) (success bool) {
	return atomic.CompareAndSwapInt32(&l.keychain[closing].key, 0, int32(w))
}

func (l *locker) isCloseBy(w who) (yes bool) {
	return atomic.LoadInt32(&l.keychain[closing].key) == int32(w)
}

func (l *locker) lock(k key) (success bool) {
	return atomic.CompareAndSwapInt32(&l.keychain[k].key, 0, 1)
}

func (l *locker) unlock(k key) {
	atomic.StoreInt32(&l.keychain[k].key, 0)
}

func (l *locker) stop(k key) {
	for !atomic.CompareAndSwapInt32(&l.keychain[k].key, 0, 2) && atomic.LoadInt32(&l.keychain[k].key) != 2 {
		backoff := int(fastrand.Int31n(maxBackOff)) + 1
		for i := 0; i < backoff; i++ {
			runtime.Gosched()
		}
	}
}

func (l *locker) isUnlock(k key) bool {
	return atomic.LoadInt32(&l.keychain[k].key) == 0
}
