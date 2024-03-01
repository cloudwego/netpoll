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

package netpoll

import (
	"reflect"
	"unsafe"

	"github.com/bytedance/gopkg/lang/mcache"
)

// zero-copy slice convert to string
func unsafeSliceToString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

// zero-copy slice convert to string
func unsafeStringToSlice(s string) (b []byte) {
	p := unsafe.Pointer((*reflect.StringHeader)(unsafe.Pointer(&s)).Data)
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	hdr.Data = uintptr(p)
	hdr.Cap = len(s)
	hdr.Len = len(s)
	return b
}

// mallocMax is 8MB
const mallocMax = block8k * block1k

// malloc limits the cap of the buffer from mcache.
func malloc(size, capacity int) []byte {
	if capacity > mallocMax {
		return make([]byte, size, capacity)
	}
	return mcache.Malloc(size, capacity)
}

// free limits the cap of the buffer from mcache.
func free(buf []byte) {
	if cap(buf) > mallocMax {
		return
	}
	mcache.Free(buf)
}
