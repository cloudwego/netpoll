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

func TestZeroTimer(t *testing.T) {
	MustTrue(t, noDeadline.IsZero())
}

func TestRuntimePoll(t *testing.T) {
	ln, err := CreateListener("tcp", ":1234")
	MustNil(t, err)

	stop := make(chan int, 1)
	defer close(stop)

	go func() {
		for {
			select {
			case <-stop:
				err := ln.Close()
				MustNil(t, err)
				return
			default:
			}
			conn, err := ln.Accept()
			if conn == nil && err == nil {
				continue
			}
		}
	}()

	for i := 0; i < 10; i++ {
		conn, err := DialConnection("tcp", ":1234", time.Second)
		MustNil(t, err)
		conn.Close()
	}
}
