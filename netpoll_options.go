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
	"context"
	"io"
	"log"
	"os"
	"runtime"
	"time"
)

var (
	pollmanager = newManager(runtime.GOMAXPROCS(0)/20 + 1) // pollmanager manage all pollers
	logger      = log.New(os.Stderr, "", log.LstdFlags)

	// global config
	defaultLinkBufferSize   = pagesize
	featureAlwaysNoCopyRead = false
)

// Config expose some tuning parameters to control the internal behaviors of netpoll.
// Every parameter with the default zero value should keep the default behavior of netpoll.
type Config struct {
	PollerNum    int                                 // number of pollers
	BufferSize   int                                 // default size of a new connection's LinkBuffer
	Runner       func(ctx context.Context, f func()) // runner for event handler, most of the time use a goroutine pool.
	LoggerOutput io.Writer                           // logger output
	LoadBalance  LoadBalance                         // load balance for poller picker
	Feature                                          // define all features that not enable by default
}

// Feature expose some new features maybe promoted as a default behavior but not yet.
type Feature struct {
	// AlwaysNoCopyRead allows some copy Read functions like ReadBinary/ReadString
	// will use NoCopy read and will not reuse the underlying buffer.
	// It gains more performance benefits when need read much big string/bytes in codec.
	AlwaysNoCopyRead bool
}

// Configure the internal behaviors of netpoll.
// Configure must called in init() function, because the poller will read some global variable after init() finished
func Configure(config Config) (err error) {
	if config.PollerNum > 0 {
		if err = pollmanager.SetNumLoops(config.PollerNum); err != nil {
			return err
		}
	}
	if config.BufferSize > 0 {
		defaultLinkBufferSize = config.BufferSize
	}

	if config.Runner != nil {
		setRunner(config.Runner)
	}
	if config.LoggerOutput != nil {
		logger = log.New(config.LoggerOutput, "", log.LstdFlags)
	}
	if config.LoadBalance >= 0 {
		if err = pollmanager.SetLoadBalance(config.LoadBalance); err != nil {
			return err
		}
	}

	featureAlwaysNoCopyRead = config.AlwaysNoCopyRead
	return nil
}

// Initialize the pollers actively. By default, it's lazy initialized.
// It's safe to call it multi times.
func Initialize() {
	// The first call of Pick() will init pollers
	_ = pollmanager.Pick()
}

// SetNumLoops is used to set the number of pollers, generally do not need to actively set.
// By default, the number of pollers is equal to runtime.GOMAXPROCS(0)/20+1.
// If the number of cores in your service process is less than 20c, theoretically only one poller is needed.
// Otherwise you may need to adjust the number of pollers to achieve the best results.
// Experience recommends assigning a poller every 20c.
//
// You can only use SetNumLoops before any connection is created. An example usage:
//
//	func init() {
//	    netpoll.SetNumLoops(...)
//	}
//
// Deprecated: use Configure instead.
func SetNumLoops(numLoops int) error {
	return pollmanager.SetNumLoops(numLoops)
}

// SetLoadBalance sets the load balancing method. Load balancing is always a best effort to attempt
// to distribute the incoming connections between multiple polls.
// This option only works when numLoops is set.
// Deprecated: use Configure instead.
func SetLoadBalance(lb LoadBalance) error {
	return pollmanager.SetLoadBalance(lb)
}

// SetLoggerOutput sets the logger output target.
// Deprecated: use Configure instead.
func SetLoggerOutput(w io.Writer) {
	logger = log.New(w, "", log.LstdFlags)
}

// SetRunner set the runner function for every OnRequest/OnConnect callback
// Deprecated: use Configure instead.
func SetRunner(f func(ctx context.Context, f func())) {
	setRunner(f)
}

// DisableGopool will remove gopool(the goroutine pool used to run OnRequest),
// which means that OnRequest will be run via `go OnRequest(...)`.
// Usually, OnRequest will cause stack expansion, which can be solved by reusing goroutine.
// But if you can confirm that the OnRequest will not cause stack expansion,
// it is recommended to use DisableGopool to reduce redundancy and improve performance.
// Deprecated: use Configure instead.
func DisableGopool() error {
	return disableGopool()
}

// WithOnPrepare registers the OnPrepare method to EventLoop.
func WithOnPrepare(onPrepare OnPrepare) Option {
	return Option{func(op *options) {
		op.onPrepare = onPrepare
	}}
}

// WithOnConnect registers the OnConnect method to EventLoop.
func WithOnConnect(onConnect OnConnect) Option {
	return Option{func(op *options) {
		op.onConnect = onConnect
	}}
}

// WithOnDisconnect registers the OnDisconnect method to EventLoop.
func WithOnDisconnect(onDisconnect OnDisconnect) Option {
	return Option{func(op *options) {
		op.onDisconnect = onDisconnect
	}}
}

// WithReadTimeout sets the read timeout of connections.
func WithReadTimeout(timeout time.Duration) Option {
	return Option{func(op *options) {
		op.readTimeout = timeout
	}}
}

// WithWriteTimeout sets the write timeout of connections.
func WithWriteTimeout(timeout time.Duration) Option {
	return Option{func(op *options) {
		op.writeTimeout = timeout
	}}
}

// WithIdleTimeout sets the idle timeout of connections.
func WithIdleTimeout(timeout time.Duration) Option {
	return Option{func(op *options) {
		op.idleTimeout = timeout
	}}
}

// Option .
type Option struct {
	f func(*options)
}

type options struct {
	onPrepare    OnPrepare
	onConnect    OnConnect
	onDisconnect OnDisconnect
	onRequest    OnRequest
	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration
}
