## ADDED Requirements

### Requirement: Address inputs remain owned by callers
Listen/Dial and Ext/SocketConfig entry points SHALL prepare wildcard addresses using internal copies without modifying caller objects, IPAddrs backing storage, or net.IP bytes. Concurrent calls sharing read-only addresses SHALL not race because of library writes.

#### Scenario: Shared wildcard address
- **WHEN** multiple calls share an address with empty IPAddrs
- **THEN** the input SHALL remain unchanged and all internal wildcard completion SHALL use independent copies

#### Scenario: Spare-capacity alias
- **WHEN** a caller slice has len=0, cap>0, and an alias to its backing storage
- **THEN** internal append operations SHALL not modify the aliased contents

#### Scenario: Control callback observes the address
- **WHEN** the input is nil, Port-only, or an explicit local address
- **THEN** Control SHALL observe the existing string semantics before wildcard IP completion, while binding uses an internal copy

### Requirement: Invalid endpoint inputs fail before side effects
High-level Listen/Dial SHALL validate the network, port, and required remote address before socket creation and Control. SCTPBind/SCTPConnect SHALL reject invalid structured addresses before kernel operations. Ports SHALL be within 0..65535, and nil raddr SHALL return an error.

#### Scenario: Invalid structured ports
- **WHEN** a port is negative, 65536, or a larger int
- **THEN** the system SHALL return an address error rather than silently bind or connect to a truncated port

#### Scenario: Missing remote address
- **WHEN** raddr is nil in DialSCTP, DialSCTPExt, or SocketConfig.Dial
- **THEN** the system SHALL return an error instead of panicking, create no socket, and not call Control

#### Scenario: Valid wildcard and ephemeral port
- **WHEN** Listen uses a valid nil/wildcard local address or local port 0
- **THEN** the system SHALL preserve automatic binding; nil laddr in Dial SHALL preserve automatic local-address selection

### Requirement: Multihomed text never discards explicit prefixes
Every segment of multihomed text SHALL contain a host; valid single-segment wildcard usage SHALL remain supported. The parser SHALL reject missing hosts in the first, middle, or final segment and must not discard parsed addresses because the final segment is empty.

#### Scenario: Empty segment in a multihomed endpoint
- **WHEN** input is 127.0.0.1/:port, its IPv6 equivalent, or another form containing an empty host segment
- **THEN** parsing SHALL return an error rather than produce a wildcard that broadens the listen scope

#### Scenario: Valid address and zone forms
- **WHEN** input is a single :port, valid IPv4/IPv6 multihoming, or an address with a zone
- **THEN** existing parsing SHALL remain intact, and numeric interface-name/index conflicts SHALL retain name precedence

### Requirement: Address queries grow within an explicit budget
Local/remote address queries SHALL double the address area from 4 KiB on ENOMEM up to 1 MiB, resetting the input header and uint32 optlen each time. The system SHALL decode a complete list only after success and preserve local/peer optlen differences. Failures and budget exhaustion SHALL return errors, never incomplete lists.

#### Scenario: Valid list exceeds the old fixed buffer
- **WHEN** 300 valid IPv4 addresses are bound and the list requires 4800 bytes
- **THEN** the query SHALL return all addresses, Listener.Addr SHALL be non-nil, and small-list controls SHALL remain correct

#### Scenario: Memory error persists at the budget
- **WHEN** the query still returns ENOMEM at the 1 MiB budget
- **THEN** the system SHALL stop retrying and return capacity context with errors.Is(ENOMEM) true; Addr SHALL not fabricate a complete list

#### Scenario: Mixed families or malformed lengths
- **WHEN** a successful buffer contains mixed families or invalid lengths/counts
- **THEN** the system SHALL decode valid lists address by address and reject out-of-range lengths, truncation, and unknown families without out-of-bounds access
