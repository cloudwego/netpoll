# Tutorial

This tutorial gets you started with [Netpoll][Netpoll] through some simple [examples][Examples], includes how to
use [Server](#1-use-sever), [Client](#2-use-dialer) and [nocopy APIs](#3-use-nocopy-api).

## 1. Use Sever

[Here][server-example] is a simple server demo, we will explain how it is constructed next.

### 1.1 Create Listener

First we need to get a `Listener`, it can be `net.Listener` or `netpoll.Listener`, which is no difference for server
usage. Create a `Listener` as shown below:

```go
package main

import "net"

func main() {
	listener, err := net.CreateListener(network, address)
	if err != nil {
		panic("create net listener failed")
	}
	...
}
```

or

```go
package main

import "github.com/cloudwego/netpoll"

func main() {
	listener, err := netpoll.CreateListener(network, address)
	if err != nil {
		panic("create netpoll listener failed")
	}
	...
}
```

### 1.2 New EventLoop

`EventLoop` is an event-driven scheduler, a real NIO Server, responsible for connection management, event scheduling,
etc.

params:

* `OnRequest` is an interface that users should implement by themselves to process business
  logic. [Code Comment][netpoll.go] describes its behavior in detail.
* `Option` is used to customize the configuration when creating `EventLoop`, and the following example shows its usage.
  For more details, please refer to [options][netpoll_options.go].

The creation process is as follows:

```go
package main

import (
	"time"
	"github.com/cloudwego/netpoll"
)

var eventLoop netpoll.EventLoop

func main() {
	...
	eventLoop, _ := netpoll.NewEventLoop(
		handle,
		netpoll.WithOnPrepare(prepare),
		netpoll.WithReadTimeout(time.Second),
	)
	...
}
```

### 1.3 Run Server

`EventLoop` provides services by binding `Listener`, as shown below.
`Serve` function will block until an error occurs, such as a panic or the user actively calls `Shutdown`.

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

var eventLoop netpoll.EventLoop

func main() {
	...
	// start listen loop ...
	eventLoop.Serve(listener)
}
```

### 1.4 Shutdown Server

`EventLoop` provides the `Shutdown` function, which is used to stop the server gracefully. The usage is as follows.

```go
package main

import (
	"context"
	"time"
	"github.com/cloudwego/netpoll"
)

var eventLoop netpoll.EventLoop

func main() {
	// stop server ...
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	eventLoop.Shutdown(ctx)
}
```

## 2. Use Dialer

[Netpoll][Netpoll] also has the ability to be used on the Client side. It provides `Dialer`, similar to `net.Dialer`.
Again, [here][client-example] is a simple client demo, and then we introduce it in detail.

### 2.1 The Fast Way

Similar to [Net][net], [Netpoll][Netpoll] provides several public functions for directly dialing a connection. such as:

```go
DialConnection(network, address string, timeout time.Duration) (connection Connection, err error)

DialTCP(ctx context.Context, network string, laddr, raddr *TCPAddr) (*TCPConnection, error)

DialUnix(network string, laddr, raddr *UnixAddr) (*UnixConnection, error)
```

### 2.2 Create Dialer

[Netpoll][Netpoll] also defines the `Dialer` interface. The usage is as follows:
(of course, you can usually use the fast way)

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

func main() {
	// Dial a connection with Dialer.
	dialer := netpoll.NewDialer()
	conn, err := dialer.DialConnection(network, address, timeout)
	if err != nil {
		panic("dial netpoll connection failed")
	}
	...
}
```

## 3. Use Nocopy API

`Connection` provides Nocopy APIs - `Reader` and `Writer`, to avoid frequent copying. Let’s introduce their simple
usage.

```go
package main

type Connection interface {
	// Recommended nocopy APIs
	Reader() Reader
	Writer() Writer
	... // see code comments for more details
}
```

### 3.1 Simple Usage

Nocopy APIs is designed as a two-step operation.

On `Reader`, after reading data through `Next`, `Peek`, `ReadString`, etc., you still have to actively call `Release` to
release the buffer(`Nocopy` reads the original address of the buffer, so you must take the initiative to confirm that
the buffer is no longer used).

Similarly, on `Writer`, you first need to allocate a buffer to write data, and then call `Flush` to confirm that all
data has been written.
`Writer` also provides rich APIs to allocate buffers, such as `Malloc`, `WriteString` and so on.

The following shows some simple examples of reading and writing data. For more details, please refer to
the [code comments][nocopy.go].

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

func main() {
	var conn netpoll.Connection
	var reader, writer = conn.Reader(), conn.Writer()
	
	// reading 
	buf, _ := reader.Next(n)
	... parse the read data ...
	reader.Release()
	
	// writing
	var write_data []byte
	... make the write data ...
	alloc, _ := writer.Malloc(len(write_data))
	copy(alloc, write_data) // write data 
	writer.Flush()
}
```

### 3.2 Advanced Usage

If you want to use the connection to send (or receive) multiple sets of data, then you will face the work of packing and
unpacking the data.

On [net][net], this kind of work is generally done by copying. An example is as follows:

```go
package main

import (
	"net"
)

func main() {
	var conn net.Conn
	var buf = make([]byte, 8192)
	
	// reading
	for {
		n, _ := conn.Read(buf)
		... unpacking & handling ...
		var i int
		for i = 0; i <= n-pkgsize; i += pkgsize {
			pkg := append([]byte{}, buf[i:i+pkgsize]...)
			go func() {
				... handling pkg ...
			}
		}
		buf = append(buf[:0], buf[i:n]...)
	}
	
	// writing
	var write_datas <-chan []byte
	... packing write ...
	for {
		pkg := <-write_datas
		conn.Write(pkg)
	}
}
```

But, this is not necessary in [Netpoll][Netpoll], nocopy APIs supports operations on the original address of the buffer,
and realizes automatic recycling and reuse of resources through reference counting.

Examples are as follows(use function `Reader.Slice` and `Writer.Append`):

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

func main() {
	var conn netpoll.Connection
	
	// reading
	reader := conn.Reader()
	for {
		... unpacking & handling ...
		pkg, _ := reader.Slice(pkgsize)
		go func() {
			... handling pkg ...
			pkg.Release()
		}
	}
	
	// writing
	var write_datas <-chan netpoll.Writer
	... packing write ...
	writer := conn.Writer()
	for {
		select {
		case pkg := <-write_datas:
			writer.Append(pkg)
		default:
			if writer.MallocLen() > 0 {
				writer.Flush()
			}
		}
	}
}
```

# How To

## 1. How to configure the number of pollers ?

`NumLoops` represents the number of `epoll` created by [Netpoll][Netpoll], which has been automatically adjusted
according to the number of P (`runtime.GOMAXPROCS(0)`) by default, and users generally don't need to care.

But if your service has heavy I/O, you may need the following configuration:

```go
package main

import (
	"runtime"
	"github.com/cloudwego/netpoll"
)

func init() {
	netpoll.SetNumLoops(runtime.GOMAXPROCS(0))
}
```

## 2. How to configure poller's connection loadbalance ?

When there are multiple pollers in [Netpoll][Netpoll], the connections in the service process will be loadbalanced to
each poller.

The following strategies are supported now:

1. Random
    * The new connection will be assigned to a randomly picked poller.
2. RoundRobin
    * The new connection will be assigned to the poller in order.

[Netpoll][Netpoll] uses `RoundRobin` by default, and users can change it in the following ways:

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

func init() {
	netpoll.SetLoadBalance(netpoll.Random)
	
	// or
	netpoll.SetLoadBalance(netpoll.RoundRobin)
}
```

## 3. How to configure [gopool][gopool] ?

[Netpoll][Netpoll] uses [gopool][gopool] as the goroutine pool by default to optimize the `stack growth` problem that
generally occurs in RPC services.

In the project [gopool][gopool], it explains how to change its configuration, so won't repeat it here.

Of course, if your project does not have a `stack growth` problem, it is best to close [gopool][gopool] as follows:

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

func init() {
	netpoll.DisableGopool()
}
```

## 4. How to prepare a new connection ?

There are different ways to prepare a new connection on the client and server.

1. On the server side, `OnPrepare` is defined to prepare for the new connection, and it also supports returning
   a `context`, which can be reused in subsequent business processing.
   `WithOnPrepare` provides this registration. When the server accepts a new connection, it will automatically execute
   the registered `OnPrepare` function to complete the preparation work. The example is as follows:

```go
package main

import (
	"context"
	"github.com/cloudwego/netpoll"
)

func main() {
	// register OnPrepare
	var onPrepare netpoll.OnPrepare = prepare
	evl, _ := netpoll.NewEventLoop(handler, netpoll.WithOnPrepare(onPrepare))
	...
}

func prepare(connection netpoll.Connection) (ctx context.Context) {
	... prepare connection ...
	return
}
```

2. On the client side, the connection preparation needs to be completed by the user. Generally speaking, the connection
   created by `Dialer` can be controlled by the user, which is different from passively accepting the connection on the
   server side. Therefore, the user not relying on the trigger, just prepare a new connection like this:

```go
package main

import (
	"context"
	"github.com/cloudwego/netpoll"
)

func main() {
	conn, err := netpoll.DialConnection(network, address, timeout)
	if err != nil {
		panic("dial netpoll connection failed")
	}
	... prepare here directly ...
	prepare(conn)
	...
}

func prepare(connection netpoll.Connection) (ctx context.Context) {
	... prepare connection ...
	return
}
```

## 5. How to configure connection timeout ?

[Netpoll][Netpoll] now supports two timeout configurations:

1. `Read Timeout`
    * In order to maintain the same operating style as `net.Conn`, `Connection.Reader` is also designed to block
      reading. So provide `Read Timeout`.
    * `Read Timeout` has no default value(wait infinitely), it can be configured via `Connection` or `EventLoop.Option`,
      for example:

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

func main() {
	var conn netpoll.Connection
	
	// 1. setting by Connection
	conn.SetReadTimeout(timeout)
	
	// or
	
	// 2. setting with Option 
	netpoll.NewEventLoop(handler, netpoll.WithReadTimeout(timeout))
	...
}
```

2. `Idle Timeout`
    * `Idle Timeout` utilizes the `TCP KeepAlive` mechanism to kick out dead connections and reduce maintenance
      overhead. When using [Netpoll][Netpoll], there is generally no need to create and close connections frequently,
      and idle connections have little effect. When the connection is inactive for a long time, in order to prevent dead
      connection caused by suspended animation, hang of the opposite end, abnormal disconnection, etc., the connection
      will be actively closed after the `Idle Timeout`.
    * The default minimum value of `Idle Timeout` is `10min`, which can be configured through `Connection` API
      or `EventLoop.Option`, for example:

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

func main() {
	var conn netpoll.Connection
	
	// 1. setting by Connection
	conn.SetIdleTimeout(timeout)
	
	// or
	
	// 2. setting with Option 
	netpoll.NewEventLoop(handler, netpoll.WithIdleTimeout(timeout))
	...
}
```

## 6. How to configure connection read event callback ?

`OnRequest` refers to the callback triggered by [Netpoll][Netpoll] when a read event occurs on the connection. On the
Server side, when creating the `EventLoop`, you can register an `OnRequest`, which will be triggered when each
connection data arrives and perform business processing. On the Client side, there is no `OnRequest` by default, and it
can be set via API when needed. E.g:

```go
package main

import (
	"context"
	"github.com/cloudwego/netpoll"
)

func main() {
	var onRequest netpoll.OnRequest = handler
	
	// 1. on server side
	evl, _ := netpoll.NewEventLoop(onRequest, opts...)
	...
	
	// 2. on client side
	conn, _ := netpoll.DialConnection(network, address, timeout)
	conn.SetOnRequest(handler)
	...
}

func handler(ctx context.Context, connection netpoll.Connection) (err error) {
	... handling ...
	return nil
}
```

## 7. How to configure the connection close callback ?

`CloseCallback` refers to the callback triggered by [Netpoll][Netpoll] when the connection is closed, which is used to
perform additional processing after the connection is closed.
[Netpoll][Netpoll] is able to perceive the connection status. When the connection is closed by peer or cleaned up by
self, it will actively trigger `CloseCallback` instead of returning an error on the next `Read` or `Write`(the way
of `net.Conn`).
`Connection` provides API for adding `CloseCallback`, callbacks that have been added cannot be removed, and multiple
callbacks are supported.

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

func main() {
	var conn netpoll.Connection
	
	// add close callback
	var cb netpoll.CloseCallback = callback
	conn.AddCloseCallback(cb)
	...
}

func callback(connection netpoll.Connection) error {
	return nil
}
```

# Attention

## 1. Wrong setting of NumLoops

If your server is running on a physical machine, the number of P created by the Go process is equal to the number of
CPUs of the machine. But the server may not use so many cores. In this case, too many pollers will cause performance
degradation.

There are several solutions:

1. Use the `taskset` command to limit CPU usage, such as:

```shell
taskset -c 0-3 $run_your_server
```

2. Actively set the number of P, for instance:

```go
package main

import (
	"runtime"
)

func init() {
	runtime.GOMAXPROCS(num_you_want)
}
```

3. Actively set the number of pollers, e.g:

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

func init() {
	netpoll.SetNumLoops(num_you_want)
}
```

[Netpoll]: https://github.com/cloudwego/netpoll

[net]: https://github.com/golang/go/tree/master/src/net

[gopool]: https://github.com/bytedance/gopkg/tree/develop/util/gopool

[Examples]: https://github.com/cloudwego/netpoll-examples

[server-example]: https://github.com/cloudwego/netpoll-examples/blob/main/server.go

[client-example]: https://github.com/cloudwego/netpoll-examples/blob/main/client.go

[netpoll.go]: https://github.com/cloudwego/netpoll/blob/main/netpoll.go

[netpoll_options.go]: https://github.com/cloudwego/netpoll/blob/main/netpoll_options.go

[nocopy.go]: https://github.com/cloudwego/netpoll/blob/main/nocopy.go
