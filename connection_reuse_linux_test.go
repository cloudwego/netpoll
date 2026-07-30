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
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestTCPConnectionIsHealthyForReuseIdle(t *testing.T) {
	conn, _ := newReusableTCPPair(t)
	if !conn.IsHealthyForReuse(time.Second) {
		t.Fatal("idle connection should be reusable")
	}
}

func TestTCPConnectionIsHealthyForReuseWaitsForPoller(t *testing.T) {
	conn, _ := newReusableTCPPair(t)
	releaseOperator := acquireOperatorForReuseTest(t, conn)

	result := make(chan bool, 1)
	go func() {
		result <- conn.IsHealthyForReuse(time.Second)
	}()
	waitForReuseCheckToLockFlushing(t, conn)
	assertReuseCheckBlocked(t, result)

	releaseOperator()
	if !waitReuseCheckResult(t, result) {
		t.Fatal("healthy connection should remain reusable after the poller releases it")
	}
}

// The poller reads from the kernel socket before it acknowledges bytes to the
// LinkBuffer. The reuse check must wait for that ownership interval, otherwise
// it can see an empty kernel socket and an empty LinkBuffer at the same time.
func TestTCPConnectionIsHealthyForReuseRejectsDataBeforeInputAck(t *testing.T) {
	conn, peer := newReusableTCPPair(t)
	releaseOperator := acquireOperatorForReuseTest(t, conn)

	if _, err := peer.Write([]byte("x")); err != nil {
		t.Fatalf("write peer data: %v", err)
	}
	buffers := conn.inputs(make([][]byte, 1))
	n, err := readForReuseTest(conn.fd, buffers)
	if err != nil || n != 1 {
		_ = conn.inputAck(0)
		t.Fatalf("drain socket before InputAck: n=%d err=%v", n, err)
	}

	result := make(chan bool, 1)
	go func() {
		result <- conn.IsHealthyForReuse(time.Second)
	}()
	waitForReuseCheckToLockFlushing(t, conn)
	assertReuseCheckBlocked(t, result)

	if err := conn.inputAck(n); err != nil {
		t.Fatalf("ack input: %v", err)
	}
	releaseOperator()
	if waitReuseCheckResult(t, result) {
		t.Fatal("connection with data drained before InputAck must not be reusable")
	}

	data, err := conn.Peek(1)
	if err != nil {
		t.Fatalf("peek data after reuse check: %v", err)
	}
	if string(data) != "x" {
		t.Fatalf("reuse check consumed or changed data: got %q", data)
	}
}

func TestTCPConnectionIsHealthyForReuseRejectsUnreadDataWithoutConsuming(t *testing.T) {
	conn, peer := newReusableTCPPair(t)
	releaseOperator := acquireOperatorForReuseTest(t, conn)

	if _, err := peer.Write([]byte("x")); err != nil {
		t.Fatalf("write peer data: %v", err)
	}
	result := make(chan bool, 1)
	go func() {
		result <- conn.IsHealthyForReuse(time.Second)
	}()
	waitForReuseCheckToLockFlushing(t, conn)
	assertReuseCheckBlocked(t, result)
	releaseOperator()

	if waitReuseCheckResult(t, result) {
		t.Fatal("connection with unread data must not be reusable")
	}
	data, err := conn.Peek(1)
	if err != nil {
		t.Fatalf("peek data after reuse check: %v", err)
	}
	if string(data) != "x" {
		t.Fatalf("reuse check consumed or changed data: got %q", data)
	}
}

func TestTCPConnectionIsHealthyForReuseRejectsPeerClose(t *testing.T) {
	conn, peer := newReusableTCPPair(t)
	releaseOperator := acquireOperatorForReuseTest(t, conn)

	if err := peer.Close(); err != nil {
		t.Fatalf("close peer: %v", err)
	}
	result := make(chan bool, 1)
	go func() {
		result <- conn.IsHealthyForReuse(time.Second)
	}()
	waitForReuseCheckToLockFlushing(t, conn)
	assertReuseCheckBlocked(t, result)
	releaseOperator()

	if waitReuseCheckResult(t, result) {
		if !waitForUnhealthyForReuse(conn) {
			t.Fatal("peer-closed connection must not be reusable")
		}
	}
}

