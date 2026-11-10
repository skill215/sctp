# I/O interface design: synchronous APIs over nonblocking descriptors

Decision date: 2026-11-10. Baseline: published v0.8.0.
Status: accepted design direction; documentation only. Optional application
channel adapters described here are not new SCTP library APIs or implemented
features. Their acceptance requirements remain to be demonstrated by each
implementation.

## 1. Decision

Keep the existing public interfaces and nonblocking descriptor implementation.
Synchronous calls wait through the Go runtime poller and retain their documented
context, deadline, Close, result, and ownership semantics.

- Preserve `Accept`, `AcceptSCTP`, legacy I/O, and wrapper compatibility.
- Prefer `AcceptSCTPContext`, `ReadEvent`, and `WriteMsgContext` for new
  cancellable integrations. Message mode remains explicitly selected.
- Do not add `AcceptChan`, `ReadChan`, `NotifyChan`, readiness channels, or
  `TryAccept` as part of adapting to nonblocking descriptors.
- Use channels at the adapter/session boundary only when the application's
  scheduling needs asynchronous delivery. Reuse existing I/O workers where
  possible; a channel does not justify an additional worker layer by itself.
- Do not restore blocking mode on library-owned descriptors or add periodic
  EAGAIN polling in consumers.

This decision preserves the v0.8.0 contracts. It does not promise identical
behavior to 0.4: see the [compatibility audit](compatibility-v0.4-v0.8.0.md).
No implementation change, new release, or tag replacement is required by this
documentation decision.

## 2. Why nonblocking fd does not require an asynchronous API

| Model | Where waiting happens | Consequence |
| --- | --- | --- |
| Nonblocking fd, synchronous public call | Calling goroutine waits through the runtime poller | Current design; no dedicated OS thread per idle poller wait |
| Blocking fd, blocking syscall | An OS thread can remain in accept/receive/send | Incompatible with assumptions of the current poller callbacks and cancellation mechanism |
| Channel with a background worker | Worker waits in the same underlying API; consumer receives a result | Moves the wait and introduces lifecycle, queue, and ownership obligations |

