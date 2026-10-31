## 1. 固定实施基线与验收证据

- [x] 1.1 在 codex/fix-v0-7-2-stability 分支准备实现，记录起点提交与已有未提交审计证据，保持历史标签及证据不变。
- [ ] 1.2 将14条阻塞项映射到正式测试名，记录0.7.1失败原因及正常对照；排除期待旧错误行为的观察断言。
- [ ] 1.3 在本机及另一原生Linux内核核对SCTP_STATUS的有效结构体大小、单关联EINVAL边界和PeelOff类型；保存设计判据的可重跑探针。
- [x] 1.4 确认消息表中的空读、空写真实syscall、默认控制位、header-only、部分数据加error及对象错误优先级都有明确预期；若与设计不一致，先更新设计。

## 2. 批次A 读取与wrapper完整性

- [x] 2.1 为#4加入nil/empty、32字节wrapper缓冲区、通知及排队数据的非消费回归，验证随后标记payload完整。
- [x] 2.2 实现零容量接收短路和wrapper短缓冲区错误，保持nil/关闭/初始化失败对象的错误结果。
- [x] 2.3 为#12加入真实reset、EOF、timeout、Close及io.ReadAll回归，消除SCTPRead和wrapper的负数/虚报计数，保留有效部分数据。
- [x] 2.4 为#9加入SENDER_DRY、缓冲区复用和正常metadata对照，确保无metadata帧清零完整头部。
- [x] 2.5 为#8加入既有事件保留、重复包装、并发配置和实际handler投递回归，实现eventsMu及内部配置helper。
- [x] 2.6 保存并传播wrapper订阅初始化错误，验证Read/Write不偷偷I/O且Close仍释放底层资源。
- [x] 2.7 两套Go运行受影响的I/O、通知、codec、timeout和生命周期race测试，独立复核#4/#8/#9/#12修复边界。

## 3. 批次B 关联终止与接收liveness

- [ ] 3.1 为#1补充Read/SCTPRead/wrapped入口，覆盖reset未消费、Write/原始I/O已消费、读已阻塞、并发读写及重复读取。
- [ ] 3.2 保留Dial/Accept/PeelOff的单关联来源，验证NewSCTPConn对已连接raw fd的分类及未知端点的保守行为。
- [ ] 3.3 仅在接收EAGAIN后于已固定fd内执行合法SCTP_STATUS查询，关联消失返回ENOTCONN，其他错误不伪装EOF。
- [ ] 3.4 验证活跃空闲连接、一对多无关联/其他关联、peeled连接和已缓冲数据对照，不新增忙等或提前关闭。
- [ ] 3.5 回归通知单一预算、queued I/O预算、Close唤醒、正常SHUTDOWN及Read/Write/Accept/Connect超时。
- [ ] 3.6 在至少两个记录版本的原生Linux内核、两套Go下验证B批次，并单独review该回归修复。

## 4. 批次C 空载荷与控制发送

- [ ] 4.1 为#5加入空普通写、空metadata写和header-only数据帧回归，用原始sendmsg/标记payload证明没有注入字节。
- [ ] 4.2 为所有空payload加入私有Linux sendmsg helper，放入已有Write gate、EINTR/EAGAIN和超时流程，保留ancillary及对象生存期。
- [ ] 4.3 保留内核默认发送参数；验证显式控制、info=nil默认ABORT、普通空消息拒绝及wrapper失败零计数，避免空写被吞成no-op。
- [ ] 4.4 为#6显式定义EOF=0x200；验证one-to-many EOF、ABORT、abort reason及控制组合/不适用socket的内核错误。
- [ ] 4.5 验证调用方SndRcvInfo不变、PPID字节序、失败发送零计数和wrapped frame重试。
- [ ] 4.6 用非SCTP socket验证零iov/ancillary的32位、大端及对齐ABI，再运行原生SCTP控制消息回归。

## 5. 批次D 地址准备与查询

