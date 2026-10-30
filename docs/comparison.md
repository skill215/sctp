# Go SCTP 库对比

比较日期：2026-10-29。

`skill215/sctp` 适合需要延续 `ishidawataru/sctp` 调用方式、使用 Linux 原生 SCTP，且能接受 Go 1.26 最低要求的项目。这个 fork 的主要优势是底层正确性修复和回归验证；高层网络 API 的完整性仍落后于 `georgeyanev/go-sctp`。对于没有兼容包袱的新项目，后者值得并行验证；对于 WebRTC DataChannel，应优先考虑 Pion 生态。

## 实现方式与适用场景

这些库分为两类：

- **内核 SCTP 封装**：本库、`ishidawataru/sctp`、`free5gc/sctp` 和 `georgeyanev/go-sctp`。Go 代码调用 Linux socket API，重传、拥塞控制和路径管理由内核负责。应用及部署环境需要内核 SCTP 支持。
- **用户态 SCTP 协议栈**：`pion/sctp`。协议逻辑在 Go 中实现，由调用方提供底层 `NetConn`，主要用于 WebRTC。WebRTC 中通常由上层提供 DTLS 传输；`pion/sctp` 本身不包办 ICE 和 DTLS，也不能直接替换本库的 Linux SCTP socket API。[Pion 配置源码](https://github.com/pion/sctp/blob/6ec762ebc1106ae8c5ce1a69208a98dfcfdddaaf/association.go#L407-L444)

“不使用 cgo”与“自行实现协议栈”是不同属性。本库不使用 cgo，但仍依赖 Linux 内核实现 SCTP。选择时应先确定需要原生 SCTP socket，还是由应用提供传输通道的用户态协议栈。

| 库 | 实现及定位 | 主要优势 | 主要限制 |
| --- | --- | --- | --- |
| `skill215/sctp` | Linux 内核封装，延续上游主要 API | fd 生命周期、poller、超时与 ABI 回归覆盖；零外部 Go 模块依赖 | Linux/386 不支持；缺少标准 deadline、Context 拨号与接收 flags |
| `ishidawataru/sctp` | 本库的上游，Linux 内核封装 | API 简单；支持较旧的 Go 版本 | 底层实现较旧；RawConn 的 Read 和 Write 回调仍会 panic |
| `free5gc/sctp` | 同源的 Linux 内核封装，增加电信场景配置 | RTO、关联参数和 MaxSeg 配置；部分类型化通知 | Deadline 仍不支持；`SCTPRead` 签名与本库不同 |
| `georgeyanev/go-sctp` | 更接近 Go 标准库 API 的 Linux 内核封装 | Context、deadline、接收 flags、事件解析、动态绑定及更多 socket 配置 | 仍依赖 Linux；迁移需要调整 API |
| `pion/sctp` | 面向 WebRTC 的 Go 用户态协议栈 | 不依赖内核 SCTP；跨平台；独立 Stream 对象与部分可靠性 API | 需要提供底层传输通道；与内核 socket API 的模型不同 |

上游和 free5GC 的 RawConn 行为分别见其 [上游实现](https://github.com/ishidawataru/sctp/blob/19ddcbc6aae2b602d6384cc4048e5f91f0e84e5d/sctp_linux.go#L64-L87)与 [free5GC 实现](https://github.com/free5gc/sctp/blob/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd/sctp_linux.go#L67-L82)。free5GC 的配置和通知见 [SocketConfig](https://github.com/free5gc/sctp/blob/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd/sctp.go#L952-L986) 与 [Notification](https://github.com/free5gc/sctp/blob/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd/sctp_notification.go)。go-sctp 的能力见其 [连接 API](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/sctp_conn.go)和 [Dialer](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/dial.go)。

## 本库的优势

### 延续上游 API

本库保留了 `DialSCTP`、`ListenSCTP`、`SCTPRead`、`SCTPWrite`、`SndRcvInfo` 和 wrapped connection 等主要接口。已有 ishidawataru 项目迁移时，可以延续原有调用结构，再核对 Go 版本要求、fd 所有权、超时与 PPID 等行为变化。[本库 API](https://github.com/skill215/sctp/blob/bd2fa276b5be3bdebef3a4a9756cae7567505359/sctp.go)

这与直接切换其他库的成本不同。例如，free5GC 的 `SCTPRead` 增加了通知返回值；go-sctp 使用 `ReadMsg`、`SndInfo` 和 `RcvInfo` 等接口；Pion 则使用 Association 和 Stream 模型。它们都需要做调用与语义适配。

### 底层修复有回归测试

本库已针对以下行为补充测试：

- fd 所有权、描述符复用、并发关闭，以及关闭后唤醒阻塞或排队的 I/O。
- RawConn 回调期间保持 fd 有效，并支持通过运行时 poller 等待可读或可写。
- Read、Write、Accept 和建连超时，以及读取通知时保持同一超时预算。
- 大端 `socklen_t`、未对齐 metadata 解码、PPID 字节序、混合地址列表及数字接口名与索引冲突。

相关实现与覆盖见 [fd 生命周期测试](../sctp_lifecycle_linux_test.go)、[超时测试](../sctp_timeout_linux_test.go)、[ABI 与编码回归测试](../sctp_regression_linux_test.go)及 [接口名冲突测试](../sctp_zone_linux_test.go)。这些测试支持对具体修复的判断，不能据此推导所有生产场景下的可靠性排名。

### 依赖和实现范围较小

本库的 [go.mod](../go.mod) 没有外部模块依赖。协议处理交给内核，Go 层主要负责 socket API、缓冲区、描述符和运行时 poller 的衔接。

这降低了维护用户态协议状态机的负担，但 Linux 内核版本、参数和部署网络仍影响行为。借助内核协议栈也是其他内核封装库共有的优势，并非本库独有。

## 本库的主要差距

### 标准 deadline 与 Context

本库虽然实现 `net.Conn`，但 `SetDeadline`、`SetReadDeadline` 和 `SetWriteDeadline` 仍返回 `syscall.EOPNOTSUPP`，没有 `DialContext`。现有 `SO_RCVTIMEO` 和 `SO_SNDTIMEO` 提供每次操作的相对超时；操作开始时获取超时预算，无法提供标准 deadline 更新后影响当前阻塞操作的完整体验。[本库 deadline 接口](https://github.com/skill215/sctp/blob/bd2fa276b5be3bdebef3a4a9756cae7567505359/sctp.go#L829-L839)、[超时实现](../socket_io_linux.go)

go-sctp 已通过 `os.File` 实现连接 deadline，并提供带 Context 的 Dialer 和更一致的 `net.OpError` 上下文。需要接入超时、取消和统一错误处理的 Go 服务时，这些接口可以减少业务代码中的适配。[连接实现](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/conn.go)、[拨号接口](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/dial.go)

### 消息边界和接收标志

本库 `SCTPRead` 只返回长度、`SndRcvInfo` 和错误，没有暴露 `recvmsg` 的 `MSG_EOR`、`MSG_NOTIFICATION`、`MSG_CTRUNC` 等 flags。接收缓冲区不足以容纳完整消息时，调用方难以判断消息边界；通知区分和辅助数据截断的处理也受限。wrapped connection 添加的是本地 metadata 头，不能补足这些接收标志。[本库读取实现](https://github.com/skill215/sctp/blob/bd2fa276b5be3bdebef3a4a9756cae7567505359/sctp_linux.go#L181-L231)

go-sctp 的 `ReadMsg/ReadMsgExt` 返回接收 flags，文档说明如何通过 EOR 判断消息是否完整；`ReadMsgExt` 还允许调用方复用 oob 缓冲区。这个设计适合在不破坏现有 `SCTPRead` 签名的前提下借鉴。[ReadMsg API](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/sctp_conn.go#L91-L138)

### 通知解析和常用配置

本库主要通过原始字节回调处理通知，常用配置之外的功能往往需要调用方自行操作 socket option。free5GC 提供部分类型化通知，以及 RTO、关联参数和 MaxSeg 配置；go-sctp 提供更多事件解析、心跳、动态地址绑定和半关闭接口。[free5GC 配置源码](https://github.com/free5gc/sctp/blob/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd/sctp.go)、[go-sctp 事件解析](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/event.go)

底层 socket option 允许扩展能力，但调用方需要理解结构体布局、长度、字节序和调用时机。类型化接口可以把这部分工作集中到库中，减少重复实现。

### Go 版本与平台约束

本库最低要求 Go 1.26，运行依赖 Linux，且不支持 Linux/386。与最低要求较低的库相比，这会增加旧项目的升级成本。Go 版本现代化有利于内部实现和维护，但本身不增加 SCTP 协议能力。

| 库 | 比较快照的 go.mod 声明 |
| --- | --- |
| [skill215/sctp](https://github.com/skill215/sctp/blob/bd2fa276b5be3bdebef3a4a9756cae7567505359/go.mod) | Go 1.26.0 |
| [ishidawataru/sctp](https://github.com/ishidawataru/sctp/blob/19ddcbc6aae2b602d6384cc4048e5f91f0e84e5d/go.mod) | Go 1.12 |
| [free5gc/sctp](https://github.com/free5gc/sctp/blob/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd/go.mod) | Go 1.26.2 |
| [georgeyanev/go-sctp](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/go.mod) | Go 1.20 |
| [pion/sctp](https://github.com/pion/sctp/blob/6ec762ebc1106ae8c5ce1a69208a98dfcfdddaaf/go.mod) | Go 1.24.0 |

## 测试覆盖与性能判断

本库 CI 在 Linux/amd64 上运行 Go 1.26 和 1.27 的集成测试。发布检查要求每个 Go 版本完成十轮 race 测试，并实际运行隔离网络命名空间中的接口名冲突测试。ARM、ARM64、MIPS、MIPS64、PPC64 和 s390x 的 QEMU 检查覆盖 socket-option ABI、对齐和编码；它们不覆盖完整的 SCTP 关联。[CI 配置](../.github/workflows/ci.yaml)、[架构检查](../.github/workflows/arch.yaml)

Pion 的 CI 包括 Windows、macOS、wasm 和定期 fuzz。它覆盖的是用户态协议栈，不能与内核封装的测试范围直接等同。[Pion 平台检查](https://github.com/pion/sctp/blob/6ec762ebc1106ae8c5ce1a69208a98dfcfdddaaf/.github/workflows/test.yaml)、[Pion fuzz](https://github.com/pion/sctp/blob/6ec762ebc1106ae8c5ce1a69208a98dfcfdddaaf/.github/workflows/fuzz.yaml)

本库目前缺少 benchmark 和 fuzz 测试。这次比较没有同场性能数据，因此不对各库的吞吐、延迟、内存或 CPU 消耗排序。性能验证应固定硬件、内核、socket 参数和负载，再比较消息大小、并发关联数、stream 数、丢包条件、分配次数和尾延迟。用户态与内核实现还应说明底层传输和进程内存、内核资源的统计范围。

## 发布版本规范

截至比较日，本库使用 `0.7.1` 等不带 `v` 的标签，发布工作流也仅匹配 `0.*`。GitHub Release 已存在，但这些标签不符合 Go 模块的规范语义版本形式。Go 工具可以按标签查询并解析为伪版本；正式版本发现与升级体验需要改进。[当前发布工作流](https://github.com/skill215/sctp/blob/bd2fa276b5be3bdebef3a4a9756cae7567505359/.github/workflows/release.yaml#L3-L10)、[Go 模块版本规则](https://go.dev/ref/mod#versions)

比较时执行 `go list -m -json github.com/skill215/sctp@0.7.1`，得到版本 `v0.0.0-20261029085721-e864ee06b6c5`；`go list -m -versions github.com/skill215/sctp` 未列出规范语义版本。后续应采用 `v0.7.1` 形式的标签，并同步调整工作流的触发规则。

## 改进优先级

| 优先级 | 建议 | 目的 |
| --- | --- | --- |
| 1 | 规范带 `v` 前缀的版本标签和发布流程 | 改善模块版本发现、固定版本依赖与升级体验 |
| 2 | 增加 `ReadMsg/ReadMsgExt`，保留原有读写 API | 暴露消息边界、通知和辅助数据截断标志，支持缓冲区复用 |
| 3 | 实现标准 deadline 和 `DialContext` | 完善取消、超时及 Go 网络 API 的集成 |
| 4 | 增加类型化通知解析与常用 socket 配置 | 降低业务代码操作原始字节和 ABI 的负担 |
| 5 | 建立 benchmark、fuzz 和跨内核互操作测试 | 量化性能并扩大输入、内核与协议行为覆盖 |

这些是后续建议，不代表当前版本已经具备对应能力。

## 源码比较基线

下列固定提交用于说明比较范围；后续选型应重新核对目标版本的 API、依赖和测试结果。

| 库 | 源码快照 |
| --- | --- |
| skill215/sctp | [bd2fa27](https://github.com/skill215/sctp/commit/bd2fa276b5be3bdebef3a4a9756cae7567505359) |
| ishidawataru/sctp | [19ddcbc](https://github.com/ishidawataru/sctp/commit/19ddcbc6aae2b602d6384cc4048e5f91f0e84e5d) |
| free5gc/sctp | [e86160f](https://github.com/free5gc/sctp/commit/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd) |
| georgeyanev/go-sctp | [a06447f](https://github.com/georgeyanev/go-sctp/commit/a06447f4a276a8d3c79b807215728c74324dee1d) |
| pion/sctp | [6ec762e](https://github.com/pion/sctp/commit/6ec762ebc1106ae8c5ce1a69208a98dfcfdddaaf) |
