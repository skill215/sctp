# SCTP 底层设计与修复复查

日期：2026-11-06。基线为 v0.7.3，提交
`e4154064dc9928274bcf4615c5382dd6fd81ad21`；下述修复位于本地工作区。
范围仅限本 SCTP 仓库。

## 总体判断

对于“Go 调用 Linux 内核 SCTP”的定位，现有底层方向合理，可以继续维护，
没有证据支持重写协议栈或引入新的网络框架。真正需要加强的是 syscall 边界
校验、生命周期不变量、测试夹具，以及职责和错误语义的可读性。

它目前适合作为明确了解 Linux SCTP 语义的底层库。若要作为通用 Go 网络库，
还需要单独设计标准 deadline、DialContext、完整消息边界与通知接口。
这些是已经公开的能力限制，不能与本轮确认的 bug 混为一谈。

## 分层与生命周期

| 层 | 当前实现 | 判断及约束 |
| --- | --- | --- |
| Linux syscall / UAPI | `sctp_linux.go`、`sendmsg_linux.go`、公共 ABI 类型 | 直接绑定合理；固定宽度、布局、字节序必须由实际 ABI 测试守住 |
| fd 所有权与等待 | `os.File`、`socketIO`、RawConn、方向 gate | 由 os.File 管理物理 fd；操作必须在 Control/Read/Write 的引用保护内执行 |
| SCTP 消息语义 | SCTPRead/SCTPWrite、状态探测、通知处理 | 保留流号/PPID、内核 errno 和关联差异，避免把一对多端点当独占关联 |
| 地址与关联 | Resolve、bind/connect 编码、可增长查询、PeelOff | 接口保留，所有 int 转内核窄整数前必须检查范围 |
| 兼容适配 | 32 字节 SndRcvInfo 包装连接 | 格式是本地适配格式；不能提供 API 未暴露的完整 EOR/通知分类能力 |
| 示例程序 | CLI 解析、每连接配置、echo | 配置输入与内核读回值必须分离；无效地址应在创建 socket 前失败 |

当前连接中的原子 `_fd` 用于逻辑关闭状态和一次性 Close，物理使用权仍属于
os.File。它不是可以绕过 RawConn 直接使用的“有效 fd 保证”。listener 中的
私有 `fd` 字段已经没有读取者；未来可以移除这个影子状态，但本批不借机更换
所有权模型。

已修复 #19：FileListener 的源文件必须在 RawConn.Control 中完成原子的
F_DUPFD_CLOEXEC。KeepAlive 只能避免 GC 回收，不能防止并发 Close，也不能
替代 fd 引用保护。原文件与导入后的副本各自关闭，失败路径只关闭自己取得的
副本。回归测试同时检查并发关闭、fd 复用、原文件所有权、关闭及 nil 输入。

NewSCTPConn 的现有签名无法立即返回构造错误，因此保留 initErr 和失败后仍
正确释放 fd 的行为。未来如增加严格导入校验，宜提供返回 error 的新增入口，
而不是悄悄改变旧构造函数的所有权和失败语义。

## I/O、超时与通知

非阻塞 fd 配合 Go runtime poller 的方案适合当前实现。每个方向分别串行化
等待，读写方向可以并发；Close 同时唤醒 poller 和还在 gate 排队的操作。
超时在公开操作开始时快照，包含排队及连续消费通知的时间；清除操作 deadline
必须发生在释放方向 gate 之前。当前实现满足这些设计不变量，相邻回归覆盖
Read、Write、Accept、建连、通知循环、超时清零和关闭唤醒。

独占关联在 recvmsg EAGAIN 后使用状态查询识别已被其他操作消费的 reset，
并在报告关联消失前再次读取，避免丢弃同时到达的数据或 EOF。该处理必须保持
仅针对已知独占关联；一对多端点的 id=0 EINVAL 不能作为整体连接终止信号。
这部分依赖准确 ABI 和内核行为，两个原生 Linux 内核仍是发布验收要求。

通知 handler 在 RawConn 回调和方向 gate 外执行，因此可关闭连接，不会因为
持有底层 fd 引用而自锁。handler 是同步用户代码；调用方应避免长期阻塞，
并复制需要在返回后保留的字节。超时无法抢占任意阻塞的用户回调。

标准 SetDeadline 系列仍返回 EOPNOTSUPP，DialContext 尚未提供。后续若增加，
必须把标准 deadline、socket 相对超时、队列等待和取消合并成明确的一套截止
时间规则，不能继续叠加互相覆盖的定时器。

## ABI 与平台边界

本批 #21 和 #23 表明主要风险来自 Go int 与内核字段宽度不一致。修复对
INITMSG 的四个值验证 0..65535，对关联 ID 验证 signed 32-bit 可表示性，
再进行转换。无效请求不能改变选项，也不能操作低 32 位相同的另一个关联。
有效零值、signed 32-bit 值和既有内核错误保留。

socket option 的 socklen_t 固定为 uint32；SndRcvInfo、Status 和 peer 地址
布局不能用 Go int 替代。PPID 只在边界做主机/网络序转换，原生结构使用 native
字节序。公共 uintptr option 接口保留底层逃生能力，但也保留调用方提供正确
指针、大小及生命周期的责任。应继续运行 strict checkptr 和跨架构 ABI 测试。

