// Copyright 2024 CloudWeGo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package netpoll

import (
	"testing"
	"unsafe"
)

func TestFDOperatorRacedAccess(t *testing.T) {
	poll, err := openDefaultPoll()
	MustNil(t, err)

	var data [8]byte
	dataptr := unsafe.Pointer(&data)
	op := poll.Alloc()
	fd := op.FD

	// epoll_ctl register fd
	poll.setOperator(dataptr, op)

	go func() {
		// epoll_wait get fd
		nop := poll.getOperator(fd, dataptr)
		MustTrue(t, nop.FD == fd)
	}()
}
