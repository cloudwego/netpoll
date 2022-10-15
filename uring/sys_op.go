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

package uring

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Op supports operations for SQE
type Op interface {
	Prep(*URingSQE)
	getFlag() uint8
}

// Flags of URing Operation
const (
	IORING_OP_NOP uint8 = iota
	IORING_OP_READV
	IORING_OP_WRITEV
	IORING_OP_FSYNC
	IORING_OP_READ_FIXED
	IORING_OP_WRITE_FIXED
	IORING_OP_POLL_ADD
	IORING_OP_POLL_REMOVE
	IORING_OP_SYNC_FILE_RANGE
	IORING_OP_SENDMSG
	IORING_OP_RECVMSG
	IORING_OP_TIMEOUT
	IORING_OP_TIMEOUT_REMOVE
	IORING_OP_ACCEPT
	IORING_OP_ASYNC_CANCEL
	IORING_OP_LINK_TIMEOUT
	IORING_OP_CONNECT
	IORING_OP_FALLOCATE
	IORING_OP_OPENAT
	IORING_OP_CLOSE
	IORING_OP_RSRC_UPDATE
	IORING_OP_FILES_UPDATE = IORING_OP_RSRC_UPDATE
	IORING_OP_STATX
	IORING_OP_READ
	IORING_OP_WRITE
	IORING_OP_FADVISE
	IORING_OP_MADVISE
	IORING_OP_SEND
	IORING_OP_RECV
	IORING_OP_OPENAT2
	IORING_OP_EPOLL_CTL
	IORING_OP_SPLICE
	IORING_OP_PROVIDE_BUFFERS
	IORING_OP_REMOVE_BUFFERS
	IORING_OP_TEE
	IORING_OP_SHUTDOWN
	IORING_OP_RENAMEAT
	IORING_OP_UNLINKAT
	IORING_OP_MKDIRAT
	IORING_OP_SYMLINKAT
	IORING_OP_LINKAT
	IORING_OP_MSG_RING
	IORING_OP_FSETXATTR
	IORING_OP_SETXATTR
	IORING_OP_FGETXATTR
	IORING_OP_GETXATTR
	IORING_OP_SOCKET
	IORING_OP_URING_CMD
	IORING_OP_SENDZC_NOTIF

	// this goes last, obviously */
	IORING_OP_LAST
)

// timeoutFlags of SQE
const (
	IORING_TIMEOUT_ABS uint8 = 1 << iota
	IORING_TIMEOUT_UPDATE
	IORING_TIMEOUT_BOOTTIME
	IORING_TIMEOUT_REALTIME
	IORING_LINK_TIMEOUT_UPDATE
	IORING_TIMEOUT_ETIME_SUCCESS
	IORING_TIMEOUT_CLOCK_MASK  = IORING_TIMEOUT_BOOTTIME | IORING_TIMEOUT_REALTIME
	IORING_TIMEOUT_UPDATE_MASK = IORING_TIMEOUT_UPDATE | IORING_LINK_TIMEOUT_UPDATE
)

// sqe->splice_flags, extends splice(2) flags
const SPLICE_F_FD_IN_FIXED uint32 = 1 << 31 // the last bit of __u32

// POLL_ADD flags. Note that since sqe->poll_events is the flag space, the
// command flags for POLL_ADD are stored in sqe->len.

// IORING_POLL_ADD_MULTI	Multishot poll. Sets IORING_CQE_F_MORE if
//				the poll handler will continue to report
//				CQEs on behalf of the same SQE.

// IORING_POLL_UPDATE		Update existing poll request, matching
//				sqe->addr as the old user_data field.

// IORING_POLL_LEVEL		Level triggered poll.
const (
	IORING_POLL_ADD_MULTI uint8 = 1 << iota
	IORING_POLL_UPDATE_EVENTS
	IORING_POLL_UPDATE_USER_DATA
	IORING_POLL_ADD_LEVEL
)

// ASYNC_CANCEL flags.

// IORING_ASYNC_CANCEL_ALL	Cancel all requests that match the given key
// IORING_ASYNC_CANCEL_FD	Key off 'fd' for cancelation rather than the
//				request 'user_data'
// IORING_ASYNC_CANCEL_ANY	Match any request
// IORING_ASYNC_CANCEL_FD_FIXED	'fd' passed in is a fixed descriptor
const (
	IORING_ASYNC_CANCEL_ALL uint8 = 1 << iota
	IORING_ASYNC_CANCEL_FD
	IORING_ASYNC_CANCEL_ANY
	IORING_ASYNC_CANCEL_FD_FIXED
)

// send/sendmsg and recv/recvmsg flags (sqe->ioprio)

// IORING_RECVSEND_POLL_FIRST	If set, instead of first attempting to send
//				or receive and arm poll if that yields an
//				-EAGAIN result, arm poll upfront and skip
//				the initial transfer attempt.

// IORING_RECV_MULTISHOT	Multishot recv. Sets IORING_CQE_F_MORE if
//				the handler will continue to report
//				CQEs on behalf of the same SQE.

// IORING_RECVSEND_FIXED_BUF	Use registered buffers, the index is stored in
//				the buf_index field.

