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

package netpoll

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// white-box helpers (functional tests use *testing.T + existing helpers)
// ---------------------------------------------------------------------------

// connectOnce dials a new TCP connection to addr and returns it.
func connectOnce(t *testing.T, addr string) Connection {
	t.Helper()
	conn, err := DialConnection("tcp", addr, time.Second)
	MustNil(t, err)
	return conn
}

// startEchoServer starts a real in-process netpoll EventLoop that echoes every
// byte back. It returns the listen address.
func startEchoServer(t *testing.T) (addr string, _ EventLoop) {
	t.Helper()
	ln := createTestTCPListener(t)
	addr = ln.Addr().String()
	el, err := NewEventLoop(func(ctx context.Context, connection Connection) error {
		for {
			buf, err := connection.Reader().Next(connection.Reader().Len())
			if err != nil {
				return err
			}
			if len(buf) == 0 {
				return nil
			}
			if _, err = connection.Write(buf); err != nil {
				return err
			}
			if err = connection.Reader().Release(); err != nil {
				return err
			}
		}
	})
	MustNil(t, err)
	go func() {
		_ = el.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = el.Shutdown(context.Background())
	})
	return addr, el
}

// buildIdleServer returns a raw *server whose OnRead we drive manually. No poll
// loop is started, so accept progress is fully controlled by the test.
func buildIdleServer(t *testing.T) (svr *server, addr string, ln net.Listener) {
	t.Helper()
	ln = createTestTCPListener(t)
	npln, err := ConvertListener(ln)
	MustNil(t, err)
	svr = newServer(npln, &options{}, func(err error) {})
	t.Cleanup(func() {
		_ = ln.Close()
	})
	return svr, ln.Addr().String(), ln
}

// acceptedCount returns how many connections the server has accepted so far.
func acceptedCount(svr *server) int {
	n := 0
	svr.connections.Range(func(_, _ interface{}) bool {
		n++
		return true
	})
	return n
}

// ---------------------------------------------------------------------------
// T-series: functional tests
// ---------------------------------------------------------------------------

// TestAcceptUntilEAGAIN_OneRoundAcceptsBurst is the discriminating regression
// test. N connections are fully established and parked in the kernel accept
// queue (nobody is accepting yet). A single OnRead round must accept all of
// them, proving the accept-until-EAGAIN loop.
//
// Upstream v0.7.2..v0.7.5 accepts one connection per event, so this test fails
// on that code (only 1 of N is accepted per OnRead round).
func TestAcceptUntilEAGAIN_OneRoundAcceptsBurst(t *testing.T) {
	svr, addr, _ := buildIdleServer(t)

	const burst = 64
	clients := make([]Connection, 0, burst)
	for i := 0; i < burst; i++ {
		clients = append(clients, connectOnce(t, addr))
	}

	// Give the kernel a moment so all loopback handshakes complete and the
	// connections are parked in the listener's accept queue.
	time.Sleep(100 * time.Millisecond)
	before := acceptedCount(svr)

	// ONE round of OnRead must drain the whole burst.
	MustNil(t, svr.OnRead(nil))

	after := acceptedCount(svr)
	if after-before != burst {
		t.Fatalf("one OnRead accepted %d of %d pending connections; "+
			"expected accept-until-EAGAIN to drain the whole backlog", after-before, burst)
	}

	for _, c := range clients {
		_ = c.Close()
	}
}

// TestAcceptUntilEAGAIN_NoInfiniteLoopOnEmptyQueue proves that OnRead returns
// immediately (nil) when the accept queue is empty, i.e. no busy spin.
func TestAcceptUntilEAGAIN_NoInfiniteLoopOnEmptyQueue(t *testing.T) {
	svr, _, _ := buildIdleServer(t)

	start := time.Now()
	MustNil(t, svr.OnRead(nil))
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("OnRead on empty queue took %v; expected immediate EAGAIN return", elapsed)
	}
}

