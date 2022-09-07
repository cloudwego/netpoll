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

package uring

import (
	"syscall"
	"unsafe"
)

// io_uring_register(2) opcodes and arguments
const (
	IORING_REGISTER_BUFFERS = iota
	IORING_UNREGISTER_BUFFERS
	IORING_REGISTER_FILES
	IORING_UNREGISTER_FILES
	IORING_REGISTER_EVENTFD
	IORING_UNREGISTER_EVENTFD
	IORING_REGISTER_FILES_UPDATE
	IORING_REGISTER_EVENTFD_ASYNC
	IORING_REGISTER_PROBE
	IORING_REGISTER_PERSONALITY
	IORING_UNREGISTER_PERSONALITY
	IORING_REGISTER_RESTRICTIONS
	IORING_REGISTER_ENABLE_RINGS

	/* extended with tagging */
	IORING_REGISTER_FILES2
	IORING_REGISTER_FILES_UPDATE2
	IORING_REGISTER_BUFFERS2
	IORING_REGISTER_BUFFERS_UPDATE

	/* set/clear io-wq thread affinities */
	IORING_REGISTER_IOWQ_AFF
	IORING_UNREGISTER_IOWQ_AFF

	/* set/get max number of io-wq workers */
	IORING_REGISTER_IOWQ_MAX_WORKERS

	/* register/unregister io_uring fd with the ring */
	IORING_REGISTER_RING_FDS
	IORING_UNREGISTER_RING_FDS

	/* register ring based provide buffer group */
	IORING_REGISTER_PBUF_RING
	IORING_UNREGISTER_PBUF_RING

	/* this goes last */
	IORING_REGISTER_LAST
)

// ------------------------------------------ implement io_uring_register ------------------------------------------

// RegisterBuffers regists shared buffers
func (u *URing) RegisterBuffers(buffers []syscall.Iovec) error {
	return SysRegister(u.fd, IORING_REGISTER_BUFFERS, unsafe.Pointer(&buffers[0]), len(buffers))
}

// UnRegisterBuffers unregists shared buffers
func (u *URing) UnRegisterBuffers() error {
	return SysRegister(u.fd, IORING_UNREGISTER_BUFFERS, unsafe.Pointer(nil), 0)
}

// RegisterBuffers regists shared files
func (u *URing) RegisterFilse(dp []int) error {
	return SysRegister(u.fd, IORING_REGISTER_FILES, unsafe.Pointer(&dp[0]), len(dp))
}

// UnRegisterBuffers unregists shared files
func (u *URing) UnRegisterFiles() error {
	return SysRegister(u.fd, IORING_UNREGISTER_FILES, unsafe.Pointer(nil), 0)
}