- [ ] 5.1 为#2加入共享Listen/Dial、len=0/cap>0别名、IPv4/IPv6及失败路径的输入不变/race回归。
- [ ] 5.2 实现地址深副本和内部通配补齐，保持Control观察到的原地址字符串语义及既有zone处理。
- [ ] 5.3 为#7/#10加入负数/65536/大整数端口、nil raddr、各高层入口及低层Bind/Connect回归，断言socket和Control前拒绝。
- [ ] 5.4 在地址准备层实现net.AddrError校验，保留本地端口0、nil laddr和现有公开签名。
- [ ] 5.5 为#11加入IPv4/IPv6、不同位置空host、多段输入及合法单段通配对照，实现不丢弃前缀的解析。
- [ ] 5.6 为#14实现4KiB到1MiB的有界查询，重置头/optlen，保留local/peer长度差异、错误errno和完整性校验。
- [ ] 5.7 验证300 IPv4地址原生查询，以及混合family、IPv6 stride、增长边界、持续ENOMEM、其他错误、非法数量和截断缓冲区。
- [ ] 5.8 两套Go执行地址相关race/fuzz及现有数字zone/网络名回归，记录查询资源预算的可观察行为。

## 6. 批次E 平台API与所有权

- [ ] 6.1 为#15建立外部消费者编译矩阵，引用ErrUnsupported并断言Conn/Listener实现syscall.Conn。
- [ ] 6.2 将ErrUnsupported移至共同文件，补齐unsupported Listener.SyscallConn，保持sentinel和正常Linux错误行为。
- [ ] 6.3 为#16加入真实Linux/386 SCTP fd、fd=0子进程、数字复用及并发/重复Close回归。
- [ ] 6.4 实现unsupported构造失败立即释放、fd=-1与initErr，使用Unix/Windows正确socket关闭helper，避免句柄截断和close重试。
- [ ] 6.5 在Windows原生验证真实Winsock handle释放；验证清理错误可保留且errors.Is(ErrUnsupported)成立。
- [ ] 6.6 处理#13：优先实现Accept错误返回nil接口并保留原error；若延期，明确记录P3非阻塞而不关闭为已修复。
- [ ] 6.7 复核#15与#16独立验收、初始化失败资源路径和公开API兼容，无新增协议支持承诺。

## 7. 批次F 集成复查与CI门禁

- [ ] 7.1 独立review A–E：检查锁顺序、buffer/metadata计数、raw指针生存期、fd复用及错误优先级；对新疑点做正常对照。
- [ ] 7.2 两套Go各用独立进程运行十轮完整race/shuffle测试，单轮timeout=120s，失败即停止；记录Go补丁版、seed和内核。
- [ ] 7.3 两套Go通过vet、严格checkptr、低GOGC及四类fuzz smoke；保留种子和失败复现方式。
- [ ] 7.4 更新六种架构QEMU ABI门禁，加入新增sendmsg及地址增长相关边界；区分编译、非SCTP ABI与原生协议证据。
- [ ] 7.5 增加Linux/386和Windows原生unsupported资源测试及五目标外部消费者编译，必需job不得以skip代替运行。
- [ ] 7.6 隔离namespace中的数字接口名/索引冲突实际运行三轮；验证必需权限缺失会使指定CI任务失败。
- [ ] 7.7 编译并原生运行README回显/超时示例，核对payload、stream和PPID；更新14条issue的修复证据与#13状态。

## 8. 发布准备与后续发布阶段

- [ ] 8.1 更新README、审计/发布计划及v0.7.2说明，记录空操作、wrapper头部、地址预算、平台清理和#1回归来源。
- [ ] 8.2 修改release工作流接受v0.*标签、保留旧标签指向，并使publish依赖所有新增必需job及带注释标签校验。
- [ ] 8.3 在独立模块验证候选提交的API、示例和版本可用性；确认14项阻塞条件均已满足，#13若延期有明确记录。
- [ ] 8.4 后续收到发布指令且门禁全绿后，创建规范带注释标签v0.7.2并发布；本设计阶段不执行此动作。
- [ ] 8.5 发布后核对tag/release提交一致、说明完整、go get @v0.7.2解析为规范版本；失败时保留事实并按新补丁版本流程处理。
