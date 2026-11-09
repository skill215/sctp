# Bounded setup and message continuity

Incremental A–E enhancement on the existing U2–U5 working tree, Go 1.26 and pure
Go production builds. This does not implement the separate SCTP v1 proposal or
modify diameterkit. Candidate provenance and actual test results are recorded in
[the delivery report](bounded-setup-delivery.md); local results are not remote CI
or a release.

## Public API mapping

Every proposed signature is implemented with its proposed name:

| Proposed / exported API | Contract |
| --- | --- |
| `(*SCTPListener).AcceptSCTPContext(context.Context)` | Same read gate and fd as Accept/AcceptSCTP; cancellable queue and runtime poller wait |
| `(*SCTPListener).SetDeadline(time.Time)` | Current/future accepts; zero clears; never inherited by returned connections |
| `(*SCTPConn).SetSocketTimeouts(read, write time.Duration)` | SOL_SOCKET receive/send timeouts, independent of standard deadlines |
| `(*SCTPListener).BoundAddr() (*SCTPAddr, error)` | Owned explicit binding snapshot and actual port, including wildcard/multihoming/zones |
| `AddressLimits`, `SetupConfig` | Explicit combined local+remote N/Z/D bounds and copied SocketConfig |
| `NewDialer(network, local, remote string, cfg SetupConfig)` | No DNS/socket I/O; validates before freezing inputs |
| `(*Dialer).DialContext(context.Context)` | Resolve both endpoints and connect with the same original context |
| `ListenContext(ctx, network, local, cfg)` | Bounded resolution, bind/listen, verification and final context publication |
| `SetupRequirements(SetupConfig)` | Pure versioned resource query, including IORequirements |
| `ReadResult.EndOfRecord() bool` | Portable EOR access; raw RecvFlags remains available |

Existing MessageIORequirements, GetMessageOptions, GetStatus, ReadEvent,
WriteMsgContext and ParseNotification are reused. No Diameter-ready state, global
capability cache, budget manager, second reader or hidden accept queue exists.

## A+C: lifecycle and socket capabilities

Accept keeps one synchronous read owner through accept4, child initialization,
message-option readback and final publication. The final decision holds the same
state lock as Close/SetDeadline. Before publication, observed Close wins with
net.ErrClosed; then context cancellation/deadline; then the earliest standard
listener deadline or fixed socket timeout. Standard deadlines match
os.ErrDeadlineExceeded. Context-aware socket expiry matches ErrSocketTimeout and
EAGAIN; old AcceptSCTP/Accept retain EAGAIN. Nil context is invalid. Pre-cancelled
calls do no socket I/O. A deadline extended or cleared before commitment can save
an uncommitted operation. No promise of physically atomic function return and
cancellation is made.

A successfully accepted fd is still private until child setup and publication
succeed. Failed/unpublished children are closed. Successful publication detaches
the child from later listener Close/context cancellation. Started cancellation
hooks are stopped or joined, the owner is cleared and the gate released before
return. Cleanup restores the current public deadline, never an obsolete snapshot.
An idle Accept has no periodic timer. Multiple callers are supported and can
cancel their own gate waits, but the fixed resource ledger permits only one.
Legacy nonzero SO_RCVTIMEO is sampled once at entry, including queue time.

SetSocketTimeouts validates both durations before modifying either option.
Zero clears; negative, duration-rounding overflow and target-timeval overflow are
rejected with ErrInvalidConfig. Positive values round upward to microseconds;
kernel granularity can be coarser. Two setsockopt calls are not atomic.
`*SocketTimeoutUpdateError` exposes Option, ReadApplied and the unwrapped native
Err. After a failed takeover update, close the connection. Existing operations
keep their old timeout snapshot; this API is for exclusive setup/takeover.

BoundAddr returns a deep copy, and remains available after Close. An empty or
wildcard bind remains unspecified; it does not enumerate local interfaces or
advertise routable addresses. Existing Addr/SCTPLocalAddr retain their dynamic
query semantics. FileListener lacks frozen input and returns
ErrBoundAddrUnavailable; it never silently enumerates interfaces as a fallback.
Old Listen paths also capture explicit bindings but are not bounded-input APIs.

## B: bounded setup

