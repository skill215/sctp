# SCTP v0.7.2 发布目标

制定日期：2026-10-30。基线：正式 release `0.7.1`，提交 `e864ee06b6c5b0c77661c4715814a75576c23237`。

**目标：发布一个修复异常断连、I/O 数据完整性、关闭标志、地址校验和事件订阅等已知缺陷的稳定性版本，并规范 Go 模块版本标签。** 首轮审计确认两个缺陷，后续复查又确认八项，均已纳入发布阻塞清单。首轮验证记录见 [0.7.1 审计报告](release-audit-0.7.1.md)。所有目标均须完成并验证后才能发布；本计划不是完成记录或发布日期承诺。

发布进度统一跟踪于 [GitHub issue #3](https://github.com/skill215/sctp/issues/3)。首轮缺陷分别为 [#1 对端 ABORT 后读取持续等待](https://github.com/skill215/sctp/issues/1)和 [#2 共享通配地址的数据竞争](https://github.com/skill215/sctp/issues/2)，后续复查的 issue 列于下方。本文件保留版本目标与验收基线，实际修复进度见关联 issue。

## 发布阻塞项

### P1 修复对端 ABORT 后读取持续等待

- [ ] reset 被 Write 消耗后，Read、SCTPRead、wrapped Read 仍能及时识别终止的关联。
- [ ] 为 ABORT、正常 SHUTDOWN、已缓冲数据、并发读写和重复读取补充正式回归测试。
- [ ] 活跃但暂时无数据的关联仍等待可读；不把所有 `EAGAIN` 一律当作 EOF。
- [ ] 保持通知处理的同一超时预算、接收超时和 Close 唤醒行为。
- [ ] 对照审计报告的红测试验证修复，覆盖 Go 1.26 和 1.27。

### P2 停止修改调用方的地址

- [ ] Listen 和 Dial 内部准备通配地址时使用副本，不修改调用方的 `SCTPAddr` 或共享切片存储。
- [ ] 覆盖共享地址的并发调用与调用前后内容不变断言。
- [ ] 覆盖 IPv4、IPv6，以及成功、绑定失败和建连失败路径。
- [ ] 现有多地址、空 network、IPv6 zone 名称与数字索引冲突测试保持通过。

### 后续复查确认的八项缺陷

- [ ] [#4 P1 零容量读取丢失 payload 并返回越界计数](https://github.com/skill215/sctp/issues/4)。
- [ ] [#5 P1 空 payload 配 metadata 注入额外零字节](https://github.com/skill215/sctp/issues/5)。
- [ ] [#6 P2 SCTP_EOF 常量不符合 Linux UAPI](https://github.com/skill215/sctp/issues/6)。
- [ ] [#7 P2 非法端口被截断后用于监听或连接](https://github.com/skill215/sctp/issues/7)。
- [ ] [#8 P2 wrapper 覆盖已有事件订阅](https://github.com/skill215/sctp/issues/8)。
- [ ] [#9 P2 wrapper 返回未填充的 metadata 头](https://github.com/skill215/sctp/issues/9)。
- [ ] [#10 P2 nil 远端地址导致拨号 panic](https://github.com/skill215/sctp/issues/10)。
- [ ] [#11 P2 多地址末段为空时丢弃显式地址并退化为通配地址](https://github.com/skill215/sctp/issues/11)。

每项修复须满足关联 issue 的验收条件。新增 [复现测试源码](repro/0.7.1-additional-regressions_test.go.txt) 已在 Go 1.26.10 和 Go 1.27.2 上各重复三轮；修复时应将对应断言加入正式测试，并验证修复前失败、修复后通过。

## 版本发布规范

- [ ] 新版本使用带 `v` 前缀的规范语义版本标签 `v0.7.2`，保留旧标签的既有指向。
- [ ] 发布工作流支持规范标签，并在新流程验证通过后发布对应 GitHub Release。
- [ ] 从带注释标签正确读取说明；release 说明包含全部已确认缺陷的修复、Go 最低要求与实际验证范围。
- [ ] 在独立 Go 模块中验证 `go get github.com/skill215/sctp@v0.7.2` 及版本发现；新标签应解析为 `v0.7.2`，不再转换为伪版本。
- [ ] Go 最低要求保持 1.26；现有公开接口签名保持兼容。

## 发布验收

- [ ] 在 Linux/amd64 上，Go 1.26.x 与 1.27.x 各运行十轮完整 race 测试，并记录实际补丁版本。
- [ ] 全部已确认缺陷的正式回归测试在修复前失败、修复后稳定通过。
- [ ] 两套 Go 版本的 `go vet ./...` 通过。
- [ ] 严格 checkptr 与低 GOGC 下的关键 I/O、通知、生命周期及 ABI 回归通过。
- [ ] 数字接口名与索引冲突测试实际执行三轮；必需的 CI 任务不能跳过。
- [ ] ARM、ARM64、MIPS、MIPS64、PPC64、s390x 的交叉编译及 QEMU ABI/编码检查通过。
- [ ] README 中的客户端、服务端和超时配置示例可编译，SCTP 回显验证 stream 与 PPID。
- [ ] 发布前复查没有未解决的已确认发布阻塞缺陷，公开记录仍然存在的能力限制与验证边界。
- [ ] GitHub Release 仅在所有必需的测试和架构任务成功后生成；发布后核对标签、说明和 Go 模块安装结果。

全套测试通过代表所列验证范围通过，不代表对全部部署环境的无缺陷证明。

## 后续功能版本

稳定性版本之后，再规划 `v0.8.0` 的能力扩展：

1. 兼容现有 API 的 `ReadMsg/ReadMsgExt`，暴露消息边界、通知与辅助数据截断标志。
2. 标准 deadline 与 `DialContext`，同时设计其与现有 socket timeout 的交互。
3. 类型化通知解析及常用 socket 配置。
4. benchmark、fuzz 和跨内核互操作验证。

设计依据与其他库的接口对照见 [Go SCTP 库对比](comparison.md)。
