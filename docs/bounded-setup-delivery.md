# A–E reviewed delivery (2026-11-09)

A–E is implemented and locally reviewed on top of the U2–U5 commit
`1649569dd9b851b95664fef36f575c88a3547a78`. The review fixed one P2 issue and
found no remaining blocking issue in the reviewed scope. The exact pushed SHA,
installable module version/checksums and remote CI outcomes are recorded on
[PR #30](https://github.com/skill215/sctp/pull/30). This delivery is included in the v0.8.0 release candidate.
[Release tracking #31](https://github.com/skill215/sctp/issues/31) records the
actual merge/tag/release status, final module checksums and no-replace consumer
verification; pre-release candidate evidence alone is not publication evidence.
The [release notes](../releases/v0.8.0.md) describe compatibility against v0.7.4.
No diameterkit dependency change is included.

## A–E status and API mapping

| Item | Implementation | Local status |
| --- | --- | --- |
| A | AcceptSCTPContext and listener SetDeadline; shared read gate, final handoff arbitration and hook retirement | Passed |
| B | AddressLimits/SetupConfig, NewDialer/DialContext and ListenContext; combined N/Z/D, owned inputs and one context | Passed |
| C | SetSocketTimeouts and owned BoundAddr; partial-update error, wildcard/ephemeral-port handling | Passed |
| D | SetupRequirements/SetupResourceRequirements; versioned architecture-specific logical ledger | Passed |
| E | Fixed metadata cursor, EndOfRecord, negotiated directional stream checks and actual send evidence | Passed |

All proposed API names were retained. Existing ReadEvent, WriteMsgContext,
ParseNotification, GetStatus, GetMessageOptions and MessageIORequirements are
reused. [Full API contracts and compatibility](bounded-setup.md) include error
identities, caller ownership, limits, unsupported builds and snapshot lifetime.

## Review finding and resolution

A terminal error from the consuming recvmsg after a successful peek could return
zero bytes without CloseRequired even when a previous fragment remained active.
The new regression reproduced this with ECONNRESET before the fix. The fix keeps
the original errno and zero-byte/non-consumed result, adds ErrMessageBoundary and
CloseRequired, retains the unfinished cursor and makes subsequent reads return
ErrReadUnusable. The regression covers ECONNRESET and EIO. Unconsumed cancellation
continues to preserve a resumable message, as required by the existing contract.

The review also checked accept/Close/deadline handoff, cancellation hook cleanup,
input limits and ownership, socket timeout conversion/partial failure, bind
snapshot behavior, resource overlap, restart/partial-abort handling, actual stream
counts, byte order and short-write evidence. No additional blocking finding was
identified. The [machine review record](evidence/bounded-setup-review/review.json)
includes source hashes and the reproduced failure.

## Validation

| Check | Result |
| --- | --- |
| CGO_ENABLED=0 complete native suite | 352 test/subtest/example events passed; zero failures/skips |
| Complete race suite | Same 352 events passed; zero failures/skips |
| New lifecycle/message tests, race ×20 | 660 events passed; zero failures/skips |
| CGO0 vet and build | Passed |
| Existing independent API compile fixture | Linux amd64/arm64/386, Darwin/FreeBSD/Windows amd64 passed |
| README native examples | Payload/stream/PPID and timeout passed |

[Commands](evidence/bounded-setup-review/commands.txt),
[native log](evidence/bounded-setup-review/native.jsonl.gz),
[race log](evidence/bounded-setup-review/race.jsonl.gz) and
[repeated race log](evidence/bounded-setup-review/lifecycle-race.jsonl.gz) record the
reviewed source. Compressed logs preserve all test events, including skip/failure
classification. The compile fixture uses a development replace; it is not remote
module provenance. A fresh GOWORK=off/no-replace consumer of the exact pushed
commit is recorded separately on the PR.

The existing GitHub matrix runs two Ubuntu environments × Go1.26/1.27, 10 race
rounds per native job, checkptr, low-GC race, five million fuzz executions per
native job, required namespaces, native Windows/Linux386 unsupported checks and
six QEMU ABI jobs. Read the checks for the exact commit on the PR for their outcome.
QEMU is ABI evidence, not native SCTP on those architectures. The existing wide
association-ID test is inapplicable on 32-bit ARM/MIPS and reports explicit skips;
those events are not counted as passed. No diameterkit migration test is claimed.

## Resource ledger and compatibility

Ledger: `bounded-setup-v2/linux/<GOARCH>`. For Go1.26.10/Linux/amd64 and
N16/Z256/D4096: DialerBytes=8960, ListenerBytes=48524, EstablishBytes=78380,
ConnectionBytes=9516. Reserve `DialerBytes + max(EstablishBytes,ConnectionBytes)`
=87340 per candidate, plus the listener and any old retiring connection.
Listener + old connection + new candidate is 145380 logical bytes.

ConnectionBytes already includes read/write/peek scratch 480/280/8 and one fixed
parsed Notification. Caller prefix/payload/notification/OOB/discard buffers and
adapter/business/control state are extra. See the [ledger derivation and
assumptions](bounded-setup.md#d-machine-readable-ledger): single reader/writer/
Accept, no queued upstream I/O, controlled contexts and bounded deadline updates;
runtime/allocator/stacks, kernel queues and DNS internals excluded. Release only
after actual operation/hook retirement. No RSS guarantee is made.

No existing public struct gained fields relative to U2–U5. Legacy zero-value
SocketConfig, nil Messages and old resolver grammar are retained. Message mode
now rejects missing/inconsistent metadata and invalid negotiated streams. Restart
invalidates this generation; an active partial-delivery abort latches reads while
preserving the successful raw notification. Old raw descriptor mutation remains
outside these guarantees. Unsupported platforms expose explicit errors.

## Consumer and diameterkit handoff

The [standalone consumer](examples/bounded-consumer/main.go.txt) covers resource
query, listen/dial/accept, deadlines, socket-timeout cleanup, fragmented stream1
PPID0/46, notifications, cancellation after successful handoff and Close. Create
an independent module, require the exact remote version recorded on PR #30, and
run with GOWORK=off and no replace. Earlier local file-proxy candidates (including
ad4ef719e7b5) are historical evidence and are superseded by the reviewed remote
candidate; their module sums must not be reused for the new version.

The next diameterkit session should:

1. After #31 confirms publication and final consumer verification, pin v0.8.0
   and its actual go.sum; do not reuse the earlier pseudo-version checksums.
2. Delete 20ms accept polling; call AcceptSCTPContext/SetDeadline.
3. Delete direct setsockopt/getsockname; call SetSocketTimeouts/BoundAddr.
4. Delete duplicated N/Z/D parsing/copying and use the bounded setup facade.
5. Delete hardcoded 8192/copied upstream formulas; query the versioned ledger,
   preserving adapter-owned budgets and old/new candidate overlap.
6. Remove duplicated SCTP metadata continuity checks after E acceptance; keep
   Diameter length/EOR and PPID policy, whole-frame deadlines and notification limits.
7. Retain transport wrappers, ConnectionID/Sequence/first cause, CEA/DWR barriers,
   stream policy, Peer election/Watchdog/REOPEN/DPR/DPA, pending/attempt completion,
   Node admission and retirement. Rerun batches 64/65 Base/S6c, budgets, cancel/Close,
   crossed election, REOPEN, once-only completion and no automatic business replay.

The main working tree's earlier unrelated/v1 planning files are preserved locally
and excluded from this PR increment. Only the bounded enhancement, its tests,
contracts and review evidence are added to the existing U2–U5 branch.
