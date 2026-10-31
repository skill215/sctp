## Why

0.7.1 仍有 14 条经重复复现和根因隔离确认的缺陷或输入校验缺口，另有 #13 一条 P3 非阻塞一致性改进。#1 是 0.5 引入的非阻塞接收回归，其余问题多数可追溯到 0.3；v0.7.2 应集中恢复可靠的 I/O、地址和 fd 生命周期行为。

## What Changes

- 修复 #1：在接收返回 EAGAIN 时区分已失去关联的 one-to-one socket 与仍然活跃的空闲连接，保留 poller、Close 唤醒和同一操作的超时预算。
- 修复 #4、#5、#6、#9、#12：定义空读写、零载荷控制发送、EOF 标志、wrapper 头部与返回计数的完整行为。
- 修复 #8：wrapper 在已有事件订阅上开启 DATA_IO，串行化库内的订阅更新，并保留初始化失败信息。
- 修复 #2、#7、#10、#11、#14：不修改调用方地址；在创建 socket 和调用 Control 之前校验高层输入；拒绝多地址空段；动态且有界地查询完整地址列表。
- 修复 #15、#16：统一平台间公开 API，保证 unsupported 构造器接收的 fd 有明确释放路径。
- 计划一并规范 #13 的 Accept 错误返回；它仍为 P3，单独延期不改变 14 条发布阻塞项。
- 补齐正式回归、原始系统调用对照、下游消费者编译和架构检查；以规范标签 `v0.7.2` 发布，保留旧标签。

本阶段只交付设计与任务。公开函数签名、32 字节 wrapper 头格式、Go 1.26 最低要求和 Go 1.26/1.27 测试矩阵保持兼容；不会新增 ReadMsg、DialContext 或标准 deadline 能力。

## Capabilities

### New Capabilities

以下是首次建立的规格覆盖，不表示新增同名产品功能。

- `sctp-association-lifecycle`：关联终止、空闲等待、并发 I/O、超时和 Close 的接收语义；#1 与 #13。
- `sctp-message-io`：读取计数、空载荷、控制消息、metadata 和 wrapper 帧；#4、#5、#6、#9、#12。
- `sctp-event-subscriptions`：已有订阅的保留、库内并发更新和初始化失败传播；#8。
- `sctp-address-contracts`：输入快照、参数校验、多地址解析和动态地址查询；#2、#7、#10、#11、#14。
- `sctp-platform-compatibility`：跨平台 API 一致性与 unsupported fd 所有权；#15、#16。
- `sctp-release-validation`：回归证据、原生与仿真验证边界、发布门禁和规范版本标签。

### Modified Capabilities

无已有 OpenSpec 规格需要修改。

## Impact

- 主要实现位置：`sctp.go`、`sctp_linux.go`、`socket_io_linux.go`、`sctp_unsupported.go`；新增内部辅助函数和必要的平台文件。
- 正式测试覆盖现有 lifecycle、timeout、codec、zone 和 regression 测试；历史 `.txt` 探针继续作为审计证据，不整包搬入回归套件。
- 调整 `.github/workflows/ci.yaml`、`arch.yaml`、`release.yaml` 的门禁和 `v0.*` 标签支持；发布说明记录错误行为的纠正与明确的资源边界。
- 不新增第三方运行时依赖。状态、进度和既有证据统一关联 [GitHub #3](https://github.com/skill215/sctp/issues/3)。
