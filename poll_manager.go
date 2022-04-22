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
	"fmt"
	"log"
	"runtime"
)

func setNumLoops(numLoops int) error {
	return pollmanager.SetNumLoops(numLoops)
}

func setLoadBalance(lb LoadBalance) error {
	return pollmanager.SetLoadBalance(lb)
}

// manage all pollers
var pollmanager *manager

func init() {
	var loops = runtime.GOMAXPROCS(0)/20 + 1
	pollmanager = &manager{}
	pollmanager.SetLoadBalance(RoundRobin)
	pollmanager.SetNumLoops(loops)
}

// LoadBalance is used to do load balancing among multiple pollers.
// a single poller may not be optimal if the number of cores is large (40C+).
type manager struct {
	NumLoops int
	balance  loadbalance // load balancing method
	polls    []Poll      // all the polls
}

// SetNumLoops will return error when set numLoops < 1
func (m *manager) SetNumLoops(numLoops int) error {
	if numLoops < 1 {
		return fmt.Errorf("set invaild numLoops[%d]", numLoops)
	}

	if numLoops < m.NumLoops {
		// if less than, close the redundant pollers
		var polls = m.polls[:numLoops]
		for idx := numLoops; idx < m.NumLoops; idx++ {
			if err := m.polls[idx].Close(); err != nil {
				log.Printf("poller close failed: %v\n", err)
			}
		}
		m.NumLoops = numLoops
		m.polls = polls
	} else {
		// new poll to fill delta.
		m.NumLoops = numLoops
		for idx := len(m.polls); idx < m.NumLoops; idx++ {
			var poll = openPoll()
			m.polls = append(m.polls, poll)
			go poll.Wait()
		}
	}

	// LoadBalance must be set first
	m.balance.Rebalance(m.polls)
	return nil
}

// SetLoadBalance set load balance.
func (m *manager) SetLoadBalance(lb LoadBalance) error {
	if m.balance != nil && m.balance.LoadBalance() == lb {
		return nil
	}
	m.balance = newLoadbalance(lb, m.polls)
	return nil
}

// Close release all resources.
func (m *manager) Close() error {
	for _, poll := range m.polls {
		poll.Close()
	}
	m.NumLoops = 0
	m.balance = nil
	m.polls = nil
	return nil
}

// Pick will select the poller for use each time based on the LoadBalance.
func (m *manager) Pick() Poll {
	return m.balance.Pick()
}
