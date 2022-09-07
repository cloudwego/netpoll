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

import "unsafe"

// URing means I/O Userspace Ring
type URing struct {
	cqRing *uringCQ
	sqRing *uringSQ

	fd int

	Params *ringParams
}

// uringSQ means Submit Queue
type uringSQ struct {
	buff    []byte
	sqeBuff []byte

	kHead        *uint32
	kTail        *uint32
	kRingMask    *uint32
	kRingEntries *uint32
	kFlags       *uint32
	kDropped     *uint32
	array        *uint32
	sqes         *URingSQE

	sqeHead uint32
	sqeTail uint32

	ringSize uint64
}

// uringCQ means Completion Queue
type uringCQ struct {
	buff   []byte
	kFlags uintptr

	kHead        *uint32
	kTail        *uint32
	kRingMask    *uint32
	kRingEntries *uint32
	kOverflow    *uint32
	cqes         *URingCQE

	ringSize uint64
}

// submitAndWait implements URing
func (u *URing) submitAndWait(nr uint32) (uint, error) {
	return u.submit(u.flushSQ(), nr)
}

// submit implements URing
func (u *URing) submit(submitted uint32, nr uint32) (uint, error) {
	var flags uint32
	if u.sqRingNeedEnter(&flags) {
		if u.Params.flags&IORING_SETUP_IOPOLL != 0 {
			flags |= IORING_ENTER_GETEVENTS
		}
		if u.Params.flags&INT_FLAG_REG_RING == 1 {
			flags |= IORING_ENTER_REGISTERED_RING
		}
	} else {
		return uint(submitted), nil
	}
	ret, err := SysEnter(u.fd, submitted, 0, flags, nil, NSIG/8)
	return ret, err
}

// flushSQ implements URing
func (u *URing) flushSQ() uint32 {
	mask := *u.sqRing.kRingMask
	tail := SMP_LOAD_ACQUIRE_U32(u.sqRing.kTail)
	subCnt := u.sqRing.sqeTail - u.sqRing.sqeHead

	if subCnt == 0 {
		return tail - SMP_LOAD_ACQUIRE_U32(u.sqRing.kHead)
	}

	for i := subCnt; i > 0; i-- {
		*(*uint32)(unsafe.Add(unsafe.Pointer(u.sqRing.array), tail&mask*uint32(_sizeU32))) = u.sqRing.sqeHead & mask
		tail++
		u.sqRing.sqeHead++
	}

	SMP_STORE_RELEASE_U32(u.sqRing.kTail, tail)

	return tail - SMP_LOAD_ACQUIRE_U32(u.sqRing.kHead)
}