Set `Limits` explicitly; zero never means unlimited. Supported ranges are
N=1..64, Z=0..4096 and D=1..16384. Recommended integration limits are
16/256/4096. N counts explicit slash hosts across both texts, Z counts their zone
bytes, D is their combined byte length. Checks happen before Split/Clone, DNS or
fd creation; parsing uses borrowed substrings. After resolution, address count,
zone bytes and IP families are checked again. Empty local text means no explicit
local dial bind, or wildcard port0 listen. Wildcards count as explicit hosts when
written. Numeric ports only: no sign, service name, overflow or empty port;
remote text/host/port0 and empty multihoming hosts are rejected.

Networks are sctp/sctp4/sctp6; slash syntax is unchanged. Each explicit host
selects one suitable address. DNS answer collections are never implicitly
multihomed. Network/text/zone strings, IP bytes and MessageOptions are owned,
including small substrings of larger strings. Inputs may be shared read-only,
but must not be concurrently mutated during construction. A Dialer may be used
concurrently if the caller reserves each candidate separately.

Control and NotificationHandler are rejected without invocation or replacement.
Messages=nil remains available for legacy I/O; nonnil selects existing immutable
message mode. Fresh sockets start with zero SO timeouts; bounded listener setup
and each accepted child explicitly enforce zero. Context dial creates its own
fresh socket without user callbacks. Standard deadlines/contexts provide I/O
budgets. All phases use the original context; no timeout restart, endpoint
fallback, reconnect, replay or abandoned setup goroutine is introduced. Bind and
option syscalls are synchronous; cancellation is checked between stages and at
publication, with all private resources cleaned before return.

DNS result allocation, caches and resolver workers are external infrastructure;
N does not bound their total memory. Numeric-IP deployments can avoid DNS.
Zone-name lookup/runtime interface infrastructure likewise is not an application
payload-memory guarantee. Constructor/bind concurrency remains caller-owned.

## D: machine-readable ledger

Final ledger: `bounded-setup-v2/linux/<GOARCH>` (v1 was the A–D intermediate
snapshot). The version tracks this implementation and ABI, not protocol features.
The compiled `unsafe.Sizeof` values supply architecture-dependent coefficients.
Finite validated maxima are converted to uint64 before arithmetic; even the
maximum supported configuration is far below uint64 overflow. Invalid limits,
callbacks/options or unsupported platforms return errors, never false zero
support. The query performs no I/O and is not a kernel capability probe.

Let `A = 2*sizeof(SCTPAddr) + N*(2*sizeof(net.IPAddr)+16+
sizeof(RawSockaddrInet6)+2*sizeof(string)) + Z + 2*D + 128`.
This envelope covers both endpoint headers, append capacity, owned IP bytes,
raw sockaddr arrays, split indices, zone strings and transient host/text copies.
Six envelopes conservatively cover simultaneous resolved endpoints, DialContext
clones, wildcard synthesis/raw encoding and conversion temporaries; listener
construction uses three for its resolved input, bind copy and frozen/output copy.
No all-interface address query is included or invoked by bounded setup.

`ConnectionBytes` sums connection/socketIO/options/file values, two operations,
one fixed Notification, all IORequirements scratch, seven channel slots ×256,
four controlled-context/hook slots ×512, a file/poller allowance of2048,
bounded control/error workspace2048, and256 bytes for legacy ancillary space.
The scalar continuity cursor and directional stream counts are in SCTPConn.
Channel/context/file allowances are versioned logical policy slots; they are not
claims about undocumented runtime allocation sizes. The slots cover the active
owners/hooks and bounded transient cleanup, not arbitrary parent/value graphs.

`DialerBytes = sizeof(Dialer)+2*sizeof(SetupConfig)+2*sizeof(MessageOptions)
+2*D+16+512` includes retained inputs and constructor workspace.
`ListenerBytes = sizeof(listener+socketIO+options+file+one operation)
+6*256+2*512+2048+2048+3*A+2*D` includes one synchronous accept owner/hook,
construction and one returned BoundAddr copy, but no child connection.
`EstablishBytes = ConnectionBytes+6*A+2048` includes the connection while its
resolved/encoded/copied addresses and setup workspace still exist. It excludes
shared Dialer/listener charges. Repeatedly retained BoundAddr copies are owned by
and charged to the caller; only one simultaneous query copy is in this ledger.

On the tested Go1.26.10 Linux/amd64, with N16/Z256/D4096:

| Item | Logical bytes |
| --- | ---: |
| DialerBytes | 8960 |
| ListenerBytes | 48524 |
| EstablishBytes | 78380 |
| ConnectionBytes | 9516 |
| Candidate = DialerBytes + max(EstablishBytes, ConnectionBytes) | 87340 |
| Listener + old retiring connection + new candidate | 145380 |

