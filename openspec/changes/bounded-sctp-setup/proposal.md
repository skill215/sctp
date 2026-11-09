## Why
Consumers currently duplicate accept polling, socket syscalls, address bounds and source-specific resource formulas. Move these generic guarantees into SCTP while retaining all Diameter policy in diameterkit.

## What Changes
- A+C: cancellable accept, listener deadline and publication gate, socket timeout setter and frozen binding snapshot.
- B+D: bounded owned setup facade and versioned pure resource query.
- E: fixed message continuity cursor, negotiated directional stream validation and portable EOR.
- Preserve legacy entrypoint grammar and nil message mode; no v1 rewrite, new platform or dependency.

## Capabilities
### New Capabilities
- `bounded-setup`: A–D lifecycle, bounded inputs and logical resource ledger.
- `message-continuity`: E fixed cursor and directional stream checks.
### Modified Capabilities
None; the separate v1 proposal remains unimplemented.

## Impact
Pure Go library, supported Linux targets, unsupported stubs, tests and docs. diameterkit is read-only. Baseline HEAD 62ad388 plus existing dirty U2–U5 work; ten handoff hashes verified equal to installed 1649569 candidate, not a whole-tree identity claim.
