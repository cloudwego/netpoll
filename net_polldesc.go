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

//go:build !windows
// +build !windows

package netpoll

import (
	"context"
)

func newPollDesc(fd int) *pollDesc {
	pd := &pollDesc{}
	poll := pollmanager.Pick()
	pd.operator = &FDOperator{
		poll:    poll,
		FD:      fd,
		OnWrite: pd.onwrite,
		OnHup:   pd.onhup,
	}
	pd.writeTrigger = make(chan struct{})
	pd.closeTrigger = make(chan struct{})
	return pd
}

type pollDesc struct {
	operator *FDOperator
	// The write event is OneShot, then mark the writable to skip duplicate calling.
	writeTrigger chan struct{}
	closeTrigger chan struct{}
}

// WaitWrite .
func (pd *pollDesc) WaitWrite(ctx context.Context) (err error) {
	if pd.operator.isUnused() {
		// add ET|Write|Hup
		if err = pd.operator.Control(PollWritable); err != nil {
			logger.Printf("NETPOLL: pollDesc register operator failed: %v", err)
			return err
		}
	}

	select {
	case <-pd.closeTrigger:
		// no need to detach, since poller has done it in OnHup.
		return Exception(ErrConnClosed, "by peer")
	case <-pd.writeTrigger:
		err = nil
	case <-ctx.Done():
		// deregister from poller, upper caller function will close fd
		pd.detach()
		err = mapErr(ctx.Err())
	}
	// double check close trigger
	select {
	case <-pd.closeTrigger:
		return Exception(ErrConnClosed, "by peer")
	default:
		return err
	}
}

func (pd *pollDesc) onwrite(p Poll) error {
	select {
	case <-pd.writeTrigger:
	default:
		pd.detach()
		close(pd.writeTrigger)
	}
	return nil
}

func (pd *pollDesc) onhup(p Poll) error {
	select {
	case <-pd.closeTrigger:
	default:
		close(pd.closeTrigger)
	}
	return nil
}

func (pd *pollDesc) detach() {
	if err := pd.operator.Control(PollDetach); err != nil {
		logger.Printf("NETPOLL: pollDesc detach operator failed: %v", err)
	}
}