// IORING_RECVSEND_NOTIF_FLUSH	Flush a notification after a successful
//				successful. Only for zerocopy sends.

const (
	IORING_RECVSEND_POLL_FIRST uint8 = 1 << iota
	IORING_RECV_MULTISHOT
	IORING_RECVSEND_FIXED_BUF
	IORING_RECVSEND_NOTIF_FLUSH
)

// accept flags stored in sqe->ioprio
const IORING_ACCEPT_MULTISHOT uint8 = 1 << iota

// IORING_OP_RSRC_UPDATE flags
const (
	IORING_RSRC_UPDATE_FILES uint8 = iota
	IORING_RSRC_UPDATE_NOTIF
)

// IORING_OP_MSG_RING command types, stored in sqe->addr
const (
	IORING_MSG_DATA    uint8 = iota // pass sqe->len as 'res' and off as user_data */
	IORING_MSG_SEND_FD              // send a registered fd to another ring */
)

// IORING_OP_MSG_RING flags (sqe->msg_ring_flags)

// IORING_MSG_RING_CQE_SKIP	Don't post a CQE to the target ring. Not
//				applicable for IORING_MSG_DATA, obviously.

const IORING_MSG_RING_CQE_SKIP uint8 = iota

// ------------------------------------------ implement Nop ------------------------------------------

func Nop() *NopOp {
	return &NopOp{}
}

type NopOp struct{}

func (op *NopOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), -1, uintptr(unsafe.Pointer(nil)), 0, 0)
}

func (op *NopOp) getFlag() uint8 {
	return IORING_OP_NOP
}

// ------------------------------------------ implement Read ------------------------------------------

func Read(fd uintptr, nbytes []byte, offset uint64) *ReadOp {
	return &ReadOp{
		fd:     fd,
		nbytes: nbytes,
		offset: offset,
	}
}

type ReadOp struct {
	fd     uintptr
	nbytes []byte
	offset uint64
}

func (op *ReadOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), uintptr(unsafe.Pointer(&op.nbytes[0])), uint32(len(op.nbytes)), op.offset)
}

func (op *ReadOp) getFlag() uint8 {
	return IORING_OP_READ
}

// ------------------------------------------ implement Write ------------------------------------------

func Write(fd uintptr, nbytes []byte, offset uint64) *WriteOp {
	return &WriteOp{
		fd:     fd,
		nbytes: nbytes,
		offset: offset,
	}
}

type WriteOp struct {
	fd     uintptr
	nbytes []byte
	offset uint64
}

func (op *WriteOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), uintptr(unsafe.Pointer(&op.nbytes[0])), uint32(len(op.nbytes)), op.offset)
}

func (op *WriteOp) getFlag() uint8 {
	return IORING_OP_WRITE
}

// ------------------------------------------ implement ReadV ------------------------------------------

func ReadV(fd uintptr, iovecs [][]byte, offset uint64) *ReadVOp {
	buff := make([]syscall.Iovec, len(iovecs))
	for i := range iovecs {
		buff[i].Base = &iovecs[i][0]
		buff[i].SetLen(len(iovecs[i]))
	}
	return &ReadVOp{
		fd:     fd,
		nrVecs: uint32(len(buff)),
		ioVecs: buff,
		offset: offset,
	}
}

type ReadVOp struct {
	fd     uintptr
	nrVecs uint32
	ioVecs []syscall.Iovec
	offset uint64
}

func (op *ReadVOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), uintptr(unsafe.Pointer(&op.ioVecs[0])), op.nrVecs, op.offset)
}

func (op *ReadVOp) getFlag() uint8 {
	return IORING_OP_READV
}

// ------------------------------------------ implement WriteV ------------------------------------------

func WriteV(fd uintptr, iovecs [][]byte, offset uint64) *WriteVOp {
	buff := make([]syscall.Iovec, len(iovecs))
	for i := range iovecs {
		buff[i].SetLen(len(iovecs[i]))
		buff[i].Base = &iovecs[i][0]
	}
	return &WriteVOp{
		fd:     fd,
		ioVecs: buff,
		offset: offset,
	}
}

type WriteVOp struct {
	fd     uintptr
	ioVecs []syscall.Iovec
	offset uint64
}

func (op *WriteVOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), uintptr(unsafe.Pointer(&op.ioVecs[0])), uint32(len(op.ioVecs)), op.offset)
}

func (op *WriteVOp) getFlag() uint8 {
	return IORING_OP_WRITEV
}

// ------------------------------------------ implement Close ------------------------------------------

func Close(fd uintptr) *CloseOp {
	return &CloseOp{
		fd: fd,
	}
}

type CloseOp struct {
	fd uintptr
}

func (op *CloseOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), 0, 0, 0)
}

func (op *CloseOp) getFlag() uint8 {
	return IORING_OP_CLOSE
}

// ------------------------------------------ implement RecvMsg ------------------------------------------

func RecvMsg(fd int, msg *syscall.Msghdr, flags uint32) *RecvMsgOp {
	return &RecvMsgOp{
		fd:    fd,
		msg:   msg,
		flags: flags,
	}
}

