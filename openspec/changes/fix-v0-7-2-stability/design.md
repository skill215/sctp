## Context

设计日期：2026-10-31。实现基线为 master 70b338ac419dff6aa745460bb379d68208d615e0，库实现与 0.7.1 的 e864ee06b6c5b0c77661c4715814a75576c23237 相同。

范围是 [#3 发布跟踪](https://github.com/skill215/sctp/issues/3) 中的 14 条阻塞项：#1、#2、#4–#12、#14–#16；#13 单列为 P3 非阻塞改进。#1 从 0.5 的 poller 改动开始回归，其他多数核心缺陷不晚于 0.3 已存在。历史来源不改变本次验收标准。

当前架构由 SCTPConn 持有 os.File 和 socketIO；socketIO 分别串行化读写方向，将 SO_RCVTIMEO/SO_SNDTIMEO 快照转换为每次操作的 poller deadline。通知回调在 fd 回调之外执行。设计保留这些边界，把修复集中在 syscall 结果解释、输入准备和资源初始化。

## Goals / Non-Goals

**Goals:**

- 为全部 14 条缺陷确定可独立审查、可验证的修复边界。
- 保持已有公开函数签名、32 字节 native-endian wrapper 头、PPID 的 host-order 接口和 Go 1.26 最低要求。
- 保留 Close 唤醒、独立读写并发、队列等待计入预算、通知共用同一读取预算，以及 one-to-many 端点继续服务其他关联的能力。
- 将审计探针改写为针对修复后契约的正式回归；每个缺陷都保留正常行为或原始系统调用对照。

**Non-Goals:**

- v0.7.2 不引入 ReadMsg、消息边界标志、DialContext、标准 deadline、新通知解析层或 Go 版本升级。
- 不重写 poller，不用整连接大锁串行化收发，不引入自动关闭连接的终止状态缓存。
- 本阶段只生成设计与任务，不实施修复，不执行发布。

## Decisions

### D1 在接收 EAGAIN 后核实单关联是否仍存在

覆盖 #1。接收回调始终先执行非阻塞 Recvmsg，读到的 payload 或通知按现有流程处理。只有返回 EAGAIN/EWOULDBLOCK 时，才判断是否应继续等待。

引入内部 associationScope，区分单关联连接与 one-to-many/未知端点。Dial、Accept 和本库 PeelOff 的返回路径标记单关联；NewSCTPConn 对 SOCK_STREAM 可确定单关联，对 SOCK_SEQPACKET 仅在已成功查询 assoc_id=0 的 SCTP_STATUS 时确定为独占关联。其余保守按端点处理。分类在返回对象前完成；PeelOff 显式保留来源，不能只依据 SO_TYPE，因为 peeled socket 仍可能是 SOCK_SEQPACKET。

单关联 EAGAIN 路径在同一个已固定 fd 的回调内直接查询 SCTP_STATUS，使用正确大小的 Status、assoc_id=0 和 uint32 socklen_t：

| 查询结果 | 接收动作 |
| --- | --- |
| 查询成功，关联仍可查询 | 返回未就绪，由既有 poller 等待 |
| 合法查询返回 EINVAL，证明单关联已经不可查询 | 返回 0、ENOTCONN，结束本次读取 |
| 查询返回其他错误 | 返回该错误，计数为 0，不转换为 EOF |
| one-to-many 或无法确定的端点 | 保留原有等待行为，不因 assoc_id=0 查询失败而终止端点 |

EINVAL 的解释只用于此处格式已知正确的内部 SCTP_STATUS 查询，不能扩展为“任意 getsockopt 的 EINVAL 都表示断连”。Linux v6.6 实现中，状态查询的 EINVAL 来源包括结构体长度不足和关联不存在；因此 ABI 校验是启用该判定的前提。每次查询都会重新初始化参数，避免输入垃圾影响判断。

此顺序保证先处理内核仍返回的数据，再解释无数据状态。不会提前丢弃缓冲区、吞掉真正的 ECONNRESET，也不读取会消费错误的 SO_ERROR。已有操作 deadline 在进入通知循环前取得，查询后仍沿用原预算。

单关联接收契约面向已完成建连的 socket；直接导入 raw fd 的调用方仍须完成其连接流程。外部导入、模型不能确定的 SOCK_SEQPACKET 不凭猜测结束读取；本库 PeelOff 的确定路径必须完整覆盖。

**备选与依据：** 恢复阻塞 fd 会破坏已验证的 Close/并发/超时行为。仅缓存 Write 的 fatal error 无法覆盖其他原始 I/O 消费 reset，还需要设计唤醒和排空顺序。Getpeername 已在三轮探针中证实会保留缓存地址，不能判定关联存活。新增的状态查询只发生在未就绪路径，避免扩大为通用状态机。

### D2 明确空读写、控制发送和帧计数

覆盖 #4、#5、#6、#9、#12。

| API/输入 | 设计结果 |
| --- | --- |
| 活跃连接 Read(nil)、Read(empty) | 0、nil；不接收 payload 或通知 |
| 活跃连接 SCTPRead 空缓冲区 | 0、nil metadata、nil；不进入 Recvmsg |
| wrapper Read 缓冲区长度小于或等于 32 | 0、EINVAL；不接收数据，不改写缓冲区 |
| 普通空 Write / 空 SCTPWrite，未带显式或默认控制位 | 使用真实零载荷发送；Linux 拒绝普通空消息时返回 0、原始错误，不注入 payload |
| wrapper 只有头部的普通数据写入 | 底层拒绝时返回 0、原始错误；不将被拒绝的头部计为成功消费 |
| 空 payload 带合法 EOF/ABORT 控制 | 发送真正的零载荷控制消息；成功返回 payload 计数 0 |
| wrapper 控制帧只有头部 | 控制发送成功后返回 32；失败且未发送时返回 0、error |

零长度读取先检查连接对象的初始化和关闭状态，再执行无操作短路；nil/初始化失败/已关闭对象继续返回明确错误，计数仍为 0。空读短路不会访问 fd 或触发通知 handler。空写仍进入既有 Write gate，并遵循连接错误与发送预算。

EOF 显式定义为 Linux MSG_FIN，即 0x200；UNORDERED、ADDR_OVER、ABORT、SACK_IMMEDIATELY 保持现有数值。控制组合、socket 类型和关联要求均由 Linux 校验；普通 SOCK_STREAM 不获得额外的 EOF 能力承诺。

所有空 payload 发送使用私有 Linux sendmsg 辅助函数：Msghdr 的 payload iov 长度为 0，存在的 ancillary buffer 原样保留，info=nil 时仍允许内核应用默认发送参数。仅空载荷分支绕开 Go SendmsgN 的补字节逻辑，非空发送保留已有实现。辅助函数放在现有 Write gate、EINTR/EAGAIN 重试和超时预算内；使用 Cmsghdr.SetLen/SetControllen、原生布局及明确的 KeepAlive，保留 uintptrescapes 规则。不修改调用方 SndRcvInfo，PPID 仍先复制再转为网络字节序。

此处不统一吞成 no-op，也不在用户态先读取默认控制位再决定发送，因为默认参数还可以按关联设置，读取后再发送会引入配置竞态。由一次真实 sendmsg 使用内核当时的参数，保留 SetDefaultSentParam 的合法 ABORT 等控制行为。

SCTPRead 的 syscall 错误返回统一消除负数计数；真正收到的有效字节不因随后 metadata 解析失败而丢弃。wrapper 仅在有有效 payload/通知字节时返回帧：

1. 有 metadata：写入完整 32 字节编码头。
2. 无 metadata：清零整个头，再返回实际 payload/通知字节。
3. n=0 且有错误/EOF：返回 0、error，不虚报头部。
4. n>0 且有错误：返回已初始化头部和有效 payload 的总计数，同时返回错误。

无 metadata 的全零头只表示没有可提供的 metadata。它无法区分“通知”和合法的 stream=0、PPID=0；沿用 NotificationHandler/原始通知载荷的现有机制，并在 README 写明该限制。

### D3 将 wrapper 的订阅更新作为一次配置操作

覆盖 #8。SCTPConn 增加只保护事件订阅操作的 eventsMu。公开 SubscribeEvents 仍表示替换掩码；wrapper 构造时执行“读取旧掩码，再写入 old | DATA_IO”，两步持有同一把配置锁。公共订阅方法和 wrapper RMW 使用共享的内部未加锁辅助函数，避免重复加锁。

eventsMu 不覆盖阻塞 I/O、用户 handler 或整个连接生命周期。通过公开 RawConn 直接改事件的调用方仍负责与其他配置操作同步，不能承诺约束外部 raw syscall。

构造器签名不变。查询或设置失败时，wrapper 保存 initErr，后续 Read/Write 在执行 I/O 前返回该错误；Close 始终委托底层释放资源。不会静默假装 metadata 已启用，也不会在读写时自行重试配置。修正配置后需要重新构造 wrapper。

### D4 统一准备地址副本和输入校验

覆盖 #2、#7、#10、#11。新增内部地址准备层，负责校验、复制以及内部通配地址补齐。复制 SCTPAddr、IPAddrs 的底层存储和各 net.IP 字节；即使输入切片 len=0、cap>0，也不能通过 append 改写调用方的别名存储。调用方仍有责任在调用期间不自行并发修改输入。

执行次序：

~~~text
network 校验 → nil/range 校验 → 地址快照 → family 选择
→ 创建/初始化 socket → Control → 绑定/连接
~~~

Control 的 address 字符串由补齐通配 IP 之前的输入快照生成，保留 nil 本地地址为空串、Port-only 地址为 :port 的既有观察结果。内部绑定使用另外准备好的副本。

错误使用 net.AddrError；不依赖字符串精确匹配。端口范围是 0..65535，本地 0 保留自动分配语义，远端 0 仍交由既有内核语义处理。DialSCTP、DialSCTPExt、SocketConfig.Dial 在创建 socket/执行 Control 前拒绝 nil raddr。

SCTPBind/SCTPConnect 等直接触及内核的公开低层入口也校验结构化地址和端口。ToRawSockAddrBuf 的返回类型不变，仍是无错误返回的低层序列化函数；不通过修改其签名或增加 panic 来表达校验失败，文档标明 networking 入口负责验证。

多地址字符串中每一段必须含 host；末段缺 host 不能清空前面已解析的地址。拒绝前导、中间、末尾空 host；单段 :port 的合法通配形式保持。显式 0.0.0.0、:: 不等同于空字符串，保留既有解析及内核校验语义。IPv6 括号、zone 名称优先于数字索引的现有修复保持。

### D5 有界重试完整地址查询

覆盖 #14。使用 8 字节 sctp_getaddrs 头与动态 byte buffer，初始地址区容量 4 KiB。仅在该地址查询返回 ENOMEM 时翻倍，最多到地址区 1 MiB，共最多 9 次尝试；每次重置 assoc_id、addr_num 和 uint32 optlen，错误分支丢弃部分写入的数据。

1 MiB 是明确的每次查询资源预算，不宣称支持无限大小的地址集合。它容纳 65,536 个 IPv4 sockaddr 或约 37,449 个 IPv6 sockaddr，远大于本次 300 地址复现。超预算仍失败时返回带容量上下文且可 errors.Is(ENOMEM) 的错误，不返回残缺列表；不新增公共配置 API。其他 errno 直接返回，不扩容重试。

Linux local 查询的 optlen 只计地址区，peer 查询还计入头部；分别规范化后，才调用现有的逐地址 family/长度解码。拒绝越界 optlen、非法数量、截断和未知 family，不能以固定 IPv4/IPv6 stride 解码混合列表。

此资源策略须在 README、公开地址查询方法注释和发行说明中明示。Listener.Addr 仍受 net.Addr 无 error 返回的签名限制；查询失败返回 nil，不用配置快照冒充完整的内核地址。

**依据与权衡：** Linux 将缓冲区不足和分配失败都报告为 ENOMEM，不能用一次 errno 区分；有界增长限制真实 OOM 时的重试和分配压力。仅扩大固定数组仍留下隐含上限；无界增长则可能把一次查询放大成内存耗尽。

### D6 统一公开 API 并在 unsupported 初始化失败时释放 fd

覆盖 #15、#16。将 ErrUnsupported 移到共同编译文件，保留现有错误文本和 sentinel 语义；正常 Linux 操作的错误不强行改写成该 sentinel。unsupported Listener 补齐 SyscallConn，返回 nil、ErrUnsupported。消费者可在所有受测目标编译同一份错误检查和 syscall.Conn 接口断言。

unsupported NewSCTPConn 无法完成初始化，因此在构造时接管并立即释放有效 fd，返回 initErr=ErrUnsupported 的对象，内部 fd 标为 -1。后续 Close 不再尝试该数字，保持返回平台不支持错误。关闭失败作为 initErr 的附加原因保留，errors.Is(ErrUnsupported) 仍成立；不盲目重试 close/EINTR，避免关闭已复用的数字。

unsupported 的 I/O、SyscallConn 和 Close 通过统一内部 helper 返回对象的 initErr；没有对象或没有附加错误时仍返回原 sentinel。这样无 error 返回值的构造器也能让清理失败经公开方法被观察到，不只是把错误存进不可见字段。

释放必须调用平台正确的 socket 关闭原语：Unix 类目标使用 native close，Windows 使用 closesocket；不能把 Winsock handle 当 os.File.Close 的普通文件句柄，也不先截断到 int32 再释放。fd=0 属于有效所有权，需要子进程验证。

此方案与受支持平台初始化失败就关闭 fd 的处理一致，不为 unsupported 引入 poller 或 finalizer。仍按 ErrUnsupported 拒绝 I/O，不扩大支持平台。

### D7 小步提交与发布边界

实现分组如下。每组先加入能在基线失败的正式行为回归，再提交修复和正常对照；不把“期望未修复行为”的审计控制断言直接复制为验收标准。

| 批次 | 内容 | 依赖 |
| --- | --- | --- |
| A | #4/#12 读取计数、空缓冲区；#9 帧头；#8 订阅 | 共用 wrapper 路径，统一复核 |
| B | #1 关联终止接收、PeelOff 分类和超时/关闭对照 | A 的计数规范；优先修复已定位的回归 |
| C | #5 空载荷与零载荷控制发送；#6 EOF 常量 | raw sendmsg 与标志修复配套验收 |
| D | #2/#7/#10/#11 地址快照与校验；#14 地址查询 | 先输入层，再验证大地址集合 |
| E | #15/#16 平台 API 和所有权；#13 可选规范化 | 独立生命周期与下游编译门禁 |
| F | 集成复查、CI 与发布准备 | A–E 的 14 项阻塞验收 |

#13 计划实现为 Accept 在错误时显式返回 nil、原始 error；若单独延期，必须记录原因且不作为发布阻塞，不把未实施描述为修复完成。

### D8 验证与 CI 设计

- 原生 Linux/amd64：Go 1.26.x、1.27.x 各 10 轮完整 race/shuffle 测试，每轮独立进程、单轮 timeout=120s，任一失败停止；记录补丁版本、seed和内核，关键 #1 场景覆盖至少两个实际内核环境。不能把十轮共用的 binary timeout 误写成单轮预算。
- 专项：Read/Write/Accept/Connect 超时、通知预算、queued I/O 与 Close、缓冲数据排空、normal SHUTDOWN、reset 未消费/已消费和重复读取。
- raw sendmsg：在六种架构 QEMU 中用非 SCTP socket 验证零 iov 和 ancillary ABI；真正 EOF/ABORT 控制在原生 SCTP 测试。支持 ARM、ARM64、MIPS、MIPS64、PPC64、s390x，显式保留 32 位 socklen_t、Cmsg 对齐和大端字节序检查。
- Linux/386：原生执行 unsupported API 与真实 SCTP fd 清理测试，不使用 race detector。Windows/amd64 增加原生 stub/socket 关闭测试，不运行 SCTP 关联测试。
- 外部消费者：两个 Go 版本编译 Linux/amd64、Linux/386、Darwin/amd64、FreeBSD/amd64、Windows/amd64，覆盖 sentinel 和 Conn/Listener 的 syscall.Conn 接口。
- 每个 Go 版本执行 vet、严格 checkptr、低 GOGC；保留 fuzz 的 sockaddr、ancillary、codec、地址往返种子和增长后的边界种子。
- 数字接口名与索引冲突在隔离 namespace 实际执行 3 轮，CI 拒绝跳过。
- 不把 QEMU ABI 测试或交叉编译写成完整 SCTP 协议验证。optional 环境用例本地可以显式跳过，发布所需的指定 CI job 不得以 skip 成功代替覆盖。

## Risks / Trade-offs

- SCTP_STATUS 的 EINVAL 也可表示非法结构体长度 → 判定仅用于内部正确 ABI、assoc_id=0 的单关联查询；门禁加入 Status 布局和活跃连接对照。
- peeled socket 与 endpoint 的 SO_TYPE 可能相同 → 记录构造来源；外部模型不明时保守等待，不能仅按关联数量=1 推断独占。
- 初始化/通知配置错误可能被现有无 error 构造器隐藏 → wrapper 存储 initErr 并在首个 I/O 返回；unsupported 构造器立即释放 fd。
- 空写和 header-only 读的历史行为改变 → 表格明确空读短路与空写真实 syscall；发行说明列出纠正后的语义，并用标记 payload 证明没有丢失/注入。
- 地址查询 ENOMEM 含义不唯一 → 有界增长、保留 errno、不得解析失败调用留下的部分列表；明确 1 MiB 预算。
- Windows socket 句柄和 Unix fd 关闭方式不同 → 平台私有 helper 与原生 Windows/386 资源复用测试，避免只凭 Linux 编译推断正确。
- 订阅锁可能引入重入问题 → 仅覆盖短配置 syscall，使用未加锁内部 helper，不覆盖用户 handler、Close 或读写等待。

## Migration Plan

1. 在基线归档验证每条正式回归的失败原因，保留原始系统调用/正常行为对照。
2. 按 A–E 分批实施；每批通过针对性测试和受影响既有测试后再集成。发布门禁 F 最后统一执行。
3. 更新 README、release audit、release plan 和 v0.7.2 说明，准确记录 #1 的回归来源与各错误行为改变。
4. 发布工作流支持 v0.*，可继续保留旧 0.* 兼容触发；规范带注释标签 v0.7.2 只在必需 job 成功后发布。历史标签不移动、不删除。
5. 先在独立临时模块验证候选提交、API 和示例；正式标签发布后再核对 go get @v0.7.2 的解析结果与 release 指向。
6. 回滚按批次撤销提交，不移动已发布标签；已发布后发现问题，以修复提交和新的补丁版本处理，不用带有已知 #1 回归的版本冒充稳定恢复点。

## Open Questions

- 在额外原生内核中确认合法 SCTP_STATUS 对“关联不可查询”的 errno 边界，以及 externally imported SOCK_SEQPACKET 的保守分类。实施 B 之前必须完成证据门禁；若与本设计不一致，先调整设计，不将未知错误伪装成 EOF。
- 1 MiB 是本版明确选择的地址查询预算。若真实部署需要更大集合，先补需求和资源测量，再设计可配置查询接口；这不阻止本版覆盖已复现的 300 地址场景。

## References

- [提案与规格](proposal.md)、[GitHub #3 与逐项证据](https://github.com/skill215/sctp/issues/3)。
- [Linux v6.6 SCTP socket 实现](https://github.com/torvalds/linux/blob/v6.6/net/sctp/socket.c)：sctp_getsockopt_sctp_status、sctp_id2assoc、sctp_getsockopt_local_addrs、sctp_getsockopt_peer_addrs、sctp_do_peeloff。
- 仓库 docs/repro 的历史/五轮/真实性复核结果是审计基线；其中部分观察断言针对未修复行为，不能作为 green-test 契约。