// TestAcceptUntilEAGAIN_ConcurrentClientsAllServed is an end-to-end regression
// test through the real poll loop: many clients dial, write and read back.
func TestAcceptUntilEAGAIN_ConcurrentClientsAllServed(t *testing.T) {
	addr, _ := startEchoServer(t)

	const clients = 200
	var wg sync.WaitGroup
	errCh := make(chan error, clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn := connectOnce(t, addr)
			defer conn.Close()
			payload := []byte(fmt.Sprintf("hello-%d", id))
			if _, err := conn.Write(payload); err != nil {
				errCh <- err
				return
			}
			buf, err := conn.Reader().Next(len(payload))
			if err != nil {
				errCh <- err
				return
			}
			if string(buf) != string(payload) {
				errCh <- fmt.Errorf("echo mismatch: got %q want %q", buf, payload)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("client error: %v", err)
	}
}

// TestAcceptUntilEAGAIN_IdleConnectionNotStarved guards against the accept loop
// monopolising the poll goroutine: an existing long-lived connection must stay
// responsive while a burst of new connections is being accepted.
func TestAcceptUntilEAGAIN_IdleConnectionNotStarved(t *testing.T) {
	addr, _ := startEchoServer(t)

	longConn := connectOnce(t, addr)
	defer longConn.Close()

	const ping = "ping"
	longDone := make(chan error, 1)
	go func() {
		for i := 0; i < 20; i++ {
			if _, err := longConn.Write([]byte(ping)); err != nil {
				longDone <- err
				return
			}
			buf, err := longConn.Reader().Next(len(ping))
			if err != nil {
				longDone <- err
				return
			}
			if string(buf) != ping {
				longDone <- fmt.Errorf("got %q", buf)
				return
			}
		}
		longDone <- nil
	}()

	var burstWG sync.WaitGroup
	for i := 0; i < 100; i++ {
		burstWG.Add(1)
		go func() {
			defer burstWG.Done()
			conn := connectOnce(t, addr)
			_ = conn.Close()
		}()
	}

	select {
	case err := <-longDone:
		MustNil(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("long-lived connection was starved during accept burst")
	}
	burstWG.Wait()
}

// ---------------------------------------------------------------------------
// benchmarks (own helpers, testing.B cannot reuse *testing.T helpers)
// ---------------------------------------------------------------------------

func benchTCPListener(b *testing.B) (net.Listener, string) {
	b.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("net.Listen: %v", err)
	}
	b.Cleanup(func() { _ = ln.Close() })
	return ln, ln.Addr().String()
}

// benchCountingEL starts a real EventLoop (with its poll loop running) that
// counts every accepted connection via OnConnect. The counter is incremented
// when the server actually accepts a connection, independent of what the
// client observes, so it is a truthful server-side accept-throughput signal.
func benchCountingEL(b *testing.B, ln net.Listener) (el EventLoop, accepted *int64) {
	b.Helper()
	var n int64
	accepted = &n
	var err error
	el, err = NewEventLoop(func(ctx context.Context, connection Connection) error {
		return nil
	}, WithOnConnect(func(ctx context.Context, connection Connection) context.Context {
		atomic.AddInt64(&n, 1)
		return ctx
	}))
	if err != nil {
		b.Fatalf("NewEventLoop: %v", err)
	}
	go func() { _ = el.Serve(ln) }()
	b.Cleanup(func() { _ = el.Shutdown(context.Background()) })
	return el, accepted
}

// BenchmarkAcceptSteadyThroughput measures sustained server-side accept
// throughput under a constant stream of new connections (steady dial-close
// load). This is the load shape where accept batching matters: with the old
// accept-one-per-event behaviour the listener poll round must be re-awakened
// for every pending connection; accept-until-EAGAIN drains the whole backlog
// in one round.
//
// Throughput is counted server-side in OnConnect, NOT from client dial
// success. A fast-accepting server makes the client dial faster until it
// exhausts its ephemeral port range ("cannot assign requested address"); that
// is a client-side artefact and would corrupt a client-measured rate. Counting
// accepted connections on the server measures what this change actually
// affects.
//
// This is a wall-clock benchmark: each b.N iteration runs one steady window of
// NETPOLL_ACCEPT_BENCH_DURATION, so under the default benchtime b.N is 1.
// Read the reported server-accepts/sec metric, not ns/op.
//
// Note: prefer Linux. macOS's small ephemeral port range (~16k) can starve the
// client before the server becomes the bottleneck, which biases the number
// down for both implementations.
//
// Tunables:
//
//	NETPOLL_ACCEPT_BENCH_WORKERS    concurrent dialers (default 400)
//	NETPOLL_ACCEPT_BENCH_DURATION   steady window per iteration (default 3s)
func BenchmarkAcceptSteadyThroughput(b *testing.B) {
	workers := 400
	if v := os.Getenv("NETPOLL_ACCEPT_BENCH_WORKERS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			workers = n
		}
	}
	duration := 3 * time.Second
	if v := os.Getenv("NETPOLL_ACCEPT_BENCH_DURATION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			duration = d
		}
	}

	ln, addr := benchTCPListener(b)
	_, accepted := benchCountingEL(b, ln)

	// steady dial-close workers: a service constantly creating connections
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				c, err := DialConnection("tcp", addr, 5*time.Second)
				if err != nil {
					// client-side failure (e.g. ephemeral port exhaustion) is not
					// counted; server-side accepts below remain truthful
					continue
				}
				_ = c.Close()
			}
		}()
	}

	// warm-up so connection recycling (TIME_WAIT, close callbacks) settles
	time.Sleep(500 * time.Millisecond)

	// measure server-side accepts over one or more steady windows
	var rateSum float64
	for i := 0; i < b.N; i++ {
		base := atomic.LoadInt64(accepted)
		b.ResetTimer()
		start := time.Now()
		time.Sleep(duration)
		b.StopTimer()
		elapsed := time.Since(start)
		delta := atomic.LoadInt64(accepted) - base
		rateSum += float64(delta) / elapsed.Seconds()
	}
	b.ReportMetric(rateSum/float64(b.N), "server-accepts/sec")

	close(stop)
	wg.Wait()
}
