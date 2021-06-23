[English](README.md)

# 简介

[Netpoll](https://github.com/cloudwego/netpoll) 是由 [字节跳动](https://www.bytedance.com) 开发的高性能 NIO(Non-blocking I/O)
网络库，专注于 RPC 场景。

RPC 通常有较重的处理逻辑，因此无法串行处理 I/O。而 Go 的标准库 [net](https://github.com/golang/go/tree/master/src/net) 设计了 BIO(Blocking I/O) 模式的
API，使得 RPC 框架设计上只能为每个连接都分配一个 goroutine。 这在高并发下，会产生大量的
goroutine，大幅增加调度开销。此外，[net.Conn](https://github.com/golang/go/blob/master/src/net/net.go) 没有提供检查连接活性的 API，因此 RPC
框架很难设计出高效的连接池，池中的失效连接无法及时清理。

另一方面，开源社区目前缺少专注于 RPC 方案的 Go 网络库。类似的项目如：[evio](https://github.com/tidwall/evio)
, [gnet](https://github.com/panjf2000/gnet) 等，均面向 [Redis](https://redis.io), [Haproxy](http://www.haproxy.org) 这样的场景。

因此 [Netpoll](https://github.com/cloudwego/netpoll) 应运而生，它借鉴了 [evio](https://github.com/tidwall/evio)
和 [netty](https://github.com/netty/netty) 的优秀设计，具有出色的 [性能](#性能)，更适用于微服务架构。
同时，[Netpoll](https://github.com/cloudwego/netpoll) 还提供了一些 [特性](#特性)，推荐在 RPC 设计中替代
[net](https://github.com/golang/go/tree/master/src/net) 。

基于 [Netpoll](https://github.com/cloudwego/netpoll) 开发的 RPC 框架 [KiteX](https://github.com/cloudwego/kitex) 和 HTTP
框架 [Hertz](https://github.com/cloudwego/hertz) (即将开源)，性能均业界领先。

[范例](https://github.com/cloudwego/netpoll-benchmark) 展示了如何使用 [Netpoll](https://github.com/cloudwego/netpoll)
构建 RPC Client 和 Server。

更多信息请参阅 [文档](#文档)。

# 特性

* **已经支持**
    - [LinkBuffer](nocopy_linkbuffer.go) 提供可以流式读写的 nocopy API
    - [gopool](https://github.com/bytedance/gopkg/util/gopool) 提供高性能的 goroutine 池
    - [mcache](https://github.com/bytedance/gopkg/lang/mcache) 提供高效的内存复用
    - [multisyscall](https://github.com/cloudwego/multisyscall) 支持批量系统调用
    - `IsActive` 支持检查连接是否存活
    - `Dialer` 支持构建 client
    - `EventLoop` 支持构建 server
    - 支持 TCP，Unix Domain Socket
    - 支持 Linux，Mac OS（操作系统）

* **即将开源**
    - [io_uring](https://github.com/axboe/liburing)
    - Shared Memory IPC
    - 串行调度 I/O，适用于纯计算
    - 支持 TLS
    - 支持 UDP

* **不被支持**
    - Windows（操作系统）

# 性能

性能测试并非数字游戏，首先应满足工业级使用要求，在 RPC 场景下，并发请求、等待超时是必要的支持项。

因此我们设定，性能测试应满足如下条件:

1. 支持并发请求, 支持超时(1s)
2. 使用协议: header(4 byte) 表明总长

对比项目为 [net](https://github.com/golang/go/tree/master/src/net), [evio](https://github.com/tidwall/evio)
, [gnet](https://github.com/panjf2000/gnet) ，我们通过 [测试代码](https://github.com/cloudwego/netpoll-benchmark) 比较了它们的性能。

更多测试参考 [Netpoll-Benchmark](https://github.com/cloudwego/netpoll-benchmark)
, [KiteX-Benchmark](https://github.com/cloudwego/kitex) 和 [Hertz-Benchmark](https://github.com/cloudwego/hertz)

### 测试环境

* CPU:    Intel(R) Xeon(R) Gold 5118 CPU @ 2.30GHz, 4 cores
* Memory: 8GB
* OS:     Debian 5.4.56.bsk.1-amd64 x86_64 GNU/Linux
* Go:     1.15.4

### 并发表现 (echo 1KB)

![image](docs/images/c_tp99.png)
![image](docs/images/c_qps.png)

### 传输表现 (并发 100)

![image](docs/images/s_tp99.png)
![image](docs/images/s_qps.png)

### 测试结论

相比 [net](https://github.com/golang/go/tree/master/src/net) ，[Netpoll](https://github.com/cloudwego/netpoll) 延迟约 34%，qps
约 110%（继续加压 net 延迟过高，数据失真）

# 文档

* [使用文档](docs/guide/usage.md)
* [设计文档](docs/reference/design.md)
* [Change Log](docs/reference/change_log.md)
* [DATA RACE 说明](docs/reference/explain.md)