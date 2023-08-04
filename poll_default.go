// Copyright 2023 CloudWeGo Authors
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

//go:build darwin || netbsd || freebsd || openbsd || dragonfly || linux
// +build darwin netbsd freebsd openbsd dragonfly linux

package netpoll

func (p *defaultPoll) Alloc() (operator *FDOperator) {
	op := p.opcache.alloc()
	op.poll = p
	return op
}

func (p *defaultPoll) Free(operator *FDOperator) {
	p.opcache.freeable(operator)
}

func (p *defaultPoll) appendHup(operator *FDOperator) {
	p.hups = append(p.hups, operator.OnHup)
	p.detach(operator)
	operator.done()
}

func (p *defaultPoll) detach(operator *FDOperator) {
	if err := operator.Control(PollDetach); err != nil {
		logger.Printf("NETPOLL: poller detach operator failed: %v", err)
	}
}

func (p *defaultPoll) onhups() {
	if len(p.hups) == 0 {
		return
	}
	hups := p.hups
	p.hups = nil
	go func(onhups []func(p Poll) error) {
		for i := range onhups {
			if onhups[i] != nil {
				onhups[i](p)
			}
		}
	}(hups)
}

// readall read all left data before close connection
func readall(op *FDOperator, br barrier) (total int, err error) {
	var bs = br.bs
	var ivs = br.ivs
	var n int
	for {
		bs = op.Inputs(br.bs)
		if len(bs) == 0 {
			return total, nil
		}

	TryRead:
		n, err = ioread(op.FD, bs, ivs)
		op.InputAck(n)
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			goto TryRead
		}
	}
}
