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
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Trigger has been validated, but no usage for now.
func TestPollTrigger(t *testing.T) {
	t.Skip()
	var trigger int
	var stop = make(chan error)
	var p = openDefaultPoll()
	go func() {
		stop <- p.Wait()
	}()

	time.Sleep(time.Millisecond)
	Equal(t, trigger, 0)
	p.Trigger()
	time.Sleep(time.Millisecond)
	Equal(t, trigger, 1)
	p.Trigger()
	time.Sleep(time.Millisecond)
	Equal(t, trigger, 2)

	p.Close()
	err := <-stop
	MustNil(t, err)
}

func TestPollMod(t *testing.T) {
	var rn, wn, hn int32
	var read = func(p Poll) error {
		atomic.AddInt32(&rn, 1)
		return nil
	}
	var write = func(p Poll) error {
		atomic.AddInt32(&wn, 1)
		return nil
	}
	var hup = func(p Poll) error {
		atomic.AddInt32(&hn, 1)
		return nil
	}
	var stop = make(chan error)
	var p = openDefaultPoll()
	go func() {
		stop <- p.Wait()
	}()

	var rfd, wfd = GetSysFdPairs()
	var rop = &FDOperator{FD: rfd, OnRead: read, OnWrite: write, OnHup: hup}
	var wop = &FDOperator{FD: wfd, OnRead: read, OnWrite: write, OnHup: hup}
	var err error
	var r, w, h int32
	r, w, h = atomic.LoadInt32(&rn), atomic.LoadInt32(&wn), atomic.LoadInt32(&hn)
	Assert(t, r == 0 && w == 0 && h == 0, r, w, h)
	err = p.Control(rop, PollReadable)
	MustNil(t, err)
	r, w, h = atomic.LoadInt32(&rn), atomic.LoadInt32(&wn), atomic.LoadInt32(&hn)
	Assert(t, r == 0 && w == 0 && h == 0, r, w, h)

	err = p.Control(wop, PollWritable) // trigger one shot
	MustNil(t, err)
	time.Sleep(50 * time.Millisecond)
	r, w, h = atomic.LoadInt32(&rn), atomic.LoadInt32(&wn), atomic.LoadInt32(&hn)
	Assert(t, r == 0 && w == 1 && h == 0, r, w, h)

	err = p.Control(rop, PollR2W) // trigger write
	MustNil(t, err)
	time.Sleep(time.Millisecond)
	r, w, h = atomic.LoadInt32(&rn), atomic.LoadInt32(&wn), atomic.LoadInt32(&hn)
	Assert(t, r == 0 && w >= 2 && h == 0, r, w, h)

	// close wfd, then trigger hup rfd
	err = syscall.Close(wfd) // trigger hup
	MustNil(t, err)
	time.Sleep(time.Millisecond)
	r, w, h = atomic.LoadInt32(&rn), atomic.LoadInt32(&wn), atomic.LoadInt32(&hn)
	Assert(t, r == 0 && w >= 2 && h >= 1, r, w, h)

	p.Close()
	err = <-stop
	MustNil(t, err)
}

func TestPollClose(t *testing.T) {
	var p = openDefaultPoll()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		p.Wait()
		wg.Done()
	}()
	p.Close()
	wg.Wait()
}

func BenchmarkPollMod(b *testing.B) {
	b.StopTimer()
	var p = openDefaultPoll()
	r, _ := GetSysFdPairs()
	var operator = &FDOperator{FD: r}
	p.Control(operator, PollReadable)

	// benchmark
	b.ReportAllocs()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		p.Control(operator, PollR2W)
	}
}
