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

//go:build loong64
// +build loong64

package netpoll

import (
	"syscall"
)

const EPOLLET = syscall.EPOLLET
const SYS_EPOLL_WAIT = syscall.SYS_EPOLL_PWAIT

type epollevent struct {
	events uint32
	_      int32
	data   [8]byte // unaligned uintptr
}
