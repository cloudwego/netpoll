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

// Submit will return the number of SQEs submitted.
func (u *URing) Submit() (uint, error) {
	return u.submitAndWait(0)
}

// SubmitAndWait is the same as Submit(), but takes an additional parameter
// nr that lets you specify how many completions to wait for.
// This call will block until nr submission requests are processed by the kernel
// and their details placed in the CQ.
func (u *URing) SubmitAndWait(nr uint32) (uint, error) {
	return u.submitAndWait(nr)
}

func (u *URing) submitAndWait(nr uint32) (uint, error) {
	return u.submit(u.flushSQ(), nr)
}

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
	ret, err := sysEnter(u.fd, submitted, 0, flags, nil)
	return ret, err
}
