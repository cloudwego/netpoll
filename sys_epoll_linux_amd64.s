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
TEXT ·BlockSyscall6<ABIInternal>(SB),NOSPLIT,$0
	CALL	·callEntersyscallblock(SB)
	// a6 already in R9.
	// a5 already in R8.
	MOVQ	SI, R10 // a4
	MOVQ	DI, DX  // a3
	MOVQ	CX, SI  // a2
	MOVQ	BX, DI  // a1
	// num already in AX.
	SYSCALL
	CMPQ	AX, $0xfffffffffffff001
	JLS	ok
	NEGQ	AX
	MOVQ	AX, CX  // errno
	MOVQ	$-1, AX // r1
	MOVQ	$0, BX  // r2
	CALL	·callExitsyscall(SB)
	RET
ok:
	// r1 already in AX.
	MOVQ	DX, BX // r2
	MOVQ	$0, CX // errno
	CALL	·callExitsyscall(SB)
	RET
