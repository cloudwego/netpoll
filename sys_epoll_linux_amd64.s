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
TEXT ·BlockSyscall6(SB),NOSPLIT,$0-80
	CALL	·callEntersyscallblock(SB)
	MOVQ	num+0(FP), AX	// syscall entry
	MOVQ	a1+8(FP), DI
	MOVQ	a2+16(FP), SI
	MOVQ	a3+24(FP), DX
	MOVQ	a4+32(FP), R10
	MOVQ	a5+40(FP), R8
	MOVQ	a6+48(FP), R9
	SYSCALL
	CMPQ	AX, $0xfffffffffffff001
	JLS	ok
	MOVQ	$-1, r1+56(FP)
	MOVQ	$0, r2+64(FP)
	NEGQ	AX
	MOVQ	AX, errno+72(FP)
	CALL	·callExitsyscall(SB)
	RET
ok:
	MOVQ	AX, r1+56(FP)
	MOVQ	DX, r2+64(FP)
	MOVQ	$0, errno+72(FP)
	CALL	·callExitsyscall(SB)
	RET
