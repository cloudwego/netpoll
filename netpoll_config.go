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

import (
	"context"
	"io"
)

// global config
var (
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
