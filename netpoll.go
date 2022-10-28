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
	"net"
	"runtime"
	"sync"
)

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
	var svr = evl.svr
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
