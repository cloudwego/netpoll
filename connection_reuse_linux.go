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

//go:build linux

package netpoll

import (
	"runtime"
	"sync/atomic"
	"syscall"
)

// IsHealthyForReuse reports whether a dialed TCP connection has no pending
// inbound data and has not been closed by its peer.
//
// The check is synchronized with the netpoll receive owner. In particular, it
// keeps the FDOperator ownership across both the LinkBuffer check and the
// non-consuming socket probe, so a poller cannot move bytes from the kernel
// receive queue into the LinkBuffer between those observations.
//
// It is intended for an idle client connection that is exclusively owned by a
// connection pool. It does not reserve the connection for a later write and
// therefore cannot prevent a peer from closing it after this method returns.
func (c *TCPConnection) IsHealthyForReuse() bool {
	if c == nil || !c.IsActive() || !c.lock(flushing) {
		return false
	}
	defer c.unlock(flushing)

	// Close stops the flushing lock before it can free and recycle the operator.
	// Recheck after acquiring it so a concurrent close cannot hand us a reused
	// operator.
	if !c.IsActive() || c.operator == nil {
		return false
	}

	op := c.operator
	for {
		if !c.IsActive() || op.isUnused() || atomic.LoadInt32(&op.detached) != 0 {
			return false
		}
		if op.do() {
			break
		}
		runtime.Gosched()
	}
	defer op.done()

	// appendHup detaches the operator and calls OnHup asynchronously. A detached
	// operator is no longer eligible even before OnHup publishes closing state.
	if atomic.LoadInt32(&op.detached) != 0 {
		return false
	}

	if !c.IsActive() || c.inputBuffer.Len() != 0 {
		return false
	}

	var buffer [1]byte
	for {
		_, _, err := syscall.Recvfrom(c.fd, buffer[:], syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
		switch err {
		case nil:
			// A positive result is unread application data; a zero-length result
			// is the peer's FIN. Neither connection is safe to reuse.
			return false
		case syscall.EAGAIN: // EWOULDBLOCK is the same errno on Linux.
			// The operator is still owned, so the LinkBuffer cannot be between
			// readv and InputAck while this decision is made. A concurrent user
			// close can detach the operator, so reject it at the decision point.
			return c.IsActive() && c.inputBuffer.Len() == 0 && atomic.LoadInt32(&op.detached) == 0
		case syscall.EINTR:
			continue
		default:
			return false
		}
	}
}
