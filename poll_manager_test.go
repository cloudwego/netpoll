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
	"sync"
	"testing"
)

func TestPollManager(t *testing.T) {
	r, w := GetSysFdPairs()
	var rconn, wconn = &connection{}, &connection{}
	err := rconn.init(&netFD{fd: r}, nil)
	MustNil(t, err)
	err = wconn.init(&netFD{fd: w}, nil)
	MustNil(t, err)

	var msg = []byte("hello world")
	n, err := wconn.Write(msg)
	MustNil(t, err)
	Equal(t, n, len(msg))

	p, err := rconn.Reader().Next(n)
	MustNil(t, err)
	Equal(t, string(p), string(msg))

	err = wconn.Close()
	MustNil(t, err)
	for rconn.IsActive() || wconn.IsActive() {
		runtime.Gosched()
	}
}

func TestPollManagerReset(t *testing.T) {
	n := pollmanager.numLoops
	err := pollmanager.Reset()
	MustNil(t, err)
	Equal(t, len(pollmanager.polls), int(n))
}

func TestPollManagerSetNumLoops(t *testing.T) {
	pm := newManager(1)

	startGs := runtime.NumGoroutine()
	poll := pm.Pick()
	newGs := runtime.NumGoroutine()
	Assert(t, poll != nil)
	Assert(t, newGs-startGs == 1, newGs, startGs)
	t.Logf("old=%d, new=%d", startGs, newGs)

	// change pollers
	oldGs := newGs
	err := pm.SetNumLoops(100)
	MustNil(t, err)
	newGs = runtime.NumGoroutine()
	t.Logf("old=%d, new=%d", oldGs, newGs)
	Assert(t, newGs == oldGs)

	// trigger polls adjustment
	var wg sync.WaitGroup
	finish := make(chan struct{})
	oldGs = startGs + 32 // 32 self goroutines
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			poll := pm.Pick()
			Assert(t, poll != nil)
			Assert(t, len(pm.polls) == 100)
			wg.Done()
			<-finish // hold goroutines
		}()
	}
	wg.Wait()
	close(finish)
}
