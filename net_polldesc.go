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
	"sync"
)

// TODO: recycle *pollDesc
func newPollDesc(fd int) *pollDesc {
	pd, op := &pollDesc{}, &FDOperator{}
	op.FD = fd
	op.OnWrite = pd.onwrite
	op.OnHup = pd.onhup

	pd.operator = op
	pd.writeTrigger = make(chan struct{})
	pd.closeTrigger = make(chan struct{})
	return pd
}

type pollDesc struct {
	once     sync.Once
	operator *FDOperator

	// The write event is OneShot, then mark the writable to skip duplicate calling.
	writeTrigger chan struct{}
	closeTrigger chan struct{}
}

// WaitWrite .
// TODO: implement - poll support timeout hung up.
func (pd *pollDesc) WaitWrite(ctx context.Context) error {
	var err error
	pd.once.Do(func() {
		// add ET|Write|Hup
		pd.operator.poll = pollmanager.Pick()
		err = pd.operator.Control(PollWritable)
		if err != nil {
			pd.detach()
		}
	})
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		pd.detach()
		return mapErr(ctx.Err())
	case <-pd.closeTrigger:
		return Exception(ErrConnClosed, "by peer")
	case <-pd.writeTrigger:
		// if writable, check hup by select
		select {
		case <-pd.closeTrigger:
			return Exception(ErrConnClosed, "by peer")
		default:
			return nil
		}
	}
}

func (pd *pollDesc) onwrite(p Poll) error {
	select {
	case <-pd.writeTrigger:
	default:
		close(pd.writeTrigger)
	}
	return nil
}

func (pd *pollDesc) onhup(p Poll) error {
	close(pd.closeTrigger)
	return nil
}

func (pd *pollDesc) detach() {
	pd.operator.Control(PollDetach)
	pd.operator.unused()
}
