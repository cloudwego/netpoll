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

package netpoll

import (
	"errors"
	"time"
)

// TODO: recycle *pollDesc
func newPollDesc(fd int) *pollDesc {
	pd, op := &pollDesc{}, &FDOperator{}
	pd.writeTicker = make(chan error, 1)

	op.FD = fd
	op.OnWrite = func(p Poll) error {
		select {
		case pd.writeTicker <- nil:
		default:
		}
		return nil
	}
	op.OnHup = func(p Poll) error {
		select {
		case pd.writeTicker <- Exception(ErrConnClosed, "by peer"):
		default:
		}
		return nil
	}
	pd.operator = op
	return pd
}

type pollDesc struct {
	operator *FDOperator
	// The write event is OneShot, then mark the writable to skip duplicate calling.
	writable    bool
	writeTicker chan error
}

// WaitWrite
// TODO: implement - poll support timeout hung up.
func (pd *pollDesc) WaitWrite(deadline time.Time) (err error) {
	// if writable, check hup by select
	if pd.writable {
		select {
		case err = <-pd.writeTicker:
			return err
		default:
			return nil
		}
	}
	// calling first time
	if deadline.IsZero() {
		return Exception(ErrDialNoDeadline, "")
	}
	dur := time.Until(deadline)
	if dur <= 0 {
		return Exception(ErrDialTimeout, dur.String())
	}
	// add ET|Write|Hup
	pd.operator.poll = pollmanager.Pick()
	err = pd.operator.Control(PollWritable)
	if err != nil {
		pd.operator.Control(PollDetach)
		return err
	}
	// add timeout trigger
	cancel := time.AfterFunc(dur, func() {
		select {
		case pd.writeTicker <- Exception(ErrDialTimeout, dur.String()):
		default:
		}
	})
	// wait
	if err = <-pd.writeTicker; err == nil {
		pd.writable = true
	} else {
		if errors.Is(err, ErrDialTimeout) {
			pd.operator.Control(PollDetach)
		}
	}
	cancel.Stop()
	return err
}
