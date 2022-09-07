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

// Probe means Probing supported capabilities
type Probe struct {
	lastOp OpFlag // last opcode supported
	opsLen uint8  // length of ops[] array below
	resv   uint16
	resv2  [3]uint32
	ops    [256]probeOp
}

// probeOp is params of Probe
type probeOp struct {
	op    OpFlag
	resv  uint8
	flags uint16 // IO_URING_OP_* flags
	resv2 uint32
}

// Op implements Probe, returns info for operation by flag.
func (p Probe) Op(idx int) *probeOp {
	return &p.ops[idx]
}

// OpFlagSupported implements Probe
func (p Probe) OpFlagSupported(op OpFlag) uint16 {
	if op > p.lastOp {
		return 0
	}
	return uint16(p.ops[op].flags) & IO_URING_OP_SUPPORTED
}

// IO_URING_OP_SUPPORTED means OpFlags whether io_uring supported or not
const IO_URING_OP_SUPPORTED uint16 = 1 << 0
