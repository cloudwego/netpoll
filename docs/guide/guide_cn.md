# 快速开始

本教程通过一些简单的 [示例][Examples] 帮助您开始使用 [Netpoll][Netpoll]，包括如何使用 [Server](#1-使用-sever)、[Client](#2-使用-dialer) 和 [nocopy API](#3-使用-nocopy-api)。

## 1. 使用 Sever

[这里][server-example] 是一个简单的 server 例子，接下来我们会解释它是如何构建的。

### 1.1 创建 Listener

首先我们需要一个 `Listener`，它可以是 `net.Listener` 或者 `netpoll.Listener`，两者都可以，依据你的代码情况自由选择。
创建 `Listener` 的过程如下：

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

或者

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

### 1.2 创建 EventLoop

`EventLoop` 是一个事件驱动的调度器，一个真正的 NIO Server，负责连接管理、事件调度等。

参数说明:

* `OnRequest` 是用户应该自己实现来处理业务逻辑的接口。 [注释][netpoll.go] 详细描述了它的行为。
* `Option` 用于自定义 `EventLoop` 创建时的配置，下面的例子展示了它的用法。更多详情请参考 [options][netpoll_options.go]。

创建过程如下：

```go
package main

import (
	"time"
	"github.com/cloudwego/netpoll"
)

var eventLoop netpoll.EventLoop

func main() {
	...
	eventLoop, _ = netpoll.NewEventLoop(
		handle,
		netpoll.WithOnPrepare(prepare),
		netpoll.WithReadTimeout(time.Second),
	)
	...
}
```

### 1.3 运行 Server

`EventLoop` 通过绑定 `Listener` 来提供服务，如下所示。`Serve` 方法为阻塞式调用，直到发生 `panic` 等错误，或者由用户主动调用 `Shutdown` 时触发退出。

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

### 1.4 关闭 Server

`EventLoop` 提供了 `Shutdown` 功能，用于优雅地停止服务器。用法如下：

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

## 2. 使用 Dialer

[Netpoll][Netpoll] 也支持在 Client 端使用，提供了 `Dialer`，类似于 `net.Dialer`。同样的，[这里][client-example] 展示了一个简单的 Client 端示例，接下来我们详细介绍一下：

### 2.1 快速方式

与 [Net][net] 类似，[Netpoll][Netpoll] 提供了几个用于直接建立连接的公共方法，可以直接调用。 如：

```go
DialConnection(network, address string, timeout time.Duration) (connection Connection, err error)

DialTCP(ctx context.Context, network string, laddr, raddr *TCPAddr) (*TCPConnection, error)

DialUnix(network string, laddr, raddr *UnixAddr) (*UnixConnection, error)
```

### 2.2 创建 Dialer

[Netpoll][Netpoll] 还定义了`Dialer` 接口。 用法如下：（通常推荐使用上一节的快速方式）

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

## 3. 使用 Nocopy API

`Connection` 提供了 Nocopy API —— `Reader` 和 `Writer`，以避免频繁复制。下面介绍一下它们的简单用法。

```go
package main

type Connection interface {
	// Recommended nocopy APIs
	Reader() Reader
	Writer() Writer
	... // see code comments for more details
}
```

### 3.1 简单用法

Nocopy API 设计为两步操作。

使用 `Reader` 时，通过 `Next`、`Peek`、`ReadString` 等方法读取数据后，还需要主动调用 `Release` 方法释放 buffer（`Nocopy` 读取 buffer 的原地址，所以您必须主动再次确认 buffer 已经不再使用）。

同样，使用 `Writer` 时，首先需要分配一个 `[]byte` 来写入数据，然后调用 `Flush` 确认所有数据都已经写入。`Writer` 还提供了丰富的 API 来分配 buffer，例如 `Malloc`、`WriteString` 等。

下面是一些简单的读写数据的例子。 更多详情请参考 [说明][nocopy.go]。

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

### 3.2 高阶用法

如果你想使用单个连接来发送（或接收）多组数据（如连接多路复用），那么你将面临数据打包和分包。在 [net][net] 上，这种工作一般都是通过复制来完成的。一个例子如下：

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

但是，[Netpoll][Netpoll] 不需要这样做，nocopy APIs 支持对 buffer 进行原地址操作（原地址组包和分包），并通过引用计数实现资源的自动回收和重用。

示例如下（使用方法 `Reader.Slice` 和 `Writer.Append`）：

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

# 常见用法

## 1. 如何配置 poller 的数量 ？

`NumLoops` 表示 [Netpoll][Netpoll] 创建的 `epoll` 的数量，默认已经根据P的数量自动调整(`runtime.GOMAXPROCS(0)`)，用户一般不需要关心。

但是如果你的服务有大量的 I/O，你可能需要如下配置：

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

## 2. 如何配置 poller 的连接负载均衡 ？

当 [Netpoll][Netpoll] 中有多个 poller 时，服务进程中的连接会负载均衡到每个 poller。

现在支持以下策略：

1. Random
   * 新连接将分配给随机选择的轮询器。
2. RoundRobin
   * 新连接将按顺序分配给轮询器。
     
[Netpoll][Netpoll] 默认使用 `RoundRobin`，用户可以通过以下方式更改：
     
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

## 3. 如何配置 [gopool][gopool] ？

[Netpoll][Netpoll] 默认使用 [gopool][gopool] 作为 goroutine 池来优化 `栈扩张` 问题（RPC 服务常见问题）。

[gopool][gopool] 项目中已经详细解释了如何自定义配置，这里不再赘述。

当然，如果你的项目没有 `栈扩张` 问题，建议最好关闭 [gopool][gopool]，关闭方式如下：

```go
package main

import (
	"github.com/cloudwego/netpoll"
)

func init() {
	netpoll.DisableGopool()
}
```

## 4. 如何初始化新的连接 ？

Client 和 Server 端通过不同的方式初始化新连接。

1. 在 Server 端，定义了 `OnPrepare` 来初始化新链接，同时支持返回一个 `context`，可以传递给后续的业务处理并复用。`WithOnPrepare` 提供方法注册。当 Server 接收新连接时，会自动执行注册的 `OnPrepare` 方法来完成准备工作。示例如下：

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

2. 在 Client 端，连接初始化需要由用户自行完成。 一般来说，`Dialer` 创建的新连接是可以由用户自行控制的，这与 Server 端被动接收连接不同。因此，用户不需要依赖触发器，可以自行初始化，如下所示：

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

## 5. 如何配置连接超时 ？

[Netpoll][Netpoll] 现在支持两种类型的超时配置：

1. 读超时（`ReadTimeout`）
   * 为了保持与 `net.Conn` 相同的操作风格，`Connection.Reader` 也被设计为阻塞读取。 所以提供了读取超时（`ReadTimeout`）。
   * 读超时（`ReadTimeout`）没有默认值（默认无限等待），可以通过 `Connection` 或 `EventLoop.Option` 进行配置，例如：

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

2. 空闲超时（`IdleTimeout`）
   * 空闲超时（`IdleTimeout`）利用 `TCP KeepAlive` 机制来踢出死连接并减少维护开销。使用 [Netpoll][Netpoll] 时，一般不需要频繁创建和关闭连接，所以通常来说，空闲连接影响不大。当连接长时间处于非活动状态时，为了防止出现假死、对端挂起、异常断开等造成的死连接，在空闲超时（`IdleTimeout`）后，netpoll 会主动关闭连接。
   * 空闲超时（`IdleTimeout`）的默认配置为 `10min`，可以通过 `Connection` API 或 `EventLoop.Option` 进行配置，例如：

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

## 6. 如何配置连接的读事件回调 ？

`OnRequest` 是指连接上发生读事件时 [Netpoll][Netpoll] 触发的回调。在 Server 端，在创建 `EventLoop` 时，可以注册一个`OnRequest`，在每次连接数据到达时触发，进行业务处理。Client端默认没有 `OnRequest`，需要时可以通过 API 设置。例如：

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

## 7. 如何配置连接的关闭回调 ？

`CloseCallback` 是指连接关闭时 [Netpoll][Netpoll] 触发的回调，用于在连接关闭后进行额外的处理。
[Netpoll][Netpoll] 能够感知连接状态。当连接被对端关闭或被自己清理时，会主动触发 `CloseCallback`，而不是由下一次调用 `Read` 或 `Write` 时返回错误（`net.Conn` 的方式）。
`Connection` 提供了添加 `CloseCallback` 的 API，已经添加的回调无法删除，支持多个回调。

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

# 注意事项

## 1. 错误设置 NumLoops

如果你的服务器运行在物理机上，Go 进程创建的 P 个数就等于机器的 CPU 核心数。 但是 Server 可能不会使用这么多核心。在这种情况下，过多的 poller 会导致性能下降。

这里提供了以下几种解决方案：

1. 使用 `taskset` 命令来限制 CPU 个数，例如：

```shell
taskset -c 0-3 $run_your_server
```

2. 主动设置 P 的个数，例如：

```go
package main

import (
	"runtime"
)

func init() {
	runtime.GOMAXPROCS(num_you_want)
}
```

3. 主动设置 poller 的个数，例如：

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
