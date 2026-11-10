## Context

Design date: 2026-10-31. Implementation baseline: master 70b338ac419dff6aa745460bb379d68208d615e0; its library implementation is identical to 0.7.1 at e864ee06b6c5b0c77661c4715814a75576c23237.

Scope comprises the 14 blockers in [release tracker #3](https://github.com/skill215/sctp/issues/3): #1, #2, #4–#12, and #14–#16. #13 is a separate P3 non-blocking improvement. #1 regressed with the poller changes in 0.5; most other core defects existed by 0.3. Historical provenance does not change acceptance criteria.

Currently, SCTPConn owns os.File and socketIO. socketIO serializes each read/write direction separately and translates SO_RCVTIMEO/SO_SNDTIMEO snapshots into per-operation poller deadlines. Notification callbacks run outside fd callbacks. Preserve these boundaries and concentrate fixes on syscall-result interpretation, input preparation, and resource initialization.

## Goals / Non-Goals

**Goals:**

- Define independently reviewable, verifiable fix boundaries for all 14 defects.
- Preserve existing public signatures, the 32-byte native-endian wrapper header, the host-order PPID interface, and the Go 1.26 minimum.
- Preserve Close wakeup, independent read/write concurrency, queue waiting within the budget, a shared read budget for notifications, and one-to-many endpoints' ability to serve other associations.
- Convert audit probes into formal regressions for the corrected contract, retaining normal-behavior or raw-syscall controls for every defect.

**Non-Goals:**

- v0.7.2 does not introduce ReadMsg, message-boundary flags, DialContext, standard deadlines, a new notification parser, or a Go version upgrade.
- Do not rewrite the poller, serialize send/receive with a connection-wide lock, or introduce a terminal-state cache that automatically closes connections.
- This stage produces only the design and tasks, without implementing fixes or publishing.

## Decisions

### D1 Verify single-association existence after receive EAGAIN

Covers #1. The receive callback always calls nonblocking Recvmsg first and processes payloads or notifications through the existing flow. Only EAGAIN/EWOULDBLOCK triggers a decision about whether to keep waiting.

Introduce an internal associationScope distinguishing single-association connections from one-to-many/unknown endpoints. Dial, Accept, and this library's PeelOff paths mark their results as single-association. NewSCTPConn first verifies SO_PROTOCOL=132, then classifies SOCK_STREAM as single-association. It classifies SOCK_SEQPACKET as an exclusive association only after successfully querying SCTP_STATUS with assoc_id=0. Treat other cases conservatively as endpoints. Complete classification before returning the object. PeelOff must preserve provenance explicitly: SO_TYPE alone is insufficient because peeled sockets may remain SOCK_SEQPACKET.

On the single-association EAGAIN path, query SCTP_STATUS directly within the same pinned-fd callback, using the correct Status size, assoc_id=0, and uint32 socklen_t:

| Query result | Receive action |
| --- | --- |
| Successful query; association remains queryable | Return not-ready and wait through the existing poller |
| Valid query returns EINVAL, establishing that the single association is no longer queryable | Return 0, ENOTCONN and finish this read |
| Any other query error | Return that error with count 0; do not convert it to EOF |
| One-to-many or unclassified endpoint | Preserve existing waiting behavior; failure of an assoc_id=0 query must not terminate the endpoint |

This interpretation of EINVAL applies only to the internal SCTP_STATUS query with a known-valid format. It does not mean that EINVAL from any getsockopt indicates disconnection. In Linux v6.6, status-query EINVAL can indicate either an undersized structure or an absent association, making ABI validation a prerequisite. Reinitialize parameters for every query to avoid decisions based on garbage input.

Implementation-review addition: after a status query establishes absence, perform one more nonblocking receive. Normal EOF or buffered data may arrive between the calls and must be delivered first. Return ENOTCONN only if the second receive still returns EAGAIN.

This order processes data still returned by the kernel before interpreting the no-data state. It neither discards buffered data prematurely nor swallows a real ECONNRESET, and it does not read SO_ERROR, which consumes errors. The operation deadline is acquired before the notification loop; the query retains that original budget.

The single-association receive contract applies to sockets whose connection establishment is complete. Callers importing raw fds remain responsible for completing connection setup. Do not terminate reads speculatively on externally imported SOCK_SEQPACKET sockets with unknown models; fully cover the known provenance of this library's PeelOff.

**Alternatives and rationale:** Restoring blocking fds would break validated Close/concurrency/timeout behavior. Caching only fatal Write errors would miss resets consumed by other raw I/O and require a wakeup/drain-order design. Three probe rounds established that Getpeername retains cached addresses and cannot establish association liveness. The new status query occurs only on the not-ready path, avoiding a general state-machine expansion.

### D2 Specify empty I/O, control sends, and frame counts

Covers #4, #5, #6, #9, and #12.

| API/input | Designed result |
| --- | --- |
| Read(nil), Read(empty) on a live connection | 0, nil; consume neither payload nor notifications |
| SCTPRead with an empty buffer on a live connection | 0, nil metadata, nil; do not enter Recvmsg |
| Wrapper Read with a buffer of at most 32 bytes | 0, EINVAL; neither receive data nor modify the buffer |
| Ordinary empty Write / SCTPWrite without explicit or default control flags | Perform a real zero-payload send; if Linux rejects the ordinary empty message, return 0 and the original error without injecting payload |
| Header-only ordinary wrapper data write | If the underlying send rejects it, return 0 and the original error; do not count a rejected header as consumed |
| Empty payload with valid EOF/ABORT control | Send a real zero-payload control message; return payload count 0 on success |
| Header-only wrapper control frame | Return 32 after successful control transmission; return 0, error if transmission fails without sending |

Zero-length reads first check object initialization and closed state, then short-circuit as no-ops. nil/initialization-failed/closed objects retain explicit errors with count 0. Empty reads neither access the fd nor invoke notification handlers. Empty writes still enter the existing Write gate and follow connection-error precedence and the send budget.

Define EOF explicitly as Linux MSG_FIN, 0x200. Preserve the values of UNORDERED, ADDR_OVER, ABORT, and SACK_IMMEDIATELY. Linux validates control combinations, socket types, and association requirements; ordinary SOCK_STREAM gains no additional EOF capability guarantee.

All empty-payload sends use a private Linux sendmsg helper. Msghdr has a zero-length payload iov, existing ancillary buffers remain intact, and info=nil still lets the kernel apply default send parameters. Only the empty-payload branch bypasses Go SendmsgN's extra-byte behavior; nonempty sends retain the existing implementation. Keep the helper within the existing Write gate, EINTR/EAGAIN retry flow, and timeout budget. Use Cmsghdr.SetLen/SetControllen, native layouts, explicit KeepAlive, and existing uintptrescapes rules. Do not mutate caller SndRcvInfo; copy PPID before converting it to network byte order.

Do not collapse empty writes into no-ops or query default control flags in userspace before deciding whether to send. Defaults can also be set per association, and a query followed by a send introduces a configuration race. A single real sendmsg uses the kernel's current parameters, retaining valid SetDefaultSentParam controls such as ABORT.

Eliminate negative counts from SCTPRead syscall-error results. Retain valid received bytes even if later metadata parsing fails. The wrapper returns a frame only when valid payload/notification bytes exist:

1. With metadata: write the complete 32-byte encoded header.
2. Without metadata: zero the whole header and return the actual payload/notification bytes.
3. n=0 with error/EOF: return 0, error, without a fictitious header.
4. n>0 with error: return the initialized header plus valid payload count, together with the error.

A zero header without metadata means only that metadata is unavailable. It cannot distinguish a notification from valid stream=0, PPID=0 data. Retain the existing NotificationHandler/raw-notification mechanism and document this limitation in README.

### D3 Treat wrapper subscription updates as one configuration operation

Covers #8. Add eventsMu to SCTPConn, protecting only event-subscription operations. Public SubscribeEvents continues to replace the mask. Wrapper construction reads the old mask and writes old | DATA_IO while holding the same configuration lock. Public subscription methods and wrapper read-modify-write share internal unlocked helpers to avoid double locking.

eventsMu does not cover blocking I/O, user handlers, or the entire connection lifecycle. Callers changing events directly through public RawConn remain responsible for synchronization with other configuration operations; external raw syscalls cannot be constrained.

Keep the constructor signature unchanged. If querying or setting subscriptions fails, the wrapper stores initErr and subsequent Read/Write return it before I/O. Close always delegates underlying resource release. Do not silently pretend metadata is enabled or retry configuration during I/O. After correcting configuration, construct a new wrapper.

### D4 Centralize address copies and input validation

Covers #2, #7, #10, and #11. Add an internal preparation layer for validation, copying, and internal wildcard completion. Copy SCTPAddr, IPAddrs backing storage, and each net.IP byte slice. Even len=0, cap>0 input slices must not permit append to modify caller-owned aliased storage. Callers remain responsible for not concurrently mutating inputs during a call.

Execution order:

~~~text
network validation → nil/range validation → address snapshot → family selection
→ socket creation/initialization → Control → bind/connect
~~~

Build Control's address string from the input snapshot before wildcard IP completion, preserving the observed empty string for nil local addresses and :port for port-only addresses. Internal binding uses a separately prepared copy.

Use net.AddrError without requiring exact string matches. Ports range from 0..65535; local 0 retains automatic allocation, and remote 0 remains subject to existing kernel semantics. DialSCTP, DialSCTPExt, and SocketConfig.Dial reject nil raddr before creating a socket or invoking Control.

Low-level public kernel-facing entry points such as SCTPBind/SCTPConnect also validate structured addresses and ports. ToRawSockAddrBuf retains its return type as a low-level serializer without an error return. Do not change its signature or add panics for validation failures; document that networking entry points perform validation.

Each segment in a multi-address string must contain a host. A missing host in the final segment must not clear previously parsed addresses. Reject leading, intermediate, and trailing empty hosts; retain valid single-segment :port wildcards. Explicit 0.0.0.0 and :: are not empty strings and retain existing parsing/kernel semantics. Preserve IPv6 brackets and the existing fix that gives zone names precedence over numeric indexes.

### D5 Retry complete address queries within a bound

Covers #14. Use an 8-byte sctp_getaddrs header and a dynamic byte buffer with an initial 4 KiB address area. Double only on ENOMEM from this address query, up to a 1 MiB address area and at most 9 attempts. Reset assoc_id, addr_num, and uint32 optlen each time; discard partially written data on errors.

1 MiB is an explicit per-query resource budget, not a promise of unlimited address sets. It holds 65,536 IPv4 sockaddr entries or approximately 37,449 IPv6 entries, well beyond the 300-address reproduction. If the query still fails at the limit, return an error carrying capacity context and matching errors.Is(ENOMEM), without a partial list. Add no public configuration API. Return other errno values directly without growth retries.

Linux local-query optlen counts only the address area, while peer-query optlen also includes the header. Normalize them separately before existing per-address family/length decoding. Reject out-of-range optlen, invalid counts, truncation, and unknown families. Do not decode mixed lists using a fixed IPv4/IPv6 stride.

State this resource policy in README, public address-query comments, and release notes. Listener.Addr retains the net.Addr signature's lack of an error return: return nil on query failure, without presenting a configuration snapshot as a complete kernel address list.

**Rationale and trade-offs:** Linux reports both insufficient buffer capacity and allocation failure as ENOMEM; one errno cannot distinguish them. Bounded growth limits retries and allocation pressure during real OOM. A larger fixed array still leaves an implicit cap, while unbounded growth may turn one query into memory exhaustion.

### D6 Unify public APIs and release fds on unsupported initialization failure

Covers #15 and #16. Move ErrUnsupported into shared compilation code, retaining its text and sentinel semantics. Do not rewrite normal Linux errors as this sentinel. Add unsupported Listener.SyscallConn, returning nil, ErrUnsupported. Consumers can compile the same error checks and syscall.Conn assertions on all tested targets.

Unsupported NewSCTPConn cannot initialize successfully, so it takes ownership of a valid fd and immediately releases it during construction. Return an object with initErr=ErrUnsupported and internal fd=-1. Later Close calls never retry that number and continue returning the unsupported-platform error. Retain cleanup failures as additional initErr causes while preserving errors.Is(ErrUnsupported). Do not blindly retry close/EINTR and risk closing a reused descriptor number.

Unsupported I/O, SyscallConn, and Close return the object's initErr through a shared internal helper; nil objects or objects without an additional error still return the original sentinel. Thus constructors without error returns expose cleanup failures through public methods rather than storing inaccessible errors.

Release resources with the correct platform socket-close primitive: native close on Unix-like targets and closesocket on Windows. Do not treat Winsock handles as ordinary os.File.Close handles or truncate them to int32 first. fd=0 is valid ownership and requires a subprocess test.

This matches the supported-platform policy of closing fds when initialization fails, without introducing a poller or finalizer for unsupported platforms. I/O still returns ErrUnsupported; support scope does not expand.

### D7 Small implementation batches and release boundaries

Use the following groups. First add formal behavioral regressions that fail on the baseline, then submit fixes and normal controls. Do not copy audit control assertions expecting unfixed behavior into acceptance tests.

| Batch | Content | Dependencies |
| --- | --- | --- |
| A | #4/#12 read counts and empty buffers; #9 headers; #8 subscriptions | Shared wrapper path; review together |
| B | #1 receive after association termination, PeelOff classification, timeout/Close controls | A's count rules; prioritize the identified regression |
| C | #5 empty payload/control sends; #6 EOF constant | Validate raw sendmsg and flag fixes together |
| D | #2/#7/#10/#11 address snapshots/validation; #14 address queries | Input layer first, then large address sets |
| E | #15/#16 platform API and ownership; optional #13 normalization | Independent lifecycle and downstream compilation gates |
| F | Integration review, CI, release preparation | Acceptance of all 14 A–E blockers |

Plan to implement #13 by making Accept explicitly return nil and the original error on failure. If deferred separately, record the reason without making it a release blocker or claiming an unimplemented fix is complete.

### D8 Validation and CI design

- Native Linux/amd64: Go 1.26.x and 1.27.x each run 10 full race/shuffle rounds, each in an independent process with timeout=120s per round. Stop on any failure; record patch version, seed, and kernel. Cover critical #1 scenarios on at least two actual kernel environments. Do not misreport a shared ten-round binary timeout as a per-round budget.
- Focused checks: Read/Write/Accept/Connect timeouts, notification budgets, queued I/O and Close, buffered-data draining, normal SHUTDOWN, unconsumed/consumed reset, and repeated reads.
- Raw sendmsg: use non-SCTP sockets under six-architecture QEMU to validate zero-iov and ancillary ABI. Validate actual EOF/ABORT controls on native SCTP. Cover ARM, ARM64, MIPS, MIPS64, PPC64, and s390x, explicitly retaining 32-bit socklen_t, Cmsg alignment, and big-endian checks.
- Linux/386: natively execute unsupported API and real SCTP fd cleanup tests without the race detector. Add native Windows/amd64 stub/socket-close tests without SCTP association tests.
- External consumers: compile under both Go versions for Linux/amd64, Linux/386, Darwin/amd64, FreeBSD/amd64, and Windows/amd64, covering the sentinel and syscall.Conn for Conn/Listener.
- Run vet, strict checkptr, and low GOGC for each Go version. Retain fuzz seeds for sockaddr, ancillary, codec, address round trips, and newly expanded boundaries.
- Execute numeric interface-name/index conflict tests for 3 rounds in an isolated namespace; CI rejects skips.
- Do not describe QEMU ABI tests or cross-compilation as complete SCTP protocol validation. Optional environment-dependent cases may explicitly skip locally; designated release-required CI jobs must execute their coverage.

## Risks / Trade-offs

- SCTP_STATUS EINVAL can also mean an invalid structure length → use the interpretation only for internal single-association queries with correct ABI and assoc_id=0; gate on Status layout and live-connection controls.
- Peeled sockets and endpoints may share SO_TYPE → record construction provenance and conservatively wait when an external model is unknown. Association count=1 alone does not establish exclusivity.
- Constructors without error returns can hide initialization/notification errors → wrappers retain initErr for the first I/O; unsupported constructors immediately release fds.
- Historical empty-write/header-only-read behavior changes → specify empty-read short-circuits and real empty-write syscalls in the table, document corrected semantics in release notes, and prove no loss/injection with marker payloads.
- Address-query ENOMEM is ambiguous → bound growth, preserve errno, never parse partial lists left by failed calls, and explicitly state the 1 MiB budget.
- Windows sockets and Unix fds require different close operations → use platform-private helpers and native Windows/386 resource-reuse tests rather than inferring correctness from Linux compilation.
- Subscription locks can introduce reentrancy issues → cover only short configuration syscalls with unlocked internal helpers, excluding user handlers, Close, and I/O waits.

## Migration Plan

1. Verify every formal regression's failure cause against an archived baseline; retain raw-syscall/normal-behavior controls.
2. Implement A–E in batches, integrating each only after focused and affected existing tests pass. Run release gate F last.
3. Update README, release audit, release plan, and v0.7.2 notes with accurate #1 provenance and changed error behavior.
4. Support v0.* in the release workflow, optionally retaining old 0.* compatibility triggers. Publish canonical annotated tag v0.7.2 only after required jobs succeed. Never move or delete historical tags.
5. Verify the candidate commit, APIs, and examples in an independent temporary module first; after official publication, verify go get @v0.7.2 resolution and the Release target.
6. Roll back commits by batch without moving published tags. Address post-publication problems with fixes and a new patch version; do not present a version with the known #1 regression as a stable recovery point.

## Open Questions

- Confirm the errno boundary for a valid SCTP_STATUS query when an association is no longer queryable, and conservative classification of externally imported SOCK_SEQPACKET, on additional native kernels. Complete this evidence gate before batch B. Adjust the design first if evidence differs; never disguise unknown errors as EOF.
- 1 MiB is this version's explicit address-query budget. If deployments require larger sets, collect requirements and resource measurements before designing a configurable query interface. This does not block coverage of the reproduced 300-address case.

## References

- [Proposal and specifications](proposal.md), [GitHub #3 and per-issue evidence](https://github.com/skill215/sctp/issues/3).
- [Linux v6.6 SCTP socket implementation](https://github.com/torvalds/linux/blob/v6.6/net/sctp/socket.c): sctp_getsockopt_sctp_status, sctp_id2assoc, sctp_getsockopt_local_addrs, sctp_getsockopt_peer_addrs, sctp_do_peeloff.
- Historical, five-round, and verification-audit results under docs/repro form the audit baseline. Some observation assertions target unfixed behavior and are not green-test contracts.
