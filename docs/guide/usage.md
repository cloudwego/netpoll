# 1. Introduction

[Netpoll](https://github.com/cloudwego/netpoll) 底层使用 epoll 管理连接，连接读写均为 epoll 事件驱动。 提供了 zero-copy 读写操作的能力，提升效率并降低内存和 gc
开销。 同时 [Netpoll](https://github.com/cloudwego/netpoll) 附带 NIO Server 实现，支持快速创建高性能 NIO server。

# 2. Tutorial

## 2.1 编写 NIO Server

现在我们开始编写一个 server 吧，首先我们先给出一个完整的 server 范例，然后分解其编写逻辑。

```go
package main

import (
	"context"
	"time"
	"github.com/cloudwego/netpoll"
)

func main() {
	network, address := "tcp", "127.0.0.1:8888"
	
	// 创建 listener
	listener, err := netpoll.CreateListener(network, address)
	if err != nil {
		panic("create netpoll listener fail")
	}
	
	// handle: 连接读数据和处理逻辑
	var onRequest netpoll.OnRequest = handler
	
	// options: EventLoop 初始化自定义配置项
	var opts = []netpoll.Option{
		netpoll.WithReadTimeout(1 * time.Second),
		netpoll.WithIdleTimeout(10 * time.Minute),
		netpoll.WithOnPrepare(nil),
	}
	
	// 创建 EventLoop
	eventLoop, err := netpoll.NewEventLoop(onRequest, opts...)
	if err != nil {
		panic("create netpoll event-loop fail")
	}
	
	// 运行 Server
	err = eventLoop.Serve(listener)
	if err != nil {
		panic("netpoll server exit")
	}
}

// 读事件处理
func handler(ctx context.Context, connection netpoll.Connection) error {
	return connection.Writer().Flush()
}
```

### 2.1.1 创建 Listener

首先我们先创建一个 `netpoll.Listener`，和 `net.Listener` 创建方式相似，通过 `network` 和 `address` 构建。

```go
listener, err := netpoll.CreateListener(network, address)
if err != nil {
    panic("create netpoll listener fail")
}
```

### 2.1.2 创建 EventLoop

`EventLoop` 是 NIO Server 的事件驱动调度器，负责连接管理、事件调度等。创建过程如下

```go
// handle: 连接读数据和处理逻辑
var handle netpoll.OnRequest

// options: EventLoop 初始化自定义配置项
var opts = []netpoll.Option{
    netpoll.WithReadTimeout(1 * time.Second),
    netpoll.WithIdleTimeout(10 * time.Minute),
    netpoll.WithOnPrepare(nil),
    ...
}

// 创建 EventLoop    
eventLoop, err := netpoll.NewEventLoop(handle, opts...)
if err != nil {
    panic("create netpoll event-loop fail")
}
```

### 2.1.3 运行 Server

`EventLoop` 通过绑定 `Listener` 来提供对外服务，范例如下。 其中 `Serve()` 方法只在异常下退出，如 panic 异常中断，或者用户主动关闭 server

```go
// 运行 Server
err = eventLoop.Serve(listener)
if err != nil {
    panic("netpoll server exit")
}
```

### 2.1.4 关闭 Server

Server 允许主动关闭退出，关闭阶段支持优雅退出（处理完正在执行的连接事件）。

```go
// 关闭 Server
eventLoop.Shutdown()
```

## 2.2 使用/编写 Dialer

[Netpoll](https://github.com/cloudwego/netpoll) 同时具备在 Client 端使用的能力，通过提供 `Dialer` 的方式，与 `net.Dialer` 相似。同样我们先给出完整使用范例。

```go
package main

import (
	"time"
	"github.com/cloudwego/netpoll"
)

func main() {
	network, address := "tcp", "127.0.0.1:8888"
	
	// 直接创建连接
	conn, err := netpoll.DialConnection(network, address, 50*time.Millisecond)
	if err != nil {
		panic("dial netpoll connection fail")
	}
	
	// 通过 dialer 创建连接
	dialer := netpoll.NewDialer()
	conn, err = dialer.DialConnection(network, address, 50*time.Millisecond)
	if err != nil {
		panic("dialer netpoll connection fail")
	}
	
	// conn write & flush message
	conn.Writer().WriteBinary([]byte("hello world"))
	conn.Writer().Flush()
}
```

### 2.2.1 缺省方式创建连接

[Netpoll](https://github.com/cloudwego/netpoll) 提供了快速建立连接的 API，如下所示，缺省配置项，适合绝大多数常规需求。

```go
// 创建任意连接
DialConnection(network, address string, timeout time.Duration) (connection Connection, err error)
// 创建 TCP 连接
DialTCP(ctx context.Context, network string, laddr, raddr *TCPAddr) (*TCPConnection, error)
// 创建 Unix 连接
DialUnix(network string, laddr, raddr *UnixAddr) (*UnixConnection, error)
```

### 2.2.2 创建 Dialer

[Netpoll](https://github.com/cloudwego/netpoll) 也支持通过 `Dialer` 对象创建连接，支持可扩展的自定义配置（目前尚未开放）。 使用方式如下

```go
// 通过 dialer 创建连接
dialer := netpoll.NewDialer()
conn, err = dialer.DialConnection(network, address, 50*time.Millisecond)
if err != nil {
    panic("dialer netpoll connection fail")
}
```

## 2.3 使用 Connection

`Connection` 专为 NIO 设计，提供了强大的 zero-copy 读写能力。相比 `net.Conn` 性能更高，内存和 gc 开销更小。（同时也实现了 `net.Conn`，但不推荐使用）。

API 定义如下进行分类和说明，配置相关部分详见 [How To](#3-how-to)，以下仅介绍 zero-copy 使用。

```go
type Connection interface {
   net.Conn // API 对齐，不推荐使用

   // 推荐使用的 zero-copy API
   Reader() Reader
   Writer() Writer

   ... // 更多参见注释部分
```

### 2.3.1 使用 zero-copy API

推荐使用 `Connection` 的零拷贝 API 来处理连接读写，其使用说明如下（更多说明参见代码注释）

```go
// 读取 n 字节, 返回底层缓存切片, 同时缓存减少 n 字节
conn.Reader().Next(n)
// 预读取 n 字节, 返回底层缓存切片, 缓存大小不变, 可重复预读
conn.Reader().Peek(n)
// 丢弃缓存最前的 n 字节, 不可找回
conn.Reader().Skip(n)
// 释放已读部分的底层缓存, (在此之前读取的)上层读缓存切片将全部失效
conn.Reader().Release()

// 在连接写缓存区顺序分配 n 字节
conn.Writer().Malloc(n)
// 将已分配的写缓存全部发送到连接对端, (在此之前分配的)上层写缓存切片将全部失效
conn.Writer().Flush()
```

### 2.3.2 继承 zero-copy 能力

连接提供了一些高级能力，不仅可以在连接上做零拷贝读写，而且还可以将零拷贝读写能力传递下去。 

我们开发了一种 `LinkBuffer`，不仅支持 zero-copy API，同时还支持 zero-copy 读写自身类型的分片。 
通过 `Slice/Append` 接口，`LinkBuffer` 支持逻辑上的任意切分和拼接，实际仅基于同一个底层缓存，切分和拼接的过程是 zero-copy 的。 
我们在连接层面也提供了 `Slice/Append` 接口，可以读写整个的 `LinkBuffer` 分片。
使得上层逻辑可以基于 `LinkBuffer` 分片继续 zero-copy 读写。 范例代码如下：

```go
// 读取 n 字节 LinkBuffer 切片
Slice(n int) (r Reader, err error)
// 拼接(写) LinkBuffer 切片
Append(w Writer) (n int, err error)
```

```go
// 持续继承 zero-copy 读写
buf1, _ := conn.Reader().Slice(n1)
buf2, _ := buf1.Slice(n2)
buf1.Append(buf2)
conn.Writer().Append(buf1)
```

# 3. How To

## 3.1 如何配置 poller 个数

`NumLoops` 是 [Netpoll](https://github.com/cloudwego/netpoll) 底层的 epoll 线程数量。
实践数据表明，单个 poller 大约可以负载 20core CPU，[Netpoll](https://github.com/cloudwego/netpoll) 默认已经根据 `runtime.GOMAXPROCS(0)` 
动态调整了 poller 数量，一般用户不需要关心。 如果想自行调整，可以通过如下方式配置

```go
// 设置合适的 poller 数量
netpoll.SetNumLoops(num_you_want)
```

## 3.2 如何配置 poller 连接负载均衡策略

一般情况下，[Netpoll](https://github.com/cloudwego/netpoll) 底层存在多个 poller，整个进程里的所有连接，会通过负载均衡策略分配给各个 poller 维护和调度。
目前共支持以下负载均衡策略：

1. Random
   * 新建立的连接将被简单随机地，分配给任意一个 poller
2. RoundRobin
   * 新建立的连接将被循环式的，依次分配给有序排列的 poller Netpoll 默认使用 RoundRobin 策略，用户可以通过以下方式自定义改变该策略

```go
// 负载均衡策略设置
netpoll.SetLoadBalance(netpoll.Random)
// or
netpoll.SetLoadBalance(netpoll.RoundRobin)
```

## 3.3 如何配置 gopool 协程池

[Netpoll](https://github.com/cloudwego/netpoll) 默认开启 [gopool](https://github.com/bytedance/gopkg/util/gopool) 协程池，
因为基于 epoll 的读写事件调度模式（多 worker 协作），特别适合使用 [gopool](https://github.com/bytedance/gopkg/util/gopool) 。 
目前尚不支持配置 [gopool](https://github.com/bytedance/gopkg/util/gopool) ，后续会考虑开放这部分能力。

## 3.4 如何初始化新连接

Server 端和 Client 端通过不同的方式初始化新建立的连接。

1. 在 Server 端，定义了 `OnPrepare` 用于自定义初始化连接，同时支持初始化一个 `context`，提供给后续的读事件处理时重复使用。
   `OnPrepare` 需要在创建 `EventLoop` 时，通过 `option` `WithOnPrepare` 注入。 
   Server 端在 `Accept` 新连接时，会自动调度执行注册的 `OnPrepare` 方法，完成连接初始化工作，代码范例如下。

```go
package main

// EventLoop 注册连接初始化逻辑 范例
func InitEventLoop() {
	// handle: 连接读数据和处理逻辑
	var onRequest netpoll.OnRequest = handler
	// prepare: 连接初始化, 返回读事件处理时使用的 context
	var onPrepare netpoll.OnPrepare = prepare
	// 创建 EventLoop 时, 注册 OnPrepare
	eventLoop, err := netpoll.NewEventLoop(onRequest, netpoll.WithOnPrepare(onPrepare))
	if err != nil {
		panic("create netpoll event-loop fail")
	}
}

// 连接初始化
func prepare(connection netpoll.Connection) context.Context {
	return context.Background()
}

// 读事件处理
func handler(ctx context.Context, connection netpoll.Connection) error {
	return connection.Writer().Flush()
}
```

2. 在 Client 端，连接初始化需要自行额外完成。一般认为，当通过 `Dialer` 创建新的连接后，不存在需要连接来感知的初始化工作，
   因此这部分(初始化)工作由上层逻辑完成，最后在需要时注册读事件回调即可（参见 [How To - 3.6 如何配置连接读事件回调](#36-如何配置连接读事件回调)）

## 3.5 如何配置连接超时

目前支持两种超时配置

1. 连接异步读超时 `read timeout`
    * 为了保持和 `net.Conn` 相同的操作风格，`Connection` 在读数据是也是阻塞读取的，允许使用 `conn.Reader().Next(n)` 的方式阻塞读取足额的 n 字节。
      而由于 [Netpoll](https://github.com/cloudwego/netpoll) 是异步回调模型，连接读等待唤醒取决于对端是否返回了数据，并且读事件被调度。
      因此这里支持读阻塞到指定超时时间后主动返回。
    * `read timeout` 没有默认值(无限等待)，可以通过 `Connection` API 或者 `EventLoop` `option` 配置

```go
// option 方式
netpoll.WithReadTimeout(1 * time.Second)
// api 方式
connection.SetReadTimeout(1 * time.Second)
```

2. 连接空闲超时 `idle timeout`
    * 空闲超时即 `TCP KeepAlive` 机制，目的是踢出死连，降低 [Netpoll](https://github.com/cloudwego/netpoll) 维护的开销。
      在使用 [Netpoll](https://github.com/cloudwego/netpoll) 时，一般来说不需要频繁的创建和关闭连接，
      不用考虑未使用的连接会造成额外开销（基于 epoll 的事件调度机制，对于无事件连接不会被调度）。
      （当然，epoll 监听不活跃的连接也会有一定的额外开销）当连接长时间不活跃时，为防止假死、对端 hung 住、对端异常掉线 等情况导致的死连接，
      [Netpoll](https://github.com/cloudwego/netpoll) 会在连接一定空闲时间后主动关闭。
    * `idle timeout` 系统默认最小值为 `10min`，可以通过 `Connection` API 或者 `EventLoop` `option` 配置

```go
// option 方式
netpoll.WithIdleTimeout(1 * time.Second)
// api 方式
connection.SetIdleTimeout(1 * time.Second)
```

## 3.6 如何配置连接读事件回调

读事件回调 `OnRequest` 是指，连接在底层读事件到来时，由 [Netpoll](https://github.com/cloudwego/netpoll) 底层调度触发的回调。
该回调是以 NIO 的方式读取和处理连接上的数据。 在 Server 端，创建 `EventLoop` 时强制需要 `OnRequest`，并在每个连接数据到来时触发，用于执行 server 业务逻辑。 
在 Client 端，默认没有读事件回调，支持在需要时通过 API 设置

```go
// handle: 连接读数据和处理逻辑
var onRequest netpoll.OnRequest = handler
// Server 端
eventLoop, err := netpoll.NewEventLoop(onRequest, opts...)
// Client 端
connection.SetOnRequest(onRequest)
```

## 3.7 如何配置连接关闭回调

连接关闭回调 `CloseCallback` 是指，连接在被关闭时，由 [Netpoll](https://github.com/cloudwego/netpoll) 底层调度触发的回调。
该回调用以在连接被关闭后，执行额外的处理逻辑。 [Netpoll](https://github.com/cloudwego/netpoll) 能够感知连接状态，当连接对端关闭、清理死连
等情况下，底层会主动触发连接关闭，此时 `CloseCallback` 起到通知的作用。触发主动的处理连接关闭，而不是在下一次读写连接时报错（`net.Conn` 的做法）。
`Connection` 提供了 API 用于添加 `CloseCallback`，已被添加的回调不可以移除，支持添加多个回调。

```go
// 添加 CloseCallback 范例
func addCloseCallback() {
   // 回调方法定义
   var cb netpoll.CloseCallback = callback
   // 添加回调
   conn.AddCloseCallback(cb)
}

func callback(connection netpoll.Connection) error {
    return nil
}
```

## 3.8 如何使用 LinkBuffer

[Netpoll](https://github.com/cloudwego/netpoll) 提供的 `LinkBuffer` 支持并发的单个读和单个写操作，有较小的 atomic 锁开销，是一种效率高、内存开销小的 buffer。

# 4. Attention

## 4.1 错误设置 NumLoops

`NumLoops` 是 [Netpoll](https://github.com/cloudwego/netpoll) 底层的 poller 线程数量。
实践数据表明，单个 poller 大约可以负载 20core CPU，[Netpoll](https://github.com/cloudwego/netpoll) 默认已经根据 `runtime.GOMAXPROCS(0)` 动态调整了 poller 数量，一般用户不需要关心。 
但对于物理机部署来说，`runtime.GOMAXPROCS(0)` 拿到的是物理机核心数，可能会导致性能下降。 解决办法有以下几种：

1. 使用 `taskset` 命令来限制 CPU 的使用

```shell
taskset 0-3 ./output/bootstrap.sh
```

2. 主动设置 P 的数量

```go
func init() {
    runtime.GOMAXPROCS(num_you_want)
}
```

3. 主动设置 poller 数量

```go
func init() {
    netpoll.SetNumLoops(num_you_want)
}
```