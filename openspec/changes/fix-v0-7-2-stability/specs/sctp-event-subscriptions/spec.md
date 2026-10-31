## ADDED Requirements

### Requirement: Wrapping preserves configured events
wrapper 构造 SHALL 保留已有订阅并加入 DATA_IO。公开 SubscribeEvents SHALL 保持替换掩码语义，库内 wrapper 合并及公开订阅更新 SHALL 串行化为完整配置操作。

#### Scenario: Existing shutdown handler
- **WHEN** 连接已订阅 SHUTDOWN 且配置 handler，随后被包装
- **THEN** 原有事件 SHALL 保留，实际关闭通知 SHALL 继续投递到 handler

#### Scenario: Repeated wrapping and concurrent library configuration
- **WHEN** DATA_IO 已开启、重复构造 wrapper，或并发调用库内事件配置
- **THEN** 系统 SHALL 不产生 data race，更新结果 SHALL 对应完整的配置顺序，不出现 RMW 丢失更新

### Requirement: Setup failures are observable without changing ownership
无 error 返回值的 wrapper 构造器 SHALL 保存订阅查询/设置失败，后续 Read/Write SHALL 在 I/O 前返回初始化错误。Close SHALL 始终能委托底层连接清理，不被 initErr 跳过。

#### Scenario: Query or update fails
- **WHEN** 构造时订阅操作失败
- **THEN** 首个读写 SHALL 返回该失败且计数为零，不假装 metadata 已启用

#### Scenario: Callback reentrancy and cleanup
- **WHEN** handler 查询配置或关闭连接，或调用方关闭配置失败的 wrapper
- **THEN** 系统 SHALL 不因持有跨 I/O/handler 的配置锁死锁，资源 SHALL 正确释放