支持范围仍为启用内核 SCTP 的 Linux，排除 Linux/386。本地复核在一个
Linux/amd64 内核上执行；ARM、ARM64、MIPS、MIPS64、PPC64、s390x 的本批验证
是测试二进制交叉编译，不能冒充实际 SCTP 或 QEMU 运行。本机未安装 QEMU。
现有 CI 的 QEMU job 只验证 ABI/编码，不验证完整 SCTP 关联。

现有架构矩阵没有覆盖 riscv64、loong64、ppc64le、mipsle、mips64le；这属于
验证覆盖缺口，不是本轮发现的这些架构已有 bug。下一阶段可以先加入 compile
job，再根据实际部署范围扩充 native/emulated 验证，明确区分每类证据。

本轮 Solaris 库构建成功；其测试编译中的已知 syscall.Dup2 缺口和 illumos
支持继续按既定边界记录、延期处理。非 Linux 的消费者编译和 stub 测试是兼容
防护，不构成这些平台获得 SCTP 支持的承诺。

## 错误处理

现有错误处理能表达核心状态，但接口间不完全统一：

- 正常 I/O 关闭通常映射为 net.ErrClosed，而关闭后的 SyscallConn/部分
  option 入口可能返回 EINVAL；RawConn.Control 还可能原样返回运行时的
  “use of closed file”错误。不能假设所有入口都满足 errors.Is(os.ErrClosed)。
- 保留 Linux errno 对底层 SCTP API 有价值；socket 读写超时返回 EAGAIN，
  建连超时返回 EINPROGRESS，与标准 deadline 错误不是同一合同。
- 普通网络入口缺少统一的操作/网络/地址上下文。未来可考虑 net.OpError，
  但要保留 errors.Is 对底层 errno 的判断，并先审计调用方对错误类型的依赖。
- unsupported、关闭/初始化失败、参数错误之间应有稳定优先级。本批把新校验
  放在已有 fd 入口之后，保持 unsupported stub 返回 ErrUnsupported 的行为，
  并增加了专门回归。

因此，本批修复明确的参数/所有权错误，不对整库做未经版本设计的错误类型
替换。下次错误模型调整应先给出逐入口行为表及兼容测试。

## 测试夹具：本轮继续测试发现的问题

低 GC 验证发现已有 TestPeelOffCloseOnExec 在建连阶段偶发 EISCONN。
独立信号压力对照中，SCTPConnect 和直接 CONNECTX3 syscall 各执行 1000 次，
均出现同样返回；peer-address 查询及 Accept 确认关联实际上已经建立。
这是阻塞 syscall 被信号重启后的内核行为，不能据此把原始 API 的所有
EISCONN 都转换成成功。问题登记为测试夹具缺陷 [#24](https://github.com/skill215/sctp/issues/24)。

修复统一为 lifecycleEndpoint：非阻塞建连、受限的就绪等待、真实 peer payload
和 DATA_IO 元数据确认关联 ID；立即成功时仍核对 CONNECTX3 返回的 ID。
完成 setup 后排空标记消息并恢复默认事件/超时。生命周期、PeelOff、状态、
发送及本轮关联 ID 回归复用这个夹具，新增 SIGURG 下的 setup、PeelOff 和
真实 payload 回归。所有 fd、进程和信号 goroutine 都有明确清理路径。

## 代码组织建议

当前可维护，但 `sctp.go` 超过千行，同时包含 ABI 类型、地址、options、关联
查询、连接接口和包装连接；`sctp_linux.go` 同时承担 syscall、构造、收发和
等待。这提高了修改时遗漏相邻契约的概率。

建议按职责、小批次整理，保持同一个 sctp 包和公开签名：

1. 抽出 wrapped_conn.go，单独承载 32 字节适配格式及转发方法。
2. 分离 UAPI 类型/常量与 socket-option 方法；将关联状态、查询、PeelOff 和
   本次新增范围校验集中在 association 相关文件。
3. 再拆 Linux socket 生命周期与消息 I/O；保留 socket_io_linux.go 的统一
   所有权、gate 和 deadline 机制，避免重新出现多套关闭/等待实现。
4. 清理未使用的 setNumOstreams 和 listener.fd；测试按功能组织，公共夹具
   集中管理，保留 bug 复现链接和修复前后证据。

拆文件本身不解决行为 bug。每一步应保持行为不变并单独 review；本批不同时
重构连接状态机、更换 syscall 后端或新增平台支持。

## 本地候选与后续门限

本地修复覆盖 #19–#24；#23 是关联目标截断，#24 是测试夹具。示例无效地址
和 buffer 配置问题仍是示例缺陷，不扩大描述为正常库收发失败。
完整命令、失败原因和最终结果见 [修复验证记录](fix-validation-2026-11-06.md)。

通过本地验证后适合进入提交/PR 和远端候选验证。实际发布仍需运行现有两个
Linux 内核、两个 Go 版本、架构 ABI、README 和独立 API 消费者门限；本批
五轮本地 race 复查不能替代发布要求的每个 native job 十轮验证。
没有充分证据证明当前版本“绝对没有 bug”。
