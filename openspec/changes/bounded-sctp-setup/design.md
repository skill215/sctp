## Context
Incremental A–E scope from ../diameterkit/doc/sctp-upstream-improvement-plan.md; pure Go 1.26. Existing U2–U5 implementations are the base.

## Goals / Non-Goals
Goals: eliminate duplicated generic setup and message integrity code. Non-goals: Diameter policy, v1 rewrite, one-to-many, new platforms, reconnect/replay.

## Decisions
Accept uses the existing pollContext read owner through child initialization and final publication. The listener deadline lock arbitrates publication against Close/deadline/context observation. Temporary hooks retire before returning. No polling timer or worker queue is introduced.
BoundAddr stores a clone of explicit bind input plus getsockname port; imported listeners report unavailable. Socket timeout changes validate both durations before mutation and report partial updates.
Bounded setup preflights combined N/Z/D before cloning, splitting or DNS, then uses the existing resolver and socket constructors with one original context. All retained strings are cloned. Required message options reuse existing verification.
Resource queries use compiled structure sizes plus explicitly versioned logical allowances and bounded address workspaces. They exclude runtime/kernel/DNS internals and require bounded caller concurrency.
Message mode uses one read-gate-protected fixed metadata cursor, EOR clearing, actual directional stream counts captured at establishment. No payload retention. Restart invalidates this generation; application closes it.

## Risks / Trade-offs
Stricter message validation rejects previously exposed inconsistent data; legacy nil-Messages mode stays unchanged. No exported struct fields are added to existing configuration types. Raw descriptor mutation and dynamic stream reset are outside the contract. Native CI and remote install provenance must be reported separately from local tests.

## Migration Plan
Deliver A+C, then A–D candidate evidence, then E and final evidence. diameterkit migration occurs in another session.