The listener's `O_NONBLOCK` determines whether an empty accept syscall waits.
The `SOCK_NONBLOCK` flag passed to accept4 sets the new child's mode. Since 0.5,
both are nonblocking, while public Accept continues to wait for a connection.
EAGAIN requests a poller wait; it is not normally exposed as an empty-queue
result from public Accept. Relative SO-timeout errors remain a separate case.
See [Linux accept semantics](https://man7.org/linux/man-pages/man2/accept.2.html)
and [Go's poll implementation](https://go.dev/src/internal/poll/fd_unix.go).

The current [accept implementation](../setup_socket_linux.go) covers accept4,
child initialization, and publication with one read owner. The
[wait controller](../socket_deadline.go) observes cancellation, deadlines, and
Close and retires started cancellation hooks before returning. Accepting an fd
in the kernel is not yet successful publication to the caller.

Setting a managed fd back to blocking can trap a syscall inside a pinned raw
callback. Poller wakeups cannot make that syscall return according to the
existing deadline/cancellation design, and Close can wait for the callback to
release its reference. A Go context cannot forcibly interrupt arbitrary user
code either. Neither issue is fixed by returning a channel.

## 3. Problems to address above the I/O API

| Integration pattern | Failure risk | Required design response |
| --- | --- | --- |
| Session control loop calls a waiting Read/Accept directly | Heartbeats, shutdown, and control commands cannot be processed by that goroutine | Separate the control loop from its bounded I/O execution |
| Background context without a shutdown path | Idle calls can outlive the owning task | Define operation cancellation and object closure responsibilities |
| Slow legacy NotificationHandler | Reading stops while user code runs; an I/O deadline cannot interrupt the handler | Prefer ReadEvent for new integrations; keep legacy callbacks short and nonblocking |
| Unbounded goroutines calling the same direction | Queued callers retain stacks, contexts, buffers, and deadlines | Apply admission before starting work; do not use library serialization as admission |
| Slow or absent event consumer | Channels fill and workers retain connections or buffers | Define bounded backpressure, expiry, and retirement policies |

An idle controlled reader is expected behavior. This design does not assert
that channels improve throughput or latency. Existing compatibility probes are
correctness evidence, not a performance comparison of scheduling models.

## 4. Three different notification responsibilities

| Notification | Owner | Delivery rule |
| --- | --- | --- |
| Descriptor readiness | Go runtime poller and SCTP wait layer | Keep internal; do not duplicate polling or expose a readiness channel |
| SCTP protocol notification | The connection's single receive path | Read through ReadEvent alongside data; parse with ParseNotification |
| Application lifecycle/completion | Adapter/session | A channel may deliver owned application events under the rules below |

Do not create a second socket reader for notifications. It would compete with
the data reader for the same receive queue. Separate data and notification
channels also do not preserve their relative processing order through `select`.

A receive executor must interpret required protocol state before further
delivery. For example, a valid consumed RESTART can invalidate the connection
generation while arriving with an error and CloseRequired. Preserve those facts
before terminal handling; malformed or unconsumed bytes are not valid events.
The [message API](../message_io_linux.go) and
[notification types](../notification.go) define the underlying result contract.

## 5. Upper-layer execution and channel boundary

The recommended bounded integration has one Accept caller per listener, one
reader per association, and one controlled writer per association. Read and
write may run concurrently. These are adapter admission rules and resource-ledger
assumptions, not new restrictions added to all legacy library callers.

The control loop owns lifecycle decisions. I/O executors call existing SCTP
APIs and report results without running unbounded business callbacks. Reserve
candidate capacity before accepting or dialing, and start the application
handshake budget at the appropriate handoff boundary, not while idle in Accept.

Asynchronous receive delivery should use one bounded, ordered stream per
connection generation. An envelope must retain the generation identifier,
monotonic receive sequence, result kind, relevant metadata, and error/terminal
facts. Sequence describes local receive order; it does not invent global wire
ordering across SCTP streams or independent read/write operations.

Do not introduce a channel if the existing consumer already has suitable
reader/writer execution and completion delivery. In particular, the diameterkit
handoff already specifies context Accept, a single ReadEvent path, admission,
and generation handling. Inspect its current implementation before adding any
queue. See the [integration handoff](diameterkit-v0.8.0-handoff.md).

### Connection ownership and cancellation

- Until successful upper-layer handoff, the adapter owns an accepted connection
  and must close it on failure or cancellation that wins the handoff decision.
- After handoff, the recipient owns Close. Canceling the old Accept/setup context
  must not revoke that ownership. Later session shutdown is a separate policy.
- Define a single handoff decision and its race with stop. A preliminary context
  check or `select` alone does not give cancellation priority when both cases
  are ready. Either valid winner must have a documented cleanup path.
- Default to no accepted-connection prefetch queue. Even with an unbuffered
  delivery channel, one accepted child can be held by the producer while waiting
  for the recipient; reserve that candidate before Accept.
- If buffered connection delivery is required, define an atomic claim/lease or
  equivalent queue ownership protocol. A successful buffered send is not proof
  that the consumer has received the connection. Sender cleanup must never race
  a consumer's claim or leave abandoned buffered connections unowned. A bare
  channel of connection pointers is insufficient without an ownership protocol.

### Buffers and result lifetime

ReadEvent writes into caller-owned buffers. ParseNotification owns its fixed
fields but borrows Tail from the supplied notification bytes. Sending a slice or
Notification through a channel copies only its value, not the backing bytes.

Choose and document one of two bounded strategies:

1. Copy retained data into explicitly budgeted storage before reusing input.
2. Transfer a buffer lease and prohibit reuse until exactly one release returns
   it to the bounded pool. Stop must account for outstanding leases.

Fixed application notification projections may omit Tail when the application
does not need it. Never retain a borrowed tail accidentally. Bound the number
and total bytes of queued and consumer-held buffers, not just channel capacity.

Preserve valid result facts with errors. A positive send count must not disappear
because cancellation was also observed. Write completion is not peer delivery
confirmation, and a short or uncertain send does not authorize tail retransmit
or automatic application replay.

### Backpressure, terminal state, and shutdown

Choose a finite queue capacity and byte budget. When capacity is exhausted,
use cancellable bounded waiting/backpressure or terminate the affected
connection according to declared policy. Do not silently drop protocol-critical
notifications or move overflow into an unbounded side list.

Stopping reads also delays later notifications in that socket's receive queue;
a separate notification reader cannot repair this. Bound stall duration where
protocol progress requires it. Preserve the original absolute frame/operation
budget across fragments, notifications, and queue waits; do not reset the budget
on each event.

Terminal cause and shutdown completion must not depend on placing another item
into a full data queue. Keep bounded terminal state accessible to the control
path, retaining valid terminal facts and sequence. An early stop signal may
prevent further delivery; it must not turn an invalidated old generation back
into an active one.

Distinguish stop request from completed retirement. A completion signal or Wait
must confirm executor exit, callback completion, and release/transfer of all
owned connections and buffer borrows. Canceling only a channel send does not
cancel an in-flight SCTP operation. Pass cancellation to the operation, and use
Close when terminating the owned socket. Canceling one Accept need not close a
listener intended for reuse.

Only the designated producer/coordinator closes output channels, after all
senders have exited. Consumers must not close them. Cleanup must work when the
consumer never reads again; account for queued items and outstanding claims
explicitly rather than relying on a goroutine blocked forever in a send.

## 6. Resource accounting

The existing SetupRequirements/MessageIORequirements describe the library's
versioned logical charges under their stated concurrency assumptions. They do
not include an arbitrary application channel architecture.

An adapter ledger must add queue/envelope storage, copied or leased payloads,
pending accepted connections, consumer-held items, worker/control state, and
overlapping generations without double-counting library charges. For an accept
queue of capacity Q, include Q queued candidates plus any child currently held
by the accept executor and all handed-off candidates still owned by the system.

A channel capacity limits slots, not referenced payload sizes or consumer-held
leases. Bound both count and bytes. Release reservations only when the owner or
borrower actually retires. Cancellation intent is not resource retirement.
Go runtime stacks, allocator/GC costs, kernel queues, and resolver infrastructure
need separate measurement; these logical ledgers are not an RSS guarantee.
See [bounded setup accounting](bounded-setup.md).

## 7. Acceptance requirements

The following gates apply to a future adapter/channel implementation or changes
to the existing waiting model. They are requirements, not results obtained by
publishing this design. Keep library regressions and consumer acceptance
separate; diameterkit batch 64/65 and migration checks remain required where
that adapter changes.

| ID | Required scenario | Acceptance evidence |
| --- | --- | --- |
| A1 | Idle Accept/read/write readiness waits | Native SCTP proves no periodic polling, bounded cancellation/Close completion, and no spin; implementation inspection establishes poller use |
| A2 | Accept arrival races with cancellation, deadline, or Close during child setup/publication | Barrier-controlled cases show exactly one handoff or cleanup, no fd leak/double-close, reusable listener after per-call cancellation, and no effect of the old context on a handed-off child |
| A3 | Deadline shorten/extend/clear and SO timeout interactions | Active and queued operations retain documented error identities; zeroing one timeout mechanism does not clear another; notifications do not refresh the absolute budget |
| A4 | Slow consumer, full queue, and abandoned consumer | Stop wakes both I/O and channel/queue waits; terminal state remains observable; workers exit and all unclaimed objects retire within the declared bound |
| A5 | Handoff/claim races and repeated shutdown | Exactly one owner for every connection and buffer lease; producers alone close channels; no send-on-closed panic, stale claim, or use after release |
| A6 | Interleaved data, notifications, restart, and partial-delivery abort | One socket reader; local receive order and generation checks hold; valid terminal facts survive accompanying errors; old-generation work is rejected |
| A7 | Buffer reuse while consumer is delayed | Race-enabled tests prove copied/leased payloads remain valid and borrowed Tail cannot escape into reused storage; count and byte caps hold |
| A8 | Send progress races with cancellation/Close | Actual n/error and uncertainty survive, each completion occurs once, and no automatic short-write tail retry occurs |
| A9 | Admission and resource retirement | Reject insufficient reservations before starting work; include queued, in-flight, consumer-held and overlapping-generation objects; hold reservations while a borrower has not exited |
| A10 | Legacy/source compatibility | Existing signatures and mode selection remain unchanged; normal Accept still waits; no new channels, polling requirement, or blocking-fd conversion is imposed on existing callers |
| A11 | Performance assessment when proposing an asynchronous optimization | Compare equivalent workloads on the same Go/kernel/hardware and record idle CPU, OS threads, goroutines, memory, throughput, tail latency, cancellation latency and queue saturation; declare workload limits and regression thresholds before measuring |

Use deterministic barriers/fault injection for race windows and real kernel SCTP
for wakeup, descriptor, and network behavior. Sleeps, global goroutine counts,
or mock-only tests cannot independently prove retirement. Record exact source
versions, configuration, commands, exit status, and skipped/unavailable cases.
Run relevant race tests and the affected consumer's acceptance suite. A library
pass alone does not certify adapter ownership or application behavior.

For diameterkit, retain the batch 64 frame/notification/send/budget requirements
and batch 65 Accept/handoff/Close/Wait/generation requirements in the
[v0.8.0 handoff](diameterkit-v0.8.0-handoff.md#5-batch-6465-and-migration-regression-acceptance).
Any new queue or worker requires updated consumer resource evidence.

## 8. Evidence, delivery, and reconsideration

The subsequent [A1-A11 coverage audit](io-acceptance-audit.md) maps this design
to existing library/consumer paths, records focused validation, and identifies
partial coverage and optional mechanisms that are not currently applicable.

The [0.4-v0.8.0 audit](compatibility-v0.4-v0.8.0.md#9-evidence-and-limits)
records historical API comparison, seven-version native probes, and focused
race regressions. Those establish the existing behavior; they do not validate
an unimplemented channel adapter. This design is based on that evidence and
inspection of the current accept, wait, message, and notification paths.

This documentation delivery checks English text, local link targets, decision
consistency, and the staged diff. It does not claim a fresh release matrix,
consumer migration, or channel performance benchmark.

Reconsider a new public asynchronous API only after a concrete consumer need
cannot be served adequately at the adapter boundary, with measurements and a
complete ownership, cancellation, ordering, resource, and acceptance contract.
External event-loop integration would require a separate fd/poller ownership
design; exposing a readiness channel alone is not such a design.
