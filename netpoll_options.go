// Copyright 2024 CloudWeGo Authors
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

import "time"

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