OOBMin=48; NotificationMin is subscription-dependent (148 with all five events).
Peek/read/write scratch=8/480/280 is **already included** in ConnectionBytes,
as is one fixed parsed Notification. For Messages=nil the query conservatively
returns the five-event message layout as a sizing envelope, not available message
I/O on that legacy socket. Application adapter state, prefix, payload, OOB,
notification/discard buffers and owned tails are extra. Never add old8192 or the
old copied resource formula on top of the new total.

Assumptions: one reader, one writer, one Accept, no queued upstream I/O; controlled
standard contexts and serialized, bounded deadline updates. Extra waiters,
unbounded update generations/context reference graphs are not covered. Release
reservations after actual calls/hooks exit, not merely after requesting cancel.
Old retiring connections, new candidates, shared listeners and application parser
buffers coexist and must all be charged. Runtime/allocator/GC/stacks, kernel
queues and DNS/zone infrastructure are excluded. Allocation measurements support
the audit but do not imply an RSS cap or a bound on garbage awaiting collection.

## E: continuity and negotiated streams

Message mode now requires explicit InfoPresent for every data record. One fixed
cursor under the existing read gate spans fragments through EOR, including
notifications between them. Association, stream, PPID and ordered/unordered must
match; ordered SSN must match, unordered SSN is ignored. Completed messages may
freely change stream/PPID, and PPID0 is legal. The cursor retains no payload,
buffer reference or per-stream map. An unconsumed cancellation does not clear it
or fabricate EOR. EOF after a partial message loses the boundary and requires
Close. A PARTIAL_DELIVERY_ABORTED notification during an active message remains
a successful raw notification with CloseRequired, and latches subsequent reads
unusable without resetting the cursor. Missing/contradictory metadata, truncation and consumed errors preserve
actual bytes/flags, set CloseRequired and latch ErrReadUnusable on future reads.

Actual Instreams/Ostreams are read from SCTP_STATUS after establishment; RX uses
Instreams and TX SID uses Ostreams. Configured requests are never substituted.
This snapshot is valid for the association generation, with no dynamic reset or
expansion support. Subscribe to association events to observe restart. A consumed
RESTART preserves its notification bytes/flags, returns ErrAssociationChanged
and CloseRequired, latches reads and rejects subsequent writes. A writer already
committed to the kernel still returns its actual evidence. The application must
terminate the generation; before userspace observes restart, the kernel remains
the final authority on stream validity. No background notification reader exists.

Writes retain the one-send contract: no tail retransmission; a positive short
success returns io.ErrShortWrite, positive bytes plus errors remain visible.
PPID is host-order uint32 with one conversion at the ABI boundary. SID selection,
PPID46/0 policy and application frame length versus EOR remain application policy.

## Compatibility and migration

No existing exported struct gains fields in this change (private state grows).
New types are additive. The pre-existing U2 SocketConfig.Messages field already
requires keyed literals when migrating from v0.7.4. Legacy resolver service-port
syntax, old nil-Messages entrypoints and zero-value SocketConfig stay intact.
Message-mode validation is intentionally stricter: previously returned missing or
inconsistent data metadata now fails closed. There is no optional second reader
or separate strict queue. Unsupported builds export the new APIs and return
ErrUnsupported. Raw-fd mutation is outside these guarantees.

The [standalone consumer source](examples/bounded-consumer/main.go.txt) covers
requirements, listen/dial/accept, timeout clearing, fragments, notification
parsing, cancellation, post-handoff operation and Close. Use the exact module and
go.sum in the delivery report with GOWORK=off; a development replace is not
publication evidence.

After upstream acceptance, diameterkit can delete its20ms accept loop, direct
setsockopt/getsockname, duplicated N/Z/D parsing/copying, hardcoded8192/copied
upstream formulas and, after E acceptance, SCTP metadata continuity checks. Keep
Diameter20-byte/24-bit length and prefix/body/discard logic, whole-frame deadlines,
notification limits, transport events/ConnectionID/Sequence/first cause, CEA/DWR
barriers, stream/PPID policy, Peer election/Watchdog/REOPEN/DPR/DPA, pending/attempt
completion and Node admission/retirement. No upstream success certifies that
migration; the separate diameterkit session must pin the candidate and rerun
batches64/65 Base/S6c, budgets, cancellation/Close, election, REOPEN and no replay.