type RecvMsgOp struct {
	fd    int
	msg   *syscall.Msghdr
	flags uint32
}

func (op *RecvMsgOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), uintptr(unsafe.Pointer(op.msg)), 1, 0)
	sqe.Flags = uint8(op.flags)
}

func (op *RecvMsgOp) getFlag() uint8 {
	return IORING_OP_RECVMSG
}

// ------------------------------------------ implement SendMsg ------------------------------------------

func SendMsg(fd int, msg *syscall.Msghdr, flags uint32) *SendMsgOp {
	return &SendMsgOp{
		fd:    fd,
		msg:   msg,
		flags: flags,
	}
}

type SendMsgOp struct {
	fd    int
	msg   *syscall.Msghdr
	flags uint32
}

func (op *SendMsgOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), uintptr(unsafe.Pointer(op.msg)), 1, 0)
	sqe.setFlags(uint8(op.flags))
}

func (op *SendMsgOp) getFlag() uint8 {
	return IORING_OP_SENDMSG
}

// ------------------------------------------ implement Accept ------------------------------------------

func Accept(fd uintptr, flags uint32) *AcceptOp {
	return &AcceptOp{
		fd:    fd,
		addr:  &unix.RawSockaddrAny{},
		len:   unix.SizeofSockaddrAny,
		flags: flags,
	}
}

type AcceptOp struct {
	fd    uintptr
	addr  *unix.RawSockaddrAny
	len   uint32
	flags uint32
}

func (op *AcceptOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), uintptr(unsafe.Pointer(op.addr)), 0, uint64(uintptr(unsafe.Pointer(&op.len))))
	sqe.UnionFlags = op.flags
}

func (op *AcceptOp) getFlag() uint8 {
	return IORING_OP_ACCEPT
}

func (op *AcceptOp) Fd() int {
	return int(op.fd)
}

// ------------------------------------------ implement Recv ------------------------------------------

func Recv(sockFd uintptr, buf []byte, flags uint32) *RecvOp {
	return &RecvOp{
		fd:    sockFd,
		buf:   buf,
		flags: flags,
	}
}

type RecvOp struct {
	fd    uintptr
	buf   []byte
	flags uint32
}

func (op *RecvOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), uintptr(unsafe.Pointer(&op.buf[0])), uint32(len(op.buf)), 0)
	sqe.setFlags(uint8(op.flags))
}

func (op *RecvOp) getFlag() uint8 {
	return IORING_OP_RECV
}

func (op *RecvOp) Fd() int {
	return int(op.fd)
}

func (op *RecvOp) SetBuff(buf []byte) {
	op.buf = buf
}

// ------------------------------------------ implement Send ------------------------------------------

func Send(sockFd uintptr, buf []byte, flags uint32) *SendOp {
	return &SendOp{
		fd:    sockFd,
		buf:   buf,
		flags: flags,
	}
}

type SendOp struct {
	fd    uintptr
	buf   []byte
	flags uint32
}

func (op *SendOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), uintptr(unsafe.Pointer(&op.buf[0])), uint32(len(op.buf)), 0)
	sqe.setFlags(uint8(op.flags))
}

func (op *SendOp) getFlag() uint8 {
	return IORING_OP_SEND
}

func (op *SendOp) Fd() int {
	return int(op.fd)
}

func (op *SendOp) SetBuff(buf []byte) {
	op.buf = buf
}

// ------------------------------------------ implement Timeout ------------------------------------------

func Timeout(duration time.Duration) *TimeoutOp {
	return &TimeoutOp{
		dur: duration,
	}
}

type TimeoutOp struct {
	dur time.Duration
}

func (op *TimeoutOp) Prep(sqe *URingSQE) {
	spec := syscall.NsecToTimespec(op.dur.Nanoseconds())
	sqe.PrepRW(op.getFlag(), -1, uintptr(unsafe.Pointer(&spec)), 1, 0)
}

func (op *TimeoutOp) getFlag() uint8 {
	return IORING_OP_TIMEOUT
}

// ------------------------------------------ implement PollAdd ------------------------------------------

func PollAdd(fd uintptr, mask uint32) *PollAddOp {
	return &PollAddOp{
		fd:       fd,
		pollMask: mask,
	}
}

type PollAddOp struct {
	fd       uintptr
	pollMask uint32
}

func (op *PollAddOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), int32(op.fd), uintptr(unsafe.Pointer(nil)), 0, 0)
	sqe.UnionFlags = op.pollMask
}

func (op *PollAddOp) getFlag() uint8 {
	return IORING_OP_POLL_ADD
}

// ------------------------------------------ implement PollRemove ------------------------------------------

func PollRemove(data uint64) *PollRemoveOp {
	return &PollRemoveOp{
		userData: data,
	}
}

type PollRemoveOp struct {
	userData uint64
}

func (op *PollRemoveOp) Prep(sqe *URingSQE) {
	sqe.PrepRW(op.getFlag(), -1, uintptr(unsafe.Pointer(nil)), 0, 0)
	sqe.setAddr(uintptr(op.userData))
}

func (op *PollRemoveOp) getFlag() uint8 {
	return IORING_OP_POLL_REMOVE
}
