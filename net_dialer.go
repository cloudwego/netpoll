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
	"net"
	"time"
)

// DialConnection is a default implementation of Dialer.
func DialConnection(network, address string, timeout time.Duration) (connection Connection, err error) {
	return defaultDialer.DialConnection(network, address, timeout)
}

// NewDialer only support TCP and unix socket now.
func NewDialer() Dialer {
	return &dialer{}
}

var defaultDialer = NewDialer()

type dialer struct{}

// DialTimeout implements Dialer.
func (d *dialer) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	return d.DialConnection(network, address, timeout)
}

// DialConnection implements Dialer.
func (d *dialer) DialConnection(network, address string, timeout time.Duration) (connection Connection, err error) {
	ctx := context.Background()
	if timeout > 0 {
		subCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		ctx = subCtx
	}

	switch network {
	case "tcp", "tcp4", "tcp6":
		return d.dialTCP(ctx, network, address)
	// case "udp", "udp4", "udp6":  // TODO: unsupport now
	case "unix", "unixgram", "unixpacket":
		raddr := &UnixAddr{
			UnixAddr: net.UnixAddr{Name: address, Net: network},
		}
		return DialUnix(network, nil, raddr)
	default:
		return nil, net.UnknownNetworkError(network)
	}
}

func (d *dialer) dialTCP(ctx context.Context, network, address string) (connection *TCPConnection, err error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	var portnum int
	if portnum, err = net.DefaultResolver.LookupPort(ctx, network, port); err != nil {
		return nil, err
	}
	var ipaddrs []net.IPAddr
	// host maybe empty if address is ":1234"
	if host == "" {
		ipaddrs = []net.IPAddr{{}}
	} else {
		ipaddrs, err = net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ipaddrs) == 0 {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
	}

	var firstErr error // The error from the first address is most relevant.
	var tcpAddr = &TCPAddr{}
	for _, ipaddr := range ipaddrs {
		tcpAddr.IP = ipaddr.IP
		tcpAddr.Port = portnum
		tcpAddr.Zone = ipaddr.Zone
		if ipaddr.IP != nil && ipaddr.IP.To4() == nil {
			connection, err = DialTCP(ctx, "tcp6", nil, tcpAddr)
		} else {
			connection, err = DialTCP(ctx, "tcp", nil, tcpAddr)
		}
		if err == nil {
			return connection, nil
		}
		select {
		case <-ctx.Done(): // check timeout error
			return nil, err
		default:
		}
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr == nil {
		firstErr = &net.OpError{Op: "dial", Net: network, Source: nil, Addr: nil, Err: errMissingAddress}
	}
	return nil, firstErr
}

// sysDialer contains a Dial's parameters and configuration.
type sysDialer struct {
	net.Dialer
	network, address string
}
