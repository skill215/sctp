## ADDED Requirements

### Requirement: Terminated single associations stop waiting
After an established single-association socket loses its association and another I/O operation consumes the reset, Read, SCTPRead, and wrapped Read SHALL return a terminal error instead of waiting indefinitely. Reads SHALL first attempt to deliver data still available from the kernel before deciding that the association is absent.

#### Scenario: Write consumes reset before read
- **WHEN** a real peer ABORT occurs, Write has returned ECONNRESET, and a subsequent read has no receive timeout
- **THEN** all three read entry points SHALL promptly return an error and valid count without requiring caller Close

#### Scenario: Read already waits when another operation consumes reset
- **WHEN** a read is already waiting and another write consumes the association-termination error
- **THEN** the waiting read SHALL finish, and close/queue controls SHALL still allow all goroutines to be cleaned up

#### Scenario: Data precedes termination
- **WHEN** kernel-readable data remains before termination
- **THEN** the system SHALL deliver the actual receive result rather than skip it through early fd closure or a cached error

### Requirement: Idle connections and shared endpoints remain usable
The system SHALL preserve waits for active idle connections and one-to-many endpoints. Association-loss detection SHALL have explicit single-association scope and correct ABI; general EAGAIN or arbitrary EINVAL must not be treated as EOF.

#### Scenario: Connected but temporarily idle
- **WHEN** an active single association returns EAGAIN and a valid status query succeeds
- **THEN** the read SHALL wait for payload, notification, timeout, or closure without busy-waiting or false EOF

#### Scenario: One-to-many has no active associations
- **WHEN** an assoc_id=0 status query fails on a one-to-many endpoint
- **THEN** the system SHALL retain the endpoint for future or other associations rather than mark the entire endpoint terminated

#### Scenario: Library peeloff creates an exclusive connection
- **WHEN** a socket returned by library PeelOff still has SO_TYPE SOCK_SEQPACKET
- **THEN** the system SHALL retain its single-association provenance and apply the terminal-read contract

### Requirement: Polling deadlines and close behavior remain compatible
The system SHALL retain nonblocking fds, directional gates, one timeout snapshot per operation, and Close wakeups. Notification handling and termination queries SHALL share the same read budget.

#### Scenario: Notifications consume the read budget
- **WHEN** a read handles multiple notifications without receiving payload
- **THEN** it SHALL time out at the original budget rather than restart the timer

#### Scenario: Close interrupts queued and active I/O
- **WHEN** a connection closes with active and queued reads/writes
- **THEN** all waits SHALL be able to finish without accidentally operating on a reused closed-fd number

### Requirement: Optional Accept normalization preserves errors
If #13 is implemented, Accept SHALL return a nil net.Conn and the original error on failure, and a valid connection on success. Release acceptance SHALL classify this as P3 non-blocking; if deferred, it SHALL explicitly record that it remains unimplemented rather than close it as fixed.

#### Scenario: Closed listener with the optional normalization implemented
- **WHEN** Accept on a closed listener returns an error
- **THEN** the connection SHALL be a nil interface and the original error SHALL be preserved

#### Scenario: Optional change is deferred
- **WHEN** #13 is deferred separately and all 14 blocking defects meet acceptance
- **THEN** the release checklist SHALL retain its non-blocking classification rather than treat this convention difference as a failed defect fix
