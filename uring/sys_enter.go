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
)

// Submission Queue Entry, IO submission data structure
type URingSQE struct {
	OpCode     uint8  // type of operation for this sqe
	Flags      uint8  // IOSQE_ flags
	IOPrio     uint16 // ioprio for the request
	Fd         int32  // file descriptor to do IO on
	Off        uint64 // offset into file
	Addr       uint64 // pointer to buffer or iovecs
	Len        uint32 // buffer size or number of iovecs
	UnionFlags uint32
	UserData   uint64 // data to be passed back at completion time

	pad [3]uint64
}

// PrepRW implements SQE
func (s *URingSQE) PrepRW(op OpFlag, fd int32, addr uintptr, len uint32, offset uint64) {
	s.OpCode = uint8(op)
	s.Flags = 0
	s.IOPrio = 0
	s.Fd = fd
	s.Off = offset
	s.setAddr(addr)
	s.Len = len
	s.UnionFlags = 0
	s.UserData = 0
	s.pad[0] = 0
	s.pad[1] = 0
	s.pad[2] = 0
}

// Completion Queue Eveny, IO completion data structure
type URingCQE struct {
	UserData uint64 // sqe->data submission passed back
	Res      int32  // result code for this event
	Flags    uint32

	// If the ring is initialized with IORING_SETUP_CQE32, then this field
	// contains 16-bytes of padding, doubling the size of the CQE.
	BigCQE [2]uint64
}

// Error implements CQE
func (c *URingCQE) Error() error {
	return syscall.Errno(uintptr(-c.Res))
}

// getData implements CQE
func (c *URingCQE) getData() uint64 {
	return c.UserData
}

// setData sets the user data field of the SQE instance passed in.
func (s *URingSQE) setData(ud uint64) {
	s.UserData = ud
}

// setFlags sets the flags field of the SQE instance passed in.
func (s *URingSQE) setFlags(flags uint8) {
	s.Flags = flags
}

// setAddr sets the flags field of the SQE instance passed in.
func (s *URingSQE) setAddr(addr uintptr) {
	s.Addr = uint64(addr)
}

// Flags of CQE
// IORING_CQE_F_BUFFER	If set, the upper 16 bits are the buffer ID
// IORING_CQE_F_MORE	If set, parent SQE will generate more CQE entries
// IORING_CQE_F_SOCK_NONEMPTY	If set, more data to read after socket recv
const (
	IORING_CQE_F_BUFFER OpFlag = 1 << iota
	IORING_CQE_F_MORE
	IORING_CQE_F_SOCK_NONEMPTY
)

const IORING_CQE_BUFFER_SHIFT = 16

// io_uring_enter(2) flags
const (
	IORING_ENTER_GETEVENTS uint32 = 1 << iota
	IORING_ENTER_SQ_WAKEUP
	IORING_ENTER_SQ_WAIT
	IORING_ENTER_EXT_ARG
	IORING_ENTER_REGISTERED_RING
)

// If sqe->file_index is set to this for opcodes that instantiate a new
// direct descriptor (like openat/openat2/accept), then io_uring will allocate
// an available direct descriptor instead of having the application pass one
// in. The picked direct descriptor will be returned in cqe->res, or -ENFILE
// if the space is full.
const (
	IOSQE_FIXED_FILE_BIT = iota
	IOSQE_IO_DRAIN_BIT
	IOSQE_IO_LINK_BIT
	IOSQE_IO_HARDLINK_BIT
	IOSQE_ASYNC_BIT
	IOSQE_BUFFER_SELECT_BIT
	IOSQE_CQE_SKIP_SUCCESS_BIT
)

// Flags of SQE
const (
	// IOSQE_FIXED_FILE means use fixed fileset
	IOSQE_FIXED_FILE uint32 = 1 << IOSQE_FIXED_FILE_BIT
	// IOSQE_IO_DRAIN means issue after inflight IO
	IOSQE_IO_DRAIN uint32 = 1 << IOSQE_IO_DRAIN_BIT
	// IOSQE_IO_LINK means links next sqe
	IOSQE_IO_LINK uint32 = 1 << IOSQE_IO_LINK_BIT
	// IOSQE_IO_HARDLINK means like LINK, but stronger
	IOSQE_IO_HARDLINK uint32 = 1 << IOSQE_IO_HARDLINK_BIT
	// IOSQE_ASYNC means always go async
	IOSQE_ASYNC uint32 = 1 << IOSQE_ASYNC_BIT
	// IOSQE_BUFFER_SELECT means select buffer from sqe->buf_group
	IOSQE_BUFFER_SELECT uint32 = 1 << IOSQE_BUFFER_SELECT_BIT
	// IOSQE_CQE_SKIP_SUCCESS means don't post CQE if request succeeded
	IOSQE_CQE_SKIP_SUCCESS uint32 = 1 << IOSQE_CQE_SKIP_SUCCESS_BIT
)
