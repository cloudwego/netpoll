// Copyright 2021 CloudWeGo Authors
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
	"syscall"
	"unsafe"
)

// just support ipv4
func SetKeepAlive(fd fdtype, secs int) error {

	// open keep-alive
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1); err != nil {
		return err
	}
	var keepAliveIn = syscall.TCPKeepalive{OnOff: 1, Time: uint32(secs) * 1000, Interval: uint32(secs) * 1000}
	var bytesReturn uint32 = 0
	return syscall.WSAIoctl(fd, syscall.SIO_KEEPALIVE_VALS, (*byte)(unsafe.Pointer(&keepAliveIn)), uint32(unsafe.Sizeof(keepAliveIn)), nil, 0, &bytesReturn, nil, 0)
}
