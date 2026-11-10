## ADDED Requirements

### Requirement: Public portability APIs compile consistently
ErrUnsupported SHALL be exported on every tested target, and both Conn and Listener SHALL provide SyscallConn. Unsupported targets SHALL return a nil raw connection and identifiable ErrUnsupported rather than fail downstream builds because a public member is missing.

#### Scenario: External consumers compile on supported matrix targets
- **WHEN** the same consumer references ErrUnsupported or asserts that Conn/Listener implement syscall.Conn
- **THEN** it SHALL compile for Linux/amd64, Linux/386, Darwin/amd64, FreeBSD/amd64, and Windows/amd64 on both Go versions

#### Scenario: Unsupported raw API is called
- **WHEN** SyscallConn is called on an unsupported target
- **THEN** the raw connection SHALL be nil and errors.Is(ErrUnsupported) SHALL identify the error

### Requirement: Failed unsupported construction releases the supplied socket
Unsupported NewSCTPConn SHALL take ownership of a valid fd and release it immediately when initialization cannot complete, returning an object recording the platform error. Cleanup SHALL use the platform socket-close primitive, include fd=0, avoid truncating Windows handles, and never close that number again on later Close calls.

#### Scenario: Linux 386 real SCTP descriptor
- **WHEN** the Linux/386 constructor receives a real SCTP socket
- **THEN** the original fd SHALL be released before construction returns and later I/O SHALL report unsupported-platform errors

#### Scenario: Descriptor is reused
- **WHEN** the system reuses the number after construction fails and the old object is closed again or concurrently
- **THEN** the new resource SHALL remain valid and the old object SHALL not call native close again

#### Scenario: Descriptor zero or Windows socket handle
- **WHEN** a Unix child imports fd=0 or Windows imports a real Winsock socket handle
- **THEN** the correct socket resource SHALL be released without leaks from an fd>0 condition or an incorrect file-handle close operation

#### Scenario: Native release reports an error
- **WHEN** native close fails during initialization cleanup
- **THEN** initErr SHALL retain the error and later public I/O, SyscallConn, or Close SHALL return it while preserving ErrUnsupported identity; the system SHALL not blindly retry an fd that may already have been reused
