## ADDED Requirements

### Requirement: Fixed metadata continuity and stream bounds
Message reads SHALL retain one fixed cursor across notifications until EOR, validate assoc/stream/PPID/ordered and ordered SSN, and latch unusable reads preserving actual evidence on violations. Send SID SHALL use actual Ostreams; receive SHALL use actual Instreams.
#### Scenario: Notification between fragments
- **WHEN** a notification arrives during a fragmented message
- **THEN** it is returned without clearing the cursor and subsequent inconsistent data fails closed.
#### Scenario: Complete messages change stream
- **WHEN** an EOR is followed by another complete message on a different valid stream/PPID
- **THEN** it is accepted, including PPID zero.
