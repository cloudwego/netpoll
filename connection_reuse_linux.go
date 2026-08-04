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
	"time"
)

// IsHealthyForReuse reports whether a dialed TCP connection has no pending
// inbound data and has not been closed by its peer. ownerWaitTimeout bounds
// only the wait to acquire the receive owner's FDOperator; the socket probe
// itself is non-blocking.
//
// The check is synchronized with the netpoll receive owner. In particular, it
// keeps the FDOperator ownership across both the LinkBuffer check and the
// non-consuming socket probe, so a poller cannot move bytes from the kernel
// receive queue into the LinkBuffer between those observations.
//
// It is intended for an idle client connection that is exclusively owned by a
// connection pool. It does not reserve the connection for a later write and
// therefore cannot prevent a peer from closing it after this method returns.
func (c *TCPConnection) IsHealthyForReuse(ownerWaitTimeout time.Duration) bool {
	// Reject invalid, already-closing, or concurrently flushing connections
	// before touching their receive operator. A non-positive budget cannot
	// safely establish ownership and therefore fails closed.
	if c == nil || ownerWaitTimeout <= 0 || !c.IsActive() || !c.lock(flushing) {
		return false
	}
	// Keep close and flush transitions serialized for the entire observation.
	defer c.unlock(flushing)

	// Close stops the flushing lock before it can free and recycle the operator.
	// Recheck after acquiring it so a concurrent close cannot hand us a reused
	// operator.
	if !c.IsActive() || c.operator == nil {
		return false
	}

	// Try the uncontended owner transition first so the healthy idle fast path
	// avoids both a clock read and a scheduler yield.
	op := c.operator
	if !op.do() {
		// A poller currently owns the operator. Wait only within the caller's
		// budget, while continuously rejecting lifecycle transitions that make
		// this operator ineligible for reuse.
		deadline := time.Now().Add(ownerWaitTimeout)
		for {
			// Stop waiting as soon as the connection closes or the operator starts
			// its detach/recycle lifecycle.
			if !c.IsActive() || op.isUnused() || atomic.LoadInt32(&op.detached) != 0 {
				return false
			}
			// Check the absolute deadline before each ownership attempt so repeated
			// contention cannot extend the caller-provided budget.
			if !time.Now().Before(deadline) {
				return false
			}
			if op.do() {
				// Ownership may be acquired exactly as the deadline expires. Release
				// it explicitly before failing so the poller is never left blocked.
				if !time.Now().Before(deadline) {
					op.done()
					return false
				}
				break
			}
			// Yield instead of busy-spinning while the poller finishes its current
			// read/InputAck critical section.
			runtime.Gosched()
		}
	}
	// Hold receive ownership through both buffer inspection and socket probing;
	// every return below must hand it back to the poller.
	defer op.done()

	// appendHup detaches the operator and calls OnHup asynchronously. A detached
	// operator is no longer eligible even before OnHup publishes closing state.
	if atomic.LoadInt32(&op.detached) != 0 {
		return false
	}

	// Buffered application data means the connection is not an idle reusable
	// transport, even when the kernel receive queue is currently empty.
	if !c.IsActive() || c.inputBuffer.Len() != 0 {
		return false
	}

	// Peek one byte directly from the socket. MSG_PEEK preserves application
	// data and MSG_DONTWAIT guarantees this probe never waits for network I/O.
	var buffer [1]byte
	_, _, err := syscall.Recvfrom(c.fd, buffer[:], syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
	return c.isHealthyForReuseAfterPeek(op, err)
}

// isHealthyForReuseAfterPeek classifies the single nonblocking socket probe
// and revalidates connection state at the final reuse decision point.
func (c *TCPConnection) isHealthyForReuseAfterPeek(op *FDOperator, err error) bool {
	if err != syscall.EAGAIN { // EWOULDBLOCK is the same errno on Linux.
		// Data, FIN, EINTR, and socket errors all fail closed. In particular,
		// EINTR is not retried so this nonblocking probe remains bounded.
		return false
	}

	// The operator is still owned, so the LinkBuffer cannot be between readv
	// and InputAck while this decision is made. A concurrent user close can
	// detach the operator, so reject it at the decision point.
	return c.IsActive() && c.inputBuffer.Len() == 0 && atomic.LoadInt32(&op.detached) == 0
}
