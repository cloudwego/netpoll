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
	"testing"
	"time"
)

func TestPollManager(t *testing.T) {
	r, w := GetSysFdPairs()
	var rconn, wconn = &connection{}, &connection{}
	rconn.init(&netFD{fd: r}, nil)
	wconn.init(&netFD{fd: w}, nil)

	var msg = []byte("hello world")
	n, err := wconn.Write(msg)
	MustNil(t, err)
	Equal(t, n, len(msg))

	p, err := rconn.Reader().Next(n)
	MustNil(t, err)
	Equal(t, string(p), string(msg))

	err = wconn.Close()
	MustNil(t, err)
	time.Sleep(10 * time.Millisecond)
	MustTrue(t, !rconn.IsActive())
	MustTrue(t, !wconn.IsActive())
}

func TestPollManagerReset(t *testing.T) {
	n := pollmanager.NumLoops
	err := pollmanager.Reset()
	MustNil(t, err)
	Equal(t, len(pollmanager.polls), n)
	Equal(t, pollmanager.NumLoops, n)
}
