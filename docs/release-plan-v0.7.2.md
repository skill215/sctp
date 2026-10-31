# SCTP v0.7.2 发布目标

制定日期：2026-10-30。设计更新：2026-10-31。实现基线：0.7.1，提交 e864ee06b6c5b0c77661c4715814a75576c23237；当前 master 的库实现与其相同。

**状态：A–E 已分批实现，正在执行最终发布门禁。** v0.7.2 修复14条经复现及根因隔离确认的缺陷/输入校验缺口，并已实现 #13 的 P3 非阻塞接口一致性改进。Go 最低要求保持1.26，保留公开函数签名、32字节 wrapper 头格式及旧标签指向。

完整方案位于 OpenSpec change **fix-v0-7-2-stability**：

- [提案](../openspec/changes/fix-v0-7-2-stability/proposal.md)：范围、兼容性与影响。
- [技术设计](../openspec/changes/fix-v0-7-2-stability/design.md)：内核判据、空载荷、地址预算、平台清理和取舍。
- [实施清单](../openspec/changes/fix-v0-7-2-stability/tasks.md)：按依赖顺序执行的待办。
- [行为规格](../openspec/changes/fix-v0-7-2-stability/specs/)：六个能力域的可验证场景。

公开进度及原始复现统一见 [GitHub #3](https://github.com/skill215/sctp/issues/3)。用户已授权实施与发布；只有全部必需验证通过后才创建 v0.7.2 标签并发布。

## 缺陷与修复边界

| Issue | 范围 | 设计选择 |
| --- | --- | --- |
| [#1](https://github.com/skill215/sctp/issues/1) | P1，0.5起的接收回归 | 接收先交付数据，仅在EAGAIN后于明确单关联范围查询合法SCTP_STATUS；无关联返回ENOTCONN，空闲及一对多继续等待。保留poller和同一超时预算。 |
| [#2](https://github.com/skill215/sctp/issues/2) | P2，共享地址race | 地址与IP存储复制后内部补齐通配值，不修改调用方或别名切片。 |
| [#4](https://github.com/skill215/sctp/issues/4) | P1，空接收丢数据 | 活跃连接空Read/SCTPRead为零计数无操作；wrapper缓冲区不超过32返回0、EINVAL，不接收数据。 |
| [#5](https://github.com/skill215/sctp/issues/5) | P1，空发送注入字节 | 所有空payload走真实零载荷sendmsg，保留显式/默认控制参数和内核错误。普通空消息被拒绝时不注入字节、不虚报wrapper头消费。 |
| [#6](https://github.com/skill215/sctp/issues/6) | P2，EOF常量 | 显式EOF=0x200，与零载荷控制发送配套验收。 |
| [#7](https://github.com/skill215/sctp/issues/7) | P2，结构化端口校验 | 内核操作前拒绝负数或大于65535的端口，保留本地端口0。 |
| [#8](https://github.com/skill215/sctp/issues/8) | P2，订阅覆盖 | 在配置锁内合并old掩码与DATA_IO；无error构造器保存初始化错误并由I/O返回。 |
| [#9](https://github.com/skill215/sctp/issues/9) | P2，无metadata头残留 | 返回帧的头完整初始化；metadata缺失时全零，保留原通知载荷。 |
| [#10](https://github.com/skill215/sctp/issues/10) | P2，缺失远端参数 | 高层Dial在创建socket/执行Control之前返回地址错误，不panic。 |
| [#11](https://github.com/skill215/sctp/issues/11) | P2，不完整多地址 | 拒绝多地址空host，不清空显式前缀；保留单段合法通配形式。 |
| [#12](https://github.com/skill215/sctp/issues/12) | P1，wrapper非法计数 | 消除负数与虚报头部，保留有效部分数据及错误，io.ReadAll不panic。 |
| [#14](https://github.com/skill215/sctp/issues/14) | P2，地址查询容量 | 地址区从4KiB按ENOMEM增长至明确1MiB预算；仅成功后解码完整列表，保留local/peer长度ABI差异。 |
| [#15](https://github.com/skill215/sctp/issues/15) | P2，跨平台API | ErrUnsupported共同导出，unsupported Listener补齐SyscallConn。 |
| [#16](https://github.com/skill215/sctp/issues/16) | P2，unsupported fd清理 | 构造失败时使用平台正确socket关闭原语立即释放接管的fd，覆盖0和句柄宽度，不重复关闭复用数字。 |
| [#13](https://github.com/skill215/sctp/issues/13) | P3，非阻塞改进 | 计划规范Accept错误返回nil接口；单独延期不阻止上述14条通过验收后的发布。 |

#7、#10、#11是非法或不完整输入的校验与健壮性问题。#15、#16限定为可移植性及unsupported资源生命周期，不描述成正常Linux I/O普遍故障。

## 实施顺序

| 批次 | 内容 | 验证重点 |
| --- | --- | --- |
| A | #4/#12、#9、#8 | 读取计数与wrapper帧完整性，handler投递和初始化失败 |
| B | #1 | 优先修复回归；单关联判据、PeelOff、一对多、缓冲数据、超时与Close |
| C | #5/#6 | 真正零载荷、默认发送控制参数、EOF/ABORT及ABI |
| D | #2/#7/#10/#11、#14 | 输入快照、先校验后副作用、完整地址查询 |
| E | #15/#16、#13可选 | 外部消费者编译、Unix/Windows资源释放和复用 |
| F | 集成复查与发布准备 | 所有阻塞回归、平台门禁、规范版本标签 |

每批先记录正式回归在基线的失败原因，再实施修复。历史探针中期待未修复行为的观察断言不能直接作为修复后的验收标准。

## 关键设计门禁

- #1 不使用Getpeername判断存活：探针已确认关联终止后仍可能返回缓存地址。SCTP_STATUS的EINVAL只有在合法ABI和明确单关联范围内才可用于无关联判定，其他错误不伪装EOF。
- #5 不统一吞掉空写：默认控制参数的适用性由内核决定；本轮内核对空的默认ABORT返回EINVAL，并非保证每种默认控制都可用。真实零载荷syscall保留内核当时的参数及错误，避免补字节和用户态查询/重放默认参数的竞态。
- #14 的1MiB是显式资源预算。Linux的ENOMEM同时可能表示容量不足或真实分配失败；到预算仍失败即停止并保留errno，不返回部分列表。
- #16 不把Winsock句柄当普通文件关闭，不先截断成int32，也不盲目重试close/EINTR。
- 外部模型不明的raw端点保守处理；本库Dial/Accept/PeelOff的来源必须完整保留。导入fd同时确认SO_PROTOCOL=132；状态查询发现无关联后再接收一次，避免与正常关闭竞态时丢失刚到达的数据或EOF。

## 发布验收

- [ ] 14条阻塞项都有正式回归、基线失败证据、修复后通过证据及正常/原始调用对照。
- [ ] Go1.26.x、1.27.x各十轮完整race/shuffle测试；每轮独立进程、单轮timeout=120s，记录补丁版本、seed和内核。
- [ ] #1在至少两个实际原生Linux内核环境通过，不以交叉编译代替协议运行。
- [ ] 两套Go通过vet、严格checkptr、低GOGC和四类fuzz smoke。
- [ ] ARM、ARM64、MIPS、MIPS64、PPC64、s390x交叉编译及QEMU非SCTP ABI/编码检查通过，覆盖新增零iov发送路径。
- [ ] Linux/386实际执行unsupported及真实SCTP fd清理；Windows/amd64原生执行stub和Winsock资源测试。
- [ ] 外部消费者在Linux/amd64、Linux/386、Darwin/amd64、FreeBSD/amd64、Windows/amd64的两个Go版本均可编译。
- [ ] 数字接口名/索引冲突在隔离namespace实际执行三轮；指定必需CI任务不得以skip代替。
- [ ] README示例可编译且原生回显验证payload、stream和PPID；说明记录纠正后的空操作、帧头、资源预算和能力限制。
- [ ] 独立复查确认本次修复没有引入新的已确认阻塞问题；#13状态如实记录。
- [ ] 发布工作流支持v0.*，保留旧标签；publish依赖全部必需job并校验带注释标签。
- [ ] 后续发布阶段使用规范标签v0.7.2；候选提交先独立验证，正式标签发布后再核对go get @v0.7.2解析为规范版本及tag/release提交一致。

分批实现及 review 证据见 [实施验证记录](../openspec/changes/fix-v0-7-2-stability/evidence/implementation.md)。发布验收状态按实际完成证据更新。历史验证见[真实性复核](issue-verification-2026-10-30.md)、[0.3来源追溯](repro/0.3-history-results.json)及GitHub #3。

## 后续功能版本

v0.8.0另行设计ReadMsg/ReadMsgExt、标准deadline与DialContext、类型化通知、常用socket配置、benchmark及更广泛的跨内核互操作验证。对比依据见[其他Go SCTP库对比](comparison.md)。
