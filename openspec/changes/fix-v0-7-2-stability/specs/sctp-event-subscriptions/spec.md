## ADDED Requirements

### Requirement: Wrapping preserves configured events
Wrapper construction SHALL preserve existing subscriptions and add DATA_IO. Public SubscribeEvents SHALL retain replacement-mask semantics. Library wrapper merging and public subscription updates SHALL serialize as complete configuration operations.

#### Scenario: Existing shutdown handler
- **WHEN** a connection subscribes to SHUTDOWN with a handler and is then wrapped
- **THEN** existing events SHALL remain subscribed and actual shutdown notifications SHALL continue reaching the handler

#### Scenario: Repeated wrapping and concurrent library configuration
- **WHEN** DATA_IO is already enabled, wrappers are constructed repeatedly, or library event configuration runs concurrently
- **THEN** the system SHALL avoid data races and produce a complete configuration ordering without lost RMW updates

### Requirement: Setup failures are observable without changing ownership
The wrapper constructor, which has no error return, SHALL retain subscription-query/update failures. Subsequent Read/Write SHALL report initialization errors before I/O. Close SHALL always delegate resource cleanup to the underlying connection rather than skip it because of initErr.

#### Scenario: Query or update fails
- **WHEN** a subscription operation fails during construction
- **THEN** the first read/write SHALL return that failure with a zero count rather than pretend metadata is enabled

#### Scenario: Callback reentrancy and cleanup
- **WHEN** a handler queries configuration or closes the connection, or a caller closes a wrapper whose configuration failed
- **THEN** the system SHALL avoid deadlock from a configuration lock spanning I/O/handlers and SHALL release resources correctly
