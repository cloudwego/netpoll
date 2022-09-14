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
	"unsafe"
)

// sysMmap is used to free the URingSQE and URingCQE,
func (u *URing) sysMunmap() (err error) {
	err = mumap(u.sqRing.buff)
	if u.cqRing.buff != nil && &u.cqRing.buff[0] != &u.sqRing.buff[0] {
		err = mumap(u.cqRing.buff)
	}
	return
}

// sysMmap is used to configure the URingSQE and URingCQE,
// it should only be called after the sysSetUp function has completed successfully.
func (u *URing) sysMmap(p *ringParams) (err error) {
	size := _sizeCQE
	if p.flags&IORING_SETUP_CQE32 != 0 {
		size += _sizeCQE
	}
	u.sqRing.ringSize = uint64(p.sqOffset.array) + uint64(p.sqEntries*(uint32)(_sizeU32))
	u.cqRing.ringSize = uint64(p.cqOffset.cqes) + uint64(p.cqEntries*(uint32)(size))

	if p.features&IORING_FEAT_SINGLE_MMAP != 0 {
		if u.cqRing.ringSize > u.sqRing.ringSize {
			u.sqRing.ringSize = u.cqRing.ringSize
		}
		u.cqRing.ringSize = u.sqRing.ringSize
	}

	data, err := mmap(u.fd, 0, int(u.sqRing.ringSize))
	if err != nil {
		return err
	}
	u.sqRing.buff = data

	if p.features&IORING_FEAT_SINGLE_MMAP != 0 {
		u.cqRing.buff = u.sqRing.buff
	} else {
		data, err = mmap(u.fd, int64(IORING_OFF_CQ_RING), int(u.cqRing.ringSize))
		if err != nil {
			u.sysMunmap()
			return err
		}
		u.cqRing.buff = data
	}

	ringStart := &u.sqRing.buff[0]
	u.sqRing.kHead = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.head)))
	u.sqRing.kTail = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.tail)))
	u.sqRing.kRingMask = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.ringMask)))
	u.sqRing.kRingEntries = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.ringEntries)))
	u.sqRing.kFlags = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.flags)))
	u.sqRing.kDropped = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.dropped)))
	u.sqRing.array = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.sqOffset.array)))

	size = uintptr(p.sqEntries) * _sizeSQE

	buff, err := mmap(u.fd, int64(IORING_OFF_SQES), int(size))
	if err != nil {
		_ = u.sysMunmap()
		return err
	}
	u.sqRing.sqeBuff = buff

	cqRingPtr := uintptr(unsafe.Pointer(&u.cqRing.buff[0]))
	ringStart = &u.cqRing.buff[0]

	u.cqRing.kHead = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.head)))
	u.cqRing.kTail = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.tail)))
	u.cqRing.kRingMask = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.ringMsk)))
	u.cqRing.kRingEntries = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.ringEntries)))
	u.cqRing.kOverflow = (*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.overflow)))
	u.cqRing.cqes = (*URingCQE)(unsafe.Pointer(uintptr(unsafe.Pointer(ringStart)) + uintptr(p.cqOffset.cqes)))
	if p.cqOffset.flags != 0 {
		u.cqRing.kFlags = cqRingPtr + uintptr(p.cqOffset.flags)
	}

	return nil
}

func mumap(b []byte) (err error) {
	return syscall.Munmap(b)
}

func mmap(fd int, offset int64, length int) (data []byte, err error) {
	return syscall.Mmap(fd, offset, length, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED|syscall.MAP_POPULATE)
}

// Magic offsets for the application to mmap the data it needs
const (
	// IORING_OFF_SQ_RING maps sqring to program memory space
	IORING_OFF_SQ_RING uint64 = 0
	// IORING_OFF_CQ_RING maps cqring to program memory space
	IORING_OFF_CQ_RING uint64 = 0x8000000
	// IORING_OFF_SQES maps sqes array to program memory space
	IORING_OFF_SQES uint64 = 0x10000000
)
