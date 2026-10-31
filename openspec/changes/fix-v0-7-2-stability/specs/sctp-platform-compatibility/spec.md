## ADDED Requirements

### Requirement: Public portability APIs compile consistently
ErrUnsupported SHALL 在所有受测目标导出，Conn 和 Listener SHALL 都具备 SyscallConn 方法。unsupported 目标 SHALL 返回 nil raw connection 和可识别的 ErrUnsupported，不能因缺少公开成员导致下游编译失败。

#### Scenario: External consumers compile on supported matrix targets
- **WHEN** 同一消费者引用 ErrUnsupported 或声明 Conn/Listener 实现 syscall.Conn
- **THEN** Linux/amd64、Linux/386、Darwin/amd64、FreeBSD/amd64、Windows/amd64 在两个 Go 版本下 SHALL 均可编译

#### Scenario: Unsupported raw API is called
- **WHEN** unsupported 目标上调用 SyscallConn
- **THEN** 返回 raw connection SHALL 为 nil，error SHALL 可用 errors.Is(ErrUnsupported) 识别

### Requirement: Failed unsupported construction releases the supplied socket
unsupported NewSCTPConn SHALL 接管有效 fd 并在初始化无法完成时立即释放，返回记录平台错误的对象。释放 SHALL 使用平台 socket 关闭原语，覆盖 fd=0，不截断 Windows 句柄，也不在后续 Close 重复关闭该数字。

#### Scenario: Linux 386 real SCTP descriptor
- **WHEN** Linux/386 构造器接收真实 SCTP socket
- **THEN** 构造完成后原 fd SHALL 已释放，后续 I/O SHALL 返回平台不支持错误

#### Scenario: Descriptor is reused
- **WHEN** 构造失败后系统将同一数字分配给新资源，旧对象再次或并发 Close
- **THEN** 新资源 SHALL 保持有效，旧对象 SHALL 不重新执行 native close

#### Scenario: Descriptor zero or Windows socket handle
- **WHEN** Unix 子进程导入 fd=0，或 Windows 导入实际 Winsock socket handle
- **THEN** 正确 socket 资源 SHALL 被释放，不能因 fd>0 条件或文件句柄关闭方式错误而泄漏

#### Scenario: Native release reports an error
- **WHEN** 初始化清理的 native close 返回失败
- **THEN** 错误 SHALL 保留在 initErr 中并由后续公开 I/O、SyscallConn 或 Close 返回，仍可识别 ErrUnsupported，系统 SHALL 不盲目重试可能已复用的 fd
