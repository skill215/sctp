## ADDED Requirements

### Requirement: Zero-capacity reads do not consume input
Empty Read/SCTPRead on an active connection SHALL return a zero count and nil error without consuming socket data or notifications; SCTPRead metadata SHALL be nil. Wrapper read buffers of length at most 32 SHALL return 0, EINVAL without invoking receive.

#### Scenario: Empty payload buffer with queued data
- **WHEN** the peer has sent a marker payload and the caller reads using nil or an empty slice
- **THEN** the count SHALL be 0 and the next normal read SHALL receive the entire marker payload

#### Scenario: Header-only wrapped read
- **WHEN** a wrapped Read buffer is exactly 32 bytes long
- **THEN** the system SHALL return 0, EINVAL and leave both the buffer and pending payload unchanged

#### Scenario: Closed or failed connection
- **WHEN** a zero-length operation targets a closed, nil, or initialization-failed connection
- **THEN** the system SHALL preserve the explicit object error and still return a zero count

### Requirement: Empty sends never fabricate user bytes
All empty Write/SCTPWrite calls SHALL perform a real zero-payload kernel send, preserving explicit and default send parameters. If the kernel rejects an ordinary empty message, the system SHALL return 0 and the original error without injecting peer bytes or counting a rejected wrapper header as consumed.

#### Scenario: Metadata with empty user payload
- **WHEN** SCTPWrite receives nil payload and ordinary metadata
- **THEN** its count/error SHALL match raw zero-payload sendmsg, and the peer SHALL receive no extra 0x00

#### Scenario: Header-only data frame
- **WHEN** the wrapper writes a data frame containing only a valid header
- **THEN** a rejected underlying send SHALL report zero consumption and preserve the error; no prefix byte SHALL contaminate the next marker payload

#### Scenario: Default parameters carry control flags
- **WHEN** info=nil and kernel default send parameters contain valid controls such as ABORT
- **THEN** the system SHALL let the kernel execute that control rather than swallow the empty write as a no-op or introduce a race by querying/replaying defaults in userspace

### Requirement: Control sends preserve zero payload and Linux flags
The system SHALL define SCTP_EOF as 0x200. Valid zero-payload EOF/ABORT sends SHALL preserve ancillary data and use a real zero-iov control send rather than Go SendmsgN's padding branch. Other flag values, the host-order PPID interface, and error-retry budgets SHALL remain unchanged.

#### Scenario: Valid one-to-many EOF
- **WHEN** empty payload and SCTP_EOF are sent on an applicable socket/association
- **THEN** the kernel SHALL receive 0x200 with zero payload, and the peer SHALL enter orderly shutdown without fabricated user bytes

#### Scenario: Valid abort control
- **WHEN** a valid SCTP_ABORT control or nonempty abort reason is sent
- **THEN** the system SHALL preserve kernel control/reason semantics rather than swallow it as an ordinary empty write

#### Scenario: Kernel rejects a control request
- **WHEN** the kernel rejects a control combination or an inapplicable socket/association
- **THEN** the count SHALL be 0 with the kernel error preserved, without reporting success or injecting a temporary byte

#### Scenario: Metadata object is reused
- **WHEN** multiple nonempty sends share one read-only SndRcvInfo
- **THEN** caller metadata SHALL remain unchanged and PPID/stream number SHALL reach the peer correctly

### Requirement: Read counts and metadata frames describe initialized bytes
Read, SCTPRead, and wrapper counts SHALL be nonnegative and no greater than the caller buffer. Any counted wrapper header SHALL be fully initialized; missing metadata SHALL produce a zero header rather than stale buffer contents.

#### Scenario: Reset, EOF or timeout before data
- **WHEN** reset, EOF, timeout, or closure occurs before valid data is received
- **THEN** the wrapper SHALL return 0 and the error, and io.ReadAll SHALL not panic because of a negative count or falsely counted header

#### Scenario: Notification has no metadata
- **WHEN** a wrapper without a handler receives a metadata-free notification such as SENDER_DRY
- **THEN** the first 32 bytes of the returned frame SHALL be zero and its payload SHALL preserve the actual notification

#### Scenario: Valid payload with a subsequent metadata error
- **WHEN** valid payload has arrived but subsequent metadata parsing fails
- **THEN** the wrapper SHALL return the complete count of initialized header plus valid payload and the error, without treating stale prefix bytes as payload
