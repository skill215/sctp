## ADDED Requirements

### Requirement: Terminated single associations stop waiting
系统 SHALL 在已建立的单关联 socket 失去关联、reset 已被其他 I/O 消费后，使 Read、SCTPRead 和 wrapped Read 返回终止错误，而不是无限等待。读取 SHALL 先尝试交付内核仍可提供的数据，再判定无关联。

#### Scenario: Write consumes reset before read
- **WHEN** 对端真实 ABORT 且 Write 已返回 ECONNRESET，后续读没有接收超时
- **THEN** 三个读入口 SHALL 及时返回错误及合法计数，不依赖调用方 Close 才结束

#### Scenario: Read already waits when another operation consumes reset
- **WHEN** 读操作已经等待，另一个写操作消费关联终止错误
- **THEN** 等待读 SHALL 结束，且关闭/排队控制仍可清理所有 goroutine

#### Scenario: Data precedes termination
- **WHEN** 终止前还有内核可读数据
- **THEN** 系统 SHALL 按实际接收结果交付数据，不通过提前关闭 fd 或缓存错误跳过它

### Requirement: Idle connections and shared endpoints remain usable
系统 SHALL 保持活跃空闲连接和 one-to-many 端点的等待语义；关联失效判定 SHALL 有明确的单关联作用域及正确 ABI，不将一般 EAGAIN 或任意 EINVAL 当作 EOF。

#### Scenario: Connected but temporarily idle
- **WHEN** 活跃单关联返回 EAGAIN，合法状态查询仍成功
- **THEN** 读 SHALL 等待 payload、通知、超时或关闭，不忙等、不返回假 EOF

#### Scenario: One-to-many has no active associations
- **WHEN** one-to-many 端点的 assoc_id=0 状态查询失败
- **THEN** 系统 SHALL 保留端点以服务未来或其他关联，不把它整体标为终止

#### Scenario: Library peeloff creates an exclusive connection
- **WHEN** 本库 PeelOff 返回的 socket 在 SO_TYPE 上仍为 SOCK_SEQPACKET
- **THEN** 系统 SHALL 保留其单关联来源，并适用终止读取契约

### Requirement: Polling deadlines and close behavior remain compatible
系统 SHALL 保持非阻塞 fd、分方向 gate、一次操作一个超时快照及 Close 唤醒行为。通知处理和终止查询 SHALL 使用同一读取预算。

#### Scenario: Notifications consume the read budget
- **WHEN** 一次读取处理多次通知后仍没有 payload
- **THEN** 系统 SHALL 在原始预算到期后返回超时，不重新开始计时

#### Scenario: Close interrupts queued and active I/O
- **WHEN** 连接关闭时存在活跃及排队的读写
- **THEN** 所有等待 SHALL 可结束，且没有已关闭 fd 的复用误操作

### Requirement: Optional Accept normalization preserves errors
若实施 #13，Accept SHALL 在错误时返回 nil net.Conn 和原始错误，成功时返回有效连接。发布验收 SHALL 将此项标为 P3 非阻塞；若延期，SHALL 明确记录未实施，不能按已修复关闭。

#### Scenario: Closed listener with the optional normalization implemented
- **WHEN** 已关闭 listener 的 Accept 返回错误
- **THEN** 返回连接 SHALL 是 nil 接口，原始 error SHALL 保留

#### Scenario: Optional change is deferred
- **WHEN** #13 单独延期且 14 条阻塞缺陷已满足验收
- **THEN** 发布清单 SHALL 保留其非阻塞状态，不将此约定习惯差异当作缺陷修复失败
