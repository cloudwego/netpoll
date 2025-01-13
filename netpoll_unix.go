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

//go:build darwin || netbsd || freebsd || openbsd || dragonfly || linux
// +build darwin netbsd freebsd openbsd dragonfly linux

package netpoll

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"sync"

	"github.com/cloudwego/netpoll/internal/runner"
)

var (
	pollmanager = newManager(runtime.GOMAXPROCS(0)/20 + 1) // pollmanager manage all pollers
	logger      = log.New(os.Stderr, "", log.LstdFlags)
)

// Initialize the pollers actively. By default, it's lazy initialized.
// It's safe to call it multi times.
func Initialize() {
	// The first call of Pick() will init pollers
	_ = pollmanager.Pick()
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
		runner.RunTask = config.Runner
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

// SetNumLoops is used to set the number of pollers, generally do not need to actively set.
// By default, the number of pollers is equal to runtime.GOMAXPROCS(0)/20+1.
// If the number of cores in your service process is less than 20c, theoretically only one poller is needed.
// Otherwise, you may need to adjust the number of pollers to achieve the best results.
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
//
// Deprecated: use Configure and specify config.Runner instead.
func SetRunner(f func(ctx context.Context, f func())) {
	runner.RunTask = f
}

// DisableGopool will remove gopool(the goroutine pool used to run OnRequest),
// which means that OnRequest will be run via `go OnRequest(...)`.
// Usually, OnRequest will cause stack expansion, which can be solved by reusing goroutine.
// But if you can confirm that the OnRequest will not cause stack expansion,
// it is recommended to use DisableGopool to reduce redundancy and improve performance.
//
// Deprecated: use Configure() and specify config.Runner instead.
func DisableGopool() error {
	runner.UseGoRunTask()
	return nil
}

// NewEventLoop .
func NewEventLoop(onRequest OnRequest, ops ...Option) (EventLoop, error) {
	opts := &options{
		onRequest: onRequest,
	}
	for _, do := range ops {
		do.f(opts)
	}
	return &eventLoop{
		opts: opts,
		stop: make(chan error, 1),
	}, nil
}

type eventLoop struct {
	sync.Mutex
	opts *options
	svr  *server
	stop chan error
}

// Serve implements EventLoop.
func (evl *eventLoop) Serve(ln net.Listener) error {
	npln, err := ConvertListener(ln)
	if err != nil {
		return err
	}
	evl.Lock()
	evl.svr = newServer(npln, evl.opts, evl.quit)
	evl.svr.Run()
	evl.Unlock()

	err = evl.waitQuit()
	// ensure evl will not be finalized until Serve returns
	runtime.SetFinalizer(evl, nil)
	return err
}

// Shutdown signals a shutdown a begins server closing.
func (evl *eventLoop) Shutdown(ctx context.Context) error {
	evl.Lock()
	svr := evl.svr
	evl.svr = nil
	evl.Unlock()

	if svr == nil {
		return nil
	}
	evl.quit(nil)
	return svr.Close(ctx)
}

// waitQuit waits for a quit signal
func (evl *eventLoop) waitQuit() error {
	return <-evl.stop
}

func (evl *eventLoop) quit(err error) {
	select {
	case evl.stop <- err:
	default:
	}
}
