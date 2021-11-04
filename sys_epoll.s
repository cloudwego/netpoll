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

#include "textflag.h"

// func epollwait(epfd int32, ev *epollevent, nev, timeout int32) int32
TEXT ·epollwait(SB),NOSPLIT,$0-28
	MOVL	epfd+0(FP), DI
	MOVQ	ev+8(FP), SI
	MOVL	nev+16(FP), DX
	MOVL	timeout+20(FP), R10
	MOVL	$0xe8, AX // SYS_epoll_wait
	SYSCALL
	MOVL	AX, ret+24(FP)
	RET

// func epollwaitblocking(epfd int32, ev *epollevent, nev, timeout int32) int32
TEXT ·epollwaitblocking(SB),NOSPLIT,$0-28
	CALL	·entersyscallblock(SB)
	MOVL	epfd+0(FP), DI
	MOVQ	ev+8(FP), SI
	MOVL	nev+16(FP), DX
	MOVL	timeout+20(FP), R10
	MOVL	$0xe8, AX // SYS_epoll_wait
	SYSCALL
	MOVL	AX, ret+24(FP)
	CALL	·exitsyscall(SB)
	RET
