## ADDED Requirements

### Requirement: Address inputs remain owned by callers
Listen/Dial 及 Ext/SocketConfig 入口 SHALL 使用内部地址副本准备通配地址，不修改调用方对象、IPAddrs 底层存储或 net.IP 字节。共享只读地址的并发调用 SHALL 不因库写入触发 race。

#### Scenario: Shared wildcard address
- **WHEN** 多个调用共用空 IPAddrs 的地址
- **THEN** 输入 SHALL 保持不变，所有内部通配补齐 SHALL 发生在独立副本

#### Scenario: Spare-capacity alias
- **WHEN** 调用方切片 len=0、cap>0 且底层存储另有别名
- **THEN** 内部 append SHALL 不修改该别名内容

#### Scenario: Control callback observes the address
- **WHEN** 传入 nil、Port-only 或显式本地地址
- **THEN** Control SHALL 看到补齐通配 IP 之前的既有字符串语义，绑定仍使用内部副本

### Requirement: Invalid endpoint inputs fail before side effects
高层 Listen/Dial SHALL 在 socket 创建和 Control 之前完成 network、端口及必需远端地址校验。SCTPBind/SCTPConnect SHALL 在内核操作前拒绝非法结构化地址。端口 SHALL 位于 0..65535，nil raddr SHALL 返回错误。

#### Scenario: Invalid structured ports
- **WHEN** 端口为负数、65536 或更大的 int
- **THEN** 系统 SHALL 返回地址错误，不能静默绑定或连接截断后的端口

#### Scenario: Missing remote address
- **WHEN** DialSCTP、DialSCTPExt 或 SocketConfig.Dial 的 raddr 为 nil
- **THEN** 系统 SHALL 返回错误而非 panic，不创建 socket，不调用 Control

#### Scenario: Valid wildcard and ephemeral port
- **WHEN** Listen 使用合法 nil/通配本地地址或本地端口 0
- **THEN** 系统 SHALL 保留自动绑定语义；Dial 的 nil laddr SHALL 保留自动选择本地地址

### Requirement: Multihomed text never discards explicit prefixes
多地址文本每段 SHALL 有 host；单段合法通配用法 SHALL 保留。解析器 SHALL 拒绝前导、中间或末段缺 host，不能用空末段清空已解析地址。

#### Scenario: Empty segment in a multihomed endpoint
- **WHEN** 输入为 127.0.0.1/:port、IPv6 对应形式或其他空 host 段
- **THEN** 解析 SHALL 返回错误，不产生扩大监听范围的通配地址

#### Scenario: Valid address and zone forms
- **WHEN** 输入是单段 :port、合法 IPv4/IPv6 多地址或带 zone 的地址
- **THEN** 既有解析行为 SHALL 保持，数字接口名与数字索引冲突 SHALL 保持名称优先

### Requirement: Address queries grow within an explicit budget
本地/远端地址查询 SHALL 从 4 KiB 地址区按 ENOMEM 翻倍到最多 1 MiB，每次重置输入头和 uint32 optlen。系统 SHALL 只在成功后解码完整列表，保留 local/peer optlen 的差异；失败和超预算 SHALL 返回错误，不能返回残缺列表。

#### Scenario: Valid list exceeds the old fixed buffer
- **WHEN** 合法 300 IPv4 地址已绑定，列表需要 4800 字节
- **THEN** 地址查询 SHALL 返回全部地址，Listener.Addr SHALL 非 nil，小列表控制组 SHALL 保持正确

#### Scenario: Memory error persists at the budget
- **WHEN** 查询在 1 MiB 预算仍返回 ENOMEM
- **THEN** 系统 SHALL 停止重试，返回包含容量上下文且 errors.Is(ENOMEM) 成立的错误；Addr SHALL 不伪造完整列表

#### Scenario: Mixed families or malformed lengths
- **WHEN** 成功缓冲区含混合 family，或长度/数量不合法
- **THEN** 系统 SHALL 逐地址解码合法列表，对越界、截断和未知 family 返回错误，不越界访问
