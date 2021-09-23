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
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

// newServer wrap listener into server, quit will be invoked when server exit.
func newServer(ln Listener, opt *options, quit func(err error)) *server {
	return &server{
		ln:   ln,
		opt:  opt,
		quit: quit,
	}
}

type server struct {
	operator    FDOperator
	ln          Listener
	opt         *options
	quit        func(err error)
	connections sync.Map // key=fd, value=connection
}

// Run this server.
func (s *server) Run() (err error) {
	s.operator = FDOperator{
		FD:     s.ln.Fd(),
		OnRead: s.OnRead,
		OnHup:  s.OnHup,
	}
	s.operator.poll = pollmanager.Pick()
	err = s.operator.Control(PollReadable)
	if err != nil {
		s.quit(err)
	}
	return err
}

// Close this server with deadline.
func (s *server) Close(ctx context.Context) error {
	s.operator.Control(PollDetach)
	s.ln.Close()

	var conns []gracefulExit
	s.connections.Range(func(key, value interface{}) bool {
		var conn, ok = value.(gracefulExit)
		if ok && !conn.isIdle() {
			conns = append(conns, conn)
		} else {
			value.(Connection).Close()
		}
		return true
	})

	var ticker = time.NewTicker(time.Second)
	defer ticker.Stop()
	var count = len(conns) - 1
	for count >= 0 {
		for i := count; i >= 0; i-- {
			if conns[i].isIdle() {
				conns[i].Close()
				conns[i] = conns[count]
				count--
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			continue
		}
	}
	return nil
}

// OnRead implements FDOperator.
func (s *server) OnRead(p Poll) error {
	// accept socket
	conn, err := s.ln.Accept()
	if err != nil {
		// shut down
		if strings.Contains(err.Error(), "closed") {
			s.operator.Control(PollDetach)
			s.quit(err)
			return err
		}
		// async log
		go log.Println("accept conn failed:", err.Error())
		return err
	}
	if conn == nil {
		return nil
	}
	// store & register connection
	var c = &connection{}
	c.init(conn.(*netFD), s.opt)
	if !c.IsActive() {
		return nil
	}
	var fd = c.Fd()
	c.AddCloseCallback(func(connection Connection) error {
		s.connections.Delete(fd)
		return nil
	})
	s.connections.Store(fd, c)
	return nil
}

// OnHup implements FDOperator.
func (s *server) OnHup(p Poll) error {
	s.quit(errors.New("listener close"))
	return nil
}