// appendHup writes detached before it asynchronously calls OnHup. Model that
// published state directly so this contract test cannot unregister a live test
// socket from the shared poller.
func TestTCPConnectionIsHealthyForReuseRejectsDetachedOperatorBeforeOnHup(t *testing.T) {
	conn, _ := newReusableTCPPair(t)
	atomic.StoreInt32(&conn.operator.detached, 1)
	t.Cleanup(func() {
		atomic.StoreInt32(&conn.operator.detached, 0)
	})

	if !conn.IsActive() {
		t.Fatal("probe setup unexpectedly published the closing state")
	}
	if conn.operator.isUnused() {
		t.Fatal("probe setup unexpectedly released the operator")
	}
	if conn.IsHealthyForReuse(time.Second) {
		t.Fatal("detached operator before OnHup must not be reusable")
	}
}

func TestTCPConnectionIsHealthyForReuseRejectsClosedConnection(t *testing.T) {
	conn, _ := newReusableTCPPair(t)
	if err := conn.Close(); err != nil {
		t.Fatalf("close connection: %v", err)
	}
	if conn.IsHealthyForReuse(time.Second) {
		t.Fatal("closed connection must not be reusable")
	}
}

func TestTCPConnectionIsHealthyForReuseRejectsPeerReset(t *testing.T) {
	conn, peer := newReusableTCPPair(t)
	resetPeer, ok := peer.(*net.TCPConn)
	if !ok {
		t.Fatalf("unexpected peer type %T", peer)
	}
	if err := resetPeer.SetLinger(0); err != nil {
		t.Fatalf("set linger: %v", err)
	}

	releaseOperator := acquireOperatorForReuseTest(t, conn)
	if err := peer.Close(); err != nil {
		t.Fatalf("close reset peer: %v", err)
	}
	result := make(chan bool, 1)
	go func() {
		result <- conn.IsHealthyForReuse(time.Second)
	}()
	waitForReuseCheckToLockFlushing(t, conn)
	assertReuseCheckBlocked(t, result)
	releaseOperator()

	if waitReuseCheckResult(t, result) {
		t.Fatal("peer-reset connection must not be reusable")
	}
}

func TestTCPConnectionIsHealthyForReuseRejectsInterruptedProbe(t *testing.T) {
	conn, _ := newReusableTCPPair(t)
	if conn.isHealthyForReuseAfterPeek(conn.operator, syscall.EINTR) {
		t.Fatal("interrupted nonblocking probe must fail closed")
	}
}

func TestTCPConnectionIsHealthyForReuseRejectsNilConnection(t *testing.T) {
	var conn *TCPConnection
	if conn.IsHealthyForReuse(time.Second) {
		t.Fatal("nil connection must not be reusable")
	}
}

func TestTCPConnectionIsHealthyForReuseTimesOutWaitingForPoller(t *testing.T) {
	conn, _ := newReusableTCPPair(t)
	releaseOperator := acquireOperatorForReuseTest(t, conn)
	defer releaseOperator()

	timeout := 20 * time.Millisecond
	started := time.Now()
	if conn.IsHealthyForReuse(timeout) {
		t.Fatal("connection must not be reusable when owner acquisition times out")
	}
	elapsed := time.Since(started)
	if elapsed < timeout {
		t.Fatalf("reuse check returned before timeout: elapsed=%v timeout=%v", elapsed, timeout)
	}
	if elapsed > 10*timeout {
		t.Fatalf("reuse check exceeded bounded wait: elapsed=%v timeout=%v", elapsed, timeout)
	}
	releaseOperator()
	if !conn.IsHealthyForReuse(time.Second) {
		t.Fatal("timeout must not leave operator ownership locked")
	}
}

