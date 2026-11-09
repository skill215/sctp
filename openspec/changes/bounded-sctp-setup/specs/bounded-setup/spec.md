## ADDED Requirements

### Requirement: Cancellable handoff
Accept SHALL use the existing read gate, mutable deadlines and synchronous hook retirement; cancellation before publication SHALL close the child, while cancellation after publication SHALL leave it usable.
#### Scenario: Cancel then reuse
- **WHEN** an idle accept is cancelled
- **THEN** it returns the context cause with no owner and the listener can accept again.
### Requirement: Bounded setup and public socket capabilities
Setup SHALL validate combined N/Z/D before allocation/resolution/fd creation, own all inputs, reject callbacks and use numeric ports and one absolute context. BoundAddr SHALL preserve explicit wildcard and timeout updates SHALL expose partial failure.
#### Scenario: Over budget
- **WHEN** either endpoint causes a combined limit violation
- **THEN** construction fails before DNS or socket creation.
### Requirement: Versioned resource ledger
SetupRequirements SHALL perform no I/O and expose dialer/listener/establishment/connection peaks with explicit concurrency assumptions, owned parser and included scratch.
#### Scenario: Invalid configuration
- **WHEN** limits or platform are unsupported
- **THEN** the query returns an error, never a false zero support claim.
