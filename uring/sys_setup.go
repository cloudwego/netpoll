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

import "time"

// ringParams means params of Uring
type ringParams struct {
	sqEntries    uint32
	cqEntries    uint32
	flags        uint32
	sqThreadCPU  uint32
	sqThreadIdle uint32
	features     uint32
	wqFD         uint32
	resv         [3]uint32
	sqOffset     sqRingOffsets
	cqOffset     cqRingOffsets
}

// sqRingOffsets means offsets of SQ Ring
type sqRingOffsets struct {
	head        uint32
	tail        uint32
	ringMask    uint32
	ringEntries uint32
	flags       uint32
	dropped     uint32
	array       uint32
	resv1       uint32
	resv2       uint64
}

// cqRingOffsets means offsets of CQ Ring
type cqRingOffsets struct {
	head        uint32
	tail        uint32
	ringMask    uint32
	ringEntries uint32
	overflow    uint32
	cqes        uint32
	flags       uint32
	resv1       uint32
	resv2       uint64
}

// sysSetUp() flags, used to configure the io_uring instance
const (
	// IORING_SETUP_IOPOLL, used to show io_context is polled
	IORING_SETUP_IOPOLL uint32 = 1 << iota
	// IORING_SETUP_SQPOLL, used to start SQ poll thread
	IORING_SETUP_SQPOLL
	// IORING_SETUP_SQ_AFF, used to make sq_thread_cpu valid
	IORING_SETUP_SQ_AFF
	// IORING_SETUP_CQSIZE, used to app defines CQ size
	IORING_SETUP_CQSIZE
	// IORING_SETUP_CLAMP, used to clamp SQ/CQ ring sizes
	IORING_SETUP_CLAMP
	// IORING_SETUP_ATTACH_WQ, used to attach to existing wq
	IORING_SETUP_ATTACH_WQ
	// IORING_SETUP_R_DISABLED, used to start with ring disabled
	IORING_SETUP_R_DISABLED
	// IORING_SETUP_SUBMIT_ALL, used to continue submit on error
	IORING_SETUP_SUBMIT_ALL

	// Cooperative task running. When requests complete, they often require
	// forcing the submitter to transition to the kernel to complete. If this
	// flag is set, work will be done when the task transitions anyway, rather
	// than force an inter-processor interrupt reschedule. This avoids interrupting
	// a task running in userspace, and saves an IPI.
	IORING_SETUP_COOP_TASKRUN

	// If COOP_TASKRUN is set, get notified if task work is available for
	// running and a kernel transition would be needed to run it. This sets
	// IORING_SQ_TASKRUN in the sq ring flags. Not valid with COOP_TASKRUN.
	IORING_SETUP_TASKRUN_FLAG
	IORING_SETUP_SQE128 // IORING_SETUP_SQE128, SQEs are 128 byte
	IORING_SETUP_CQE32  // IORING_SETUP_CQE32, CQEs are 32 byte

	// Only one task is allowed to submit requests
	IORING_SETUP_SINGLE_ISSUER
)

// Features flags of ringParams
const (
	IORING_FEAT_SINGLE_MMAP uint32 = 1 << iota
	IORING_FEAT_NODROP
	IORING_FEAT_SUBMIT_STABLE
	IORING_FEAT_RW_CUR_POS
	IORING_FEAT_CUR_PERSONALITY
	IORING_FEAT_FAST_POLL
	IORING_FEAT_POLL_32BITS
	IORING_FEAT_SQPOLL_NONFIXED
	IORING_FEAT_EXT_ARG
	IORING_FEAT_NATIVE_WORKERS
	IORING_FEAT_RSRC_TAGS
	IORING_FEAT_CQE_SKIP
	IORING_FEAT_LINKED_FILE
)

// setupOp provide options for io_uring instance when building
type setupOp func(params *ringParams)

// ------------------------------------------ implement io_uring_setup ------------------------------------------

// IOPoll performs busy-waiting for an I/O completion, as opposed to
// getting notifications via an asynchronous IRQ (Interrupt Request)
func IOPoll() setupOp {
	return func(params *ringParams) {
		params.flags |= IORING_SETUP_IOPOLL
	}
}

// SQPoll creates a kernel thread to perform submission queue polling,
// when this flag is specified.
func SQPoll(idle time.Duration) setupOp {
	return func(params *ringParams) {
		params.flags |= IORING_SETUP_SQPOLL
		params.sqThreadIdle = uint32(idle.Milliseconds())
	}
}

// SQAff will binds the poll thread to the cpu set in the sq_thread_cpu field of the struct ringParams if it is specified.
// This flag is only meaningful when IORING_SETUP_SQPOLL is specified.
func SQAff(cpu uint32) setupOp {
	return func(params *ringParams) {
		params.flags |= IORING_SETUP_SQ_AFF
		params.sqThreadCPU = cpu
	}
}

// CQSize creates the CQ with struct ringParams.cqes.
func CQSize(sz uint32) setupOp {
	return func(params *ringParams) {
		params.flags |= IORING_SETUP_CQSIZE
		params.cqEntries = sz
	}
}

func AttachWQ(fd uint32) setupOp {
	return func(params *ringParams) {
		params.flags |= IORING_SETUP_ATTACH_WQ
		params.wqFD = fd
	}
}

func URingDisabled() setupOp {
	return func(params *ringParams) {
		params.flags |= IORING_SETUP_R_DISABLED
	}
}
