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

// sysMmap is used to free the URingSQE and URingCQE,
func (r *URing) sysMunmap() (err error) {
	err = syscall.Munmap(r.sqRing.buff)
	if r.cqRing.buff != nil && &r.cqRing.buff[0] != &r.sqRing.buff[0] {
		err = syscall.Munmap(r.cqRing.buff)
	}
	return
}

// sysMmap is used to configure the URingSQE and URingCQE,
// it should only be called after the sysSetUp function has completed successfully.
func (r *URing) sysMmap(p *ringParams) (err error) {
	size := unsafe.Sizeof(URingCQE{})
	if p.flags&IORING_SETUP_CQE32 != 0 {
		size += unsafe.Sizeof(URingCQE{})
	}
	r.sqRing.ringSize = uint64(p.sqOffset.array) + uint64(p.sqEntries*(uint32)(unsafe.Sizeof(uint32(0))))
	r.cqRing.ringSize = uint64(p.cqOffset.cqes) + uint64(p.cqEntries*(uint32)(size))

	if p.features&IORING_FEAT_SINGLE_MMAP != 0 {
		if r.cqRing.ringSize > r.sqRing.ringSize {
			r.sqRing.ringSize = r.cqRing.ringSize
		}
		r.cqRing.ringSize = r.sqRing.ringSize
	}

	// TODO: syscall.MAP_POPULATE unsupport for macox
	data, err := syscall.Mmap(r.fd, 0, int(r.sqRing.ringSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return err
	}
	r.sqRing.buff = data

	if p.features&IORING_FEAT_SINGLE_MMAP != 0 {
		r.cqRing.buff = r.sqRing.buff
	} else {
		// TODO: syscall.MAP_POPULATE unsupport for macox
		data, err = syscall.Mmap(r.fd, int64(IORING_OFF_CQ_RING), int(r.cqRing.ringSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if err != nil {
			r.sysMunmap()
			return err
		}
		r.cqRing.buff = data
	}

	ringStart := &r.sqRing.buff[0]
	r.sqRing.kHead = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.head)))
	r.sqRing.kTail = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.tail)))
	r.sqRing.kRingMask = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.ringMask)))
	r.sqRing.kRingEntries = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.ringEntries)))
	r.sqRing.kFlags = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.flags)))
	r.sqRing.kDropped = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.dropped)))
	r.sqRing.array = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.array)))

	size = unsafe.Sizeof(URingSQE{})
	if p.flags&IORING_SETUP_SQE128 != 0 {
		size += 64
	}
	// TODO: syscall.MAP_POPULATE unsupport for macox
	buff, err := syscall.Mmap(r.fd, int64(IORING_OFF_SQES), int(size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		_ = r.sysMunmap()
		return err
	}
	r.sqRing.sqeBuff = buff

	cqRingPtr := uintptr(unsafe.Pointer(&r.cqRing.buff[0]))
	ringStart = &r.cqRing.buff[0]

	r.cqRing.kHead = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.head)))
	r.cqRing.kTail = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.tail)))
	r.cqRing.kRingMask = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.ringMsk)))
	r.cqRing.kRingEntries = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.ringEntries)))
	r.cqRing.kOverflow = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.overflow)))
	r.cqRing.cqes = (*URingCQE)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.cqes)))
	if p.cqOffset.flags != 0 {
		r.cqRing.kFlags = cqRingPtr + uintptr(p.cqOffset.flags)
	}

	return nil
}
