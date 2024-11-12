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
	"errors"
	"strings"
	"sync"
	"syscall"
	"time"
)

// newServer wrap listener into server, quit will be invoked when server exit.
func newServer(ln Listener, opts *options, onQuit func(err error)) *server {
	return &server{
		ln:     ln,
		opts:   opts,
		onQuit: onQuit,
	}
}

type server struct {
	operator    FDOperator
	ln          Listener
	opts        *options
	onQuit      func(err error)
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
		s.onQuit(err)
	}
	return err
}

// Close this server with deadline.
func (s *server) Close(ctx context.Context) error {
	s.operator.Control(PollDetach)
	s.ln.Close()

	for {
		activeConn := 0
		s.connections.Range(func(key, value interface{}) bool {
			conn, ok := value.(gracefulExit)
			if !ok || conn.isIdle() {
				value.(Connection).Close()
			} else {
				activeConn++
			}
			return true
		})
		if activeConn == 0 { // all connections have been closed
			return nil
		}

		// smart control graceful shutdown check internal
		// we should wait for more time if there are more active connections
		waitTime := time.Millisecond * time.Duration(activeConn)
		if waitTime > time.Second { // max wait time is 1000 ms
			waitTime = time.Millisecond * 1000
		} else if waitTime < time.Millisecond*50 { // min wait time is 50 ms
			waitTime = time.Millisecond * 50
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			continue
		}
	}
}

// OnRead implements FDOperator.
func (s *server) OnRead(p Poll) error {
	// accept socket
	conn, err := s.ln.Accept()
	if err == nil {
		if conn != nil {
			s.onAccept(conn.(Conn))
		}
		// EAGAIN | EWOULDBLOCK if conn and err both nil
		return nil
	}
	logger.Printf("NETPOLL: accept conn failed: %v", err)

	// delay accept when too many open files
	if isOutOfFdErr(err) {
		// since we use Epoll LT, we have to detach listener fd from epoll first
		// and re-register it when accept successfully or there is no available connection
		cerr := s.operator.Control(PollDetach)
		if cerr != nil {
			logger.Printf("NETPOLL: detach listener fd failed: %v", cerr)
			return err
		}
		go func() {
			retryTimes := []time.Duration{0, 10, 50, 100, 200, 500, 1000} // ms
			retryTimeIndex := 0
			for {
				if retryTimeIndex > 0 {
					time.Sleep(retryTimes[retryTimeIndex] * time.Millisecond)
				}
				conn, err := s.ln.Accept()
				if err == nil {
					if conn == nil {
						// recovery accept poll loop
						s.operator.Control(PollReadable)
						return
					}
					s.onAccept(conn.(Conn))
					logger.Println("NETPOLL: re-accept conn success:", conn.RemoteAddr())
					retryTimeIndex = 0
					continue
				}
				if retryTimeIndex+1 < len(retryTimes) {
					retryTimeIndex++
				}
				logger.Printf("NETPOLL: re-accept conn failed, err=[%s] and next retrytime=%dms", err.Error(), retryTimes[retryTimeIndex])
			}
		}()
	}

	// shut down
	if strings.Contains(err.Error(), "closed") {
		s.operator.Control(PollDetach)
		s.onQuit(err)
		return err
	}

	return err
}

// OnHup implements FDOperator.
func (s *server) OnHup(p Poll) error {
	s.onQuit(errors.New("listener close"))
	return nil
}

func (s *server) onAccept(conn Conn) {
	// store & register connection
	nconn := new(connection)
	nconn.init(conn, s.opts)
	if !nconn.IsActive() {
		return
	}
	fd := conn.Fd()
	nconn.AddCloseCallback(func(connection Connection) error {
		s.connections.Delete(fd)
		return nil
	})
	s.connections.Store(fd, nconn)

	// trigger onConnect asynchronously
	nconn.onConnect()
}

func isOutOfFdErr(err error) bool {
	se, ok := err.(syscall.Errno)
	return ok && (se == syscall.EMFILE || se == syscall.ENFILE)
}
