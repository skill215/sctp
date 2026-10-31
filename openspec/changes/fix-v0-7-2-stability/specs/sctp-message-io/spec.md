## ADDED Requirements

### Requirement: Zero-capacity reads do not consume input
活跃连接的空 Read/SCTPRead SHALL 返回零计数、nil error，不读取 socket 数据或通知；SCTPRead 的 metadata SHALL 为 nil。wrapper 长度不超过 32 的读缓冲区 SHALL 返回 0、EINVAL，且不调用底层接收。

#### Scenario: Empty payload buffer with queued data
- **WHEN** 对端已发送标记 payload，调用方使用 nil 或空切片读取
- **THEN** 返回计数 SHALL 为 0，下一次正常读取 SHALL 得到完整标记 payload

#### Scenario: Header-only wrapped read
- **WHEN** wrapped Read 的缓冲区长度恰好为 32
- **THEN** 系统 SHALL 返回 0、EINVAL，缓冲区与待接收 payload SHALL 保持不变

#### Scenario: Closed or failed connection
- **WHEN** 零长度操作的连接已关闭、为 nil 或初始化失败
- **THEN** 系统 SHALL 保留明确的对象错误，返回计数仍为 0

### Requirement: Empty sends never fabricate user bytes
所有空 Write/SCTPWrite SHALL 使用真实零载荷内核发送，保留显式及默认发送参数。内核拒绝普通空消息时，系统 SHALL 返回 0、原始错误，不向 peer 注入字节，也不将被拒绝的 wrapper 头计为消费成功。

#### Scenario: Metadata with empty user payload
- **WHEN** SCTPWrite 收到 nil payload 和普通 metadata
- **THEN** 返回值 SHALL 与原始零载荷 sendmsg 的计数/错误一致，peer SHALL 收不到额外的 0x00

#### Scenario: Header-only data frame
- **WHEN** wrapper 写入仅包含有效头部的数据帧
- **THEN** 底层拒绝时消费计数 SHALL 为 0 且保留错误，peer 的下一个标记 payload SHALL 不受前缀字节污染

#### Scenario: Default parameters carry control flags
- **WHEN** info=nil 且内核默认发送参数包含合法 ABORT 等控制
- **THEN** 系统 SHALL 让内核执行该控制，不把空写吞成无操作，也不通过用户态查询和重放默认参数制造竞态

### Requirement: Control sends preserve zero payload and Linux flags
系统 SHALL 将 SCTP_EOF 定义为 0x200；合法零载荷 EOF/ABORT SHALL 保留 ancillary data 并执行真正零 iov 的控制发送，不使用会补字节的 Go SendmsgN 分支。其他标志数值、PPID host-order 接口和错误重试预算 SHALL 保持。

#### Scenario: Valid one-to-many EOF
- **WHEN** 对适用 socket/关联发送空 payload 和 SCTP_EOF
- **THEN** 内核 SHALL 收到 0x200 与零载荷，peer SHALL 进入正常关闭，不收到伪造用户字节

#### Scenario: Valid abort control
- **WHEN** 发送合法 SCTP_ABORT 控制，或非空 abort reason
- **THEN** 系统 SHALL 保留控制及 reason 的内核语义，不把它吞成普通空写

#### Scenario: Kernel rejects a control request
- **WHEN** 内核拒绝控制组合或不适用的 socket/关联
- **THEN** 返回计数 SHALL 为 0 且保留内核错误，不报告成功，也不注入临时字节

#### Scenario: Metadata object is reused
- **WHEN** 多个非空发送共用同一只读 SndRcvInfo
- **THEN** 调用方 metadata SHALL 不被修改，PPID 和流编号 SHALL 正确到达对端

### Requirement: Read counts and metadata frames describe initialized bytes
Read、SCTPRead 和 wrapper 的返回计数 SHALL 非负且不超过调用方缓冲区。wrapper 成功计入的头部 SHALL 完整初始化；metadata 缺失时 SHALL 为全零头，不返回旧缓冲区内容。

#### Scenario: Reset, EOF or timeout before data
- **WHEN** 读取未取得有效数据就发生 reset、EOF、超时或关闭
- **THEN** wrapper SHALL 返回 0 和错误，io.ReadAll SHALL 不因负数或虚报头部计数 panic

#### Scenario: Notification has no metadata
- **WHEN** 无 handler 的 wrapper 收到 SENDER_DRY 等不带 metadata 的通知
- **THEN** 返回帧的前 32 字节 SHALL 为零，载荷 SHALL 保留实际通知内容

#### Scenario: Valid payload with a subsequent metadata error
- **WHEN** 已收到有效 payload，但随后 metadata 解析返回错误
- **THEN** wrapper SHALL 返回已初始化头部、有效 payload 的完整计数及该错误，不把前缀旧字节当成 payload
