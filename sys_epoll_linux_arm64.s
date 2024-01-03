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

#include "textflag.h"

// func BlockSyscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, errno uintptr)
TEXT ·BlockSyscall6(SB),NOSPLIT,$0-40
	BL	·callEntersyscallblock(SB)
	MOVW	num+0(FP), R7	// syscall entry
	MOVW	a1+4(FP), R0
	MOVW	a2+8(FP), R1
	MOVW	a3+12(FP), R2
	MOVW	a4+16(FP), R3
	MOVW	a5+20(FP), R4
	MOVW	a6+24(FP), R5
	SWI	$0
	MOVW	$0xfffff001, R6
	CMP	R6, R0
	BLS	ok
	MOVW	$-1, R1
	MOVW	R1, r1+28(FP)
	MOVW	$0, R2
	MOVW	R2, r2+32(FP)
	RSB	$0, R0, R0
	MOVW	R0, errno+36(FP)
	BL	·callExitsyscall(SB)
	RET
ok:
	MOVW	R0, r1+28(FP)
	MOVW	R1, r2+32(FP)
	MOVW	$0, R0
	MOVW	R0, errno+36(FP)
	BL	·callExitsyscall(SB)
	RET