func TestTCPConnectionIsHealthyForReuseRejectsCloseWhileWaitingForPoller(t *testing.T) {
	conn, _ := newReusableTCPPair(t)
	releaseOperator := acquireOperatorForReuseTest(t, conn)

	reuseResult := make(chan bool, 1)
	go func() {
		reuseResult <- conn.IsHealthyForReuse(time.Second)
	}()
	waitForReuseCheckToLockFlushing(t, conn)
	assertReuseCheckBlocked(t, reuseResult)

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- conn.Close()
	}()
	assertCloseBlocked(t, closeResult)

	releaseOperator()
	if waitReuseCheckResult(t, reuseResult) {
		t.Fatal("connection closed while waiting for the poller must not be reusable")
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("close connection: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close connection timed out")
	}
}

func BenchmarkTCPConnectionIsHealthyForReuseIdle(b *testing.B) {
	conn, _ := newReusableTCPPair(b)
	if !conn.IsHealthyForReuse(time.Second) {
		b.Fatal("warmup rejected idle connection")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !conn.IsHealthyForReuse(time.Second) {
			b.Fatal("idle connection was rejected")
		}
	}
}

func newReusableTCPPair(t testing.TB) (*TCPConnection, net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		peer, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- peer
	}()

	rawConn, err := NewDialer().DialConnection("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		_ = listener.Close()
		t.Fatalf("dial: %v", err)
	}
	conn, ok := rawConn.(*TCPConnection)
	if !ok {
		_ = rawConn.Close()
		_ = listener.Close()
		t.Fatalf("unexpected connection type %T", rawConn)
	}

	var peer net.Conn
	select {
	case peer = <-accepted:
	case err := <-acceptErr:
		_ = conn.Close()
		_ = listener.Close()
		t.Fatalf("accept: %v", err)
	case <-time.After(time.Second):
		_ = conn.Close()
		_ = listener.Close()
		t.Fatal("accept timed out")
	}

	t.Cleanup(func() {
		_ = conn.Close()
		_ = peer.Close()
		_ = listener.Close()
	})
	return conn, peer
}

func acquireOperatorForReuseTest(t *testing.T, conn *TCPConnection) func() {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !conn.operator.do() {
		if time.Now().After(deadline) {
			t.Fatal("acquire operator timed out")
		}
		runtime.Gosched()
	}

	var once sync.Once
	release := func() {
		once.Do(conn.operator.done)
	}
	t.Cleanup(release)
	return release
}

func readForReuseTest(fd int, buffers [][]byte) (int, error) {
	deadline := time.Now().Add(time.Second)
	vectors := make([]syscall.Iovec, len(buffers))
	for {
		n, err := ioread(fd, buffers, vectors)
		if n > 0 || err != nil || time.Now().After(deadline) {
			return n, err
		}
		runtime.Gosched()
	}
}

func assertReuseCheckBlocked(t *testing.T, result <-chan bool) {
	t.Helper()
	select {
	case value := <-result:
		t.Fatalf("reuse check returned before the poller handoff completed: %v", value)
	case <-time.After(10 * time.Millisecond):
	}
}

func waitForReuseCheckToLockFlushing(t *testing.T, conn *TCPConnection) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for conn.status(flushing) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("reuse check did not acquire the flushing lock")
		}
		runtime.Gosched()
	}
}

func assertCloseBlocked(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("close returned before the poller handoff completed: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
}

func waitReuseCheckResult(t *testing.T, result <-chan bool) bool {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(time.Second):
		t.Fatal("reuse check timed out")
		return false
	}
}

func waitForUnhealthyForReuse(conn *TCPConnection) bool {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !conn.IsHealthyForReuse(time.Second) {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
