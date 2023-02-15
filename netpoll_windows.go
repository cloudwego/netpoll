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

//go:build windows
// +build windows

// The following methods would not be used, but are intended to compile on Windows.
package netpoll

import (
	"net"
	"time"
)

// Option .
type Option struct {
	f func(*options)
}

type options struct{}

// WithOnPrepare registers the OnPrepare method to EventLoop.
func WithOnPrepare(onPrepare OnPrepare) Option {
	return Option{}
}

// WithOnConnect registers the OnConnect method to EventLoop.
func WithOnConnect(onConnect OnConnect) Option {
	return Option{}
}

// WithReadTimeout sets the read timeout of connections.
func WithReadTimeout(timeout time.Duration) Option {
	return Option{}
}

// WithIdleTimeout sets the idle timeout of connections.
func WithIdleTimeout(timeout time.Duration) Option {
	return Option{}
}

// NewDialer only support TCP and unix socket now.
func NewDialer() Dialer {
	return nil
}

// NewEventLoop .
func NewEventLoop(onRequest OnRequest, ops ...Option) (EventLoop, error) {
	return nil, nil
}

// ConvertListener converts net.Listener to Listener
func ConvertListener(l net.Listener) (nl Listener, err error) {
	return nil, nil
}

// CreateListener return a new Listener.
func CreateListener(network, addr string) (l Listener, err error) {
	return nil, nil
}
