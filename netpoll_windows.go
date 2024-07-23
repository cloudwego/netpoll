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

//go:build windows
// +build windows

// The following methods would not be used, but are intended to compile on Windows.
package netpoll

import (
	"net"
)

// Configure the internal behaviors of netpoll.
func Configure(config Config) (err error) {
	return nil
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
