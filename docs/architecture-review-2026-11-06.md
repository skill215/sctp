# SCTP low-level design and fix review

Date: 2026-11-06. Baseline: v0.7.3, commit
`e4154064dc9928274bcf4615c5382dd6fd81ad21`; the fixes below are in the local working tree.
Scope is limited to this SCTP repository.

## Overall assessment

For a library that exposes Linux kernel SCTP to Go, the existing low-level direction is reasonable and maintainable. There is no evidence supporting a protocol-stack rewrite or a new networking framework. The areas needing stronger guarantees are syscall-boundary validation, lifecycle invariants, test fixtures, and clarity of responsibilities and error semantics.

The library is currently suitable for users who understand Linux SCTP semantics. Becoming a general-purpose Go networking library would require separate designs for standard deadlines, DialContext, complete message boundaries, and notification interfaces. These are documented capability limits, distinct from the bugs confirmed in this review.

## Layers and lifecycle

| Layer | Current implementation | Assessment and constraints |
| --- | --- | --- |
| Linux syscall / UAPI | `sctp_linux.go`, `sendmsg_linux.go`, public ABI types | Direct bindings are appropriate; actual ABI tests must protect fixed widths, layout, and byte order |
| fd ownership and waiting | `os.File`, `socketIO`, RawConn, directional gates | os.File owns the physical fd; operations must run under Control/Read/Write reference protection |
| SCTP message semantics | SCTPRead/SCTPWrite, status probing, notification handling | Preserve stream/PPID, kernel errno, and association distinctions; do not treat a one-to-many endpoint as an exclusive association |
| Addresses and associations | Resolve, bind/connect encoding, growing queries, PeelOff | Keep the interfaces; validate every int before narrowing it to a kernel integer |
| Compatibility adaptation | 32-byte SndRcvInfo wrapped connection | This is a local adaptation format; it cannot supply complete EOR/notification classification absent from the API |
| Example program | CLI parsing, per-connection settings, echo | Separate configuration input from kernel readback; invalid addresses must fail before socket creation |

The connection's atomic `_fd` represents logical closure and one-time Close; physical access still belongs to os.File. It is not a guarantee that callers may bypass RawConn and use the fd directly. The listener's private `fd` field has no readers; it may be removed later, but this batch does not use that cleanup to replace the ownership model.

Issue #19 is fixed: FileListener must perform F_DUPFD_CLOEXEC atomically while the source file is pinned by RawConn.Control. KeepAlive prevents GC reclamation only; it neither prevents concurrent Close nor replaces fd reference protection. The source and imported duplicate close independently, and failure paths close only the acquired duplicate. Regressions cover concurrent closure, fd reuse, source ownership, closure, and nil input.

NewSCTPConn's existing signature cannot return construction errors immediately, so initErr and correct fd cleanup after failure remain. A future strict-import API should return an error rather than silently change the old constructor's ownership and failure semantics.

## I/O, timeouts, and notifications

Nonblocking fds with the Go runtime poller fit the current implementation. Waiting is serialized per direction while reads and writes can run concurrently; Close wakes both poller waits and operations queued at a gate. Timeout snapshots are taken at the start of the public operation, including queue time and repeated notification consumption. The operation deadline must be cleared before releasing the directional gate. The current implementation satisfies these invariants, with adjacent regressions covering Read, Write, Accept, connection establishment, notification loops, timeout clearing, and Close wakeups.

For exclusive associations, a status query after recvmsg EAGAIN detects a reset already consumed by another operation. A second read before reporting the missing association protects concurrently arriving data or EOF. This must remain limited to known exclusive associations: id=0 EINVAL on a one-to-many endpoint cannot signal termination of the entire endpoint. Accurate ABI and kernel behavior are essential, so two native Linux kernels remain a release requirement.

Notification handlers run outside RawConn callbacks and directional gates, allowing them to close the connection without self-deadlocking on an fd reference. Handlers are synchronous user code; callers should avoid prolonged blocking and copy bytes needed after return. Timeouts cannot preempt arbitrary blocking user callbacks.

The standard SetDeadline methods still return EOPNOTSUPP, and DialContext is not yet available. Adding them later requires one explicit set of deadline rules combining standard deadlines, relative socket timeouts, queue waits, and cancellation, rather than stacking timers that overwrite one another.

## ABI and platform boundaries

Issues #21 and #23 show the main risk of mismatched Go int and kernel field widths. The fixes validate all four INITMSG values against 0..65535 and association IDs against signed 32-bit representability before conversion. Invalid requests must neither change options nor act on another association with the same low 32 bits. Valid zero values, signed 32-bit values, and existing kernel errors remain intact.

Socket-option socklen_t is fixed at uint32; Go int cannot replace fields in SndRcvInfo, Status, or peer-address layouts. PPID converts between host and network order only at the boundary; native structures use native byte order. The public uintptr option interface remains a low-level escape hatch, with callers responsible for correct pointers, sizes, and lifetimes. Continue strict checkptr and cross-architecture ABI tests.

Support remains Linux with kernel SCTP, excluding Linux/386. Local review used one Linux/amd64 kernel. This batch's ARM, ARM64, MIPS, MIPS64, PPC64, and s390x checks cross-compiled test binaries; they are not native SCTP or QEMU execution. QEMU is not installed locally. Existing CI QEMU jobs validate ABI/encoding, not complete SCTP associations.

The architecture matrix does not cover riscv64, loong64, ppc64le, mipsle, or mips64le. This is a validation coverage gap, not evidence of bugs on those architectures. A later stage can add compilation jobs, then native/emulated checks based on actual deployment scope, clearly distinguishing each type of evidence.

The Solaris library built in this review. Its known syscall.Dup2 test-compilation gap and illumos support remain documented and deferred under the established scope. Non-Linux consumer builds and stub tests are compatibility safeguards, not a promise of SCTP support on those platforms.

## Error handling

The current errors express core states but are not completely uniform across interfaces:

- Closed normal I/O usually maps to net.ErrClosed, while SyscallConn and some option entry points may return EINVAL after closure. RawConn.Control may also return the runtime's original “use of closed file” error. Do not assume every entry point satisfies errors.Is(os.ErrClosed).
- Preserving Linux errno is useful for a low-level SCTP API. Socket read/write timeouts return EAGAIN and connection timeouts return EINPROGRESS; this is a different contract from standard deadline errors.
- Ordinary networking entry points lack consistent operation/network/address context. net.OpError could be considered later, while preserving errors.Is checks for underlying errno and first auditing callers' dependencies on error types.
- Unsupported targets, closure/initialization failures, and invalid parameters need stable precedence. This batch places new checks after the existing fd entry checks, preserves ErrUnsupported from unsupported stubs, and adds dedicated regressions.

This batch therefore fixes specific parameter/ownership defects without replacing error types throughout the library without a versioned design. The next error-model change should begin with per-entry-point behavior tables and compatibility tests.

## Test fixtures: an additional issue found during validation

Low-GC validation exposed intermittent EISCONN during connection setup in TestPeelOffCloseOnExec. Under independent signal pressure, SCTPConnect and a direct CONNECTX3 syscall each ran 1000 times and both produced the same result; peer-address queries and Accept confirmed that the association had actually been established. This is kernel behavior following signal restart of a blocking syscall, not justification to convert every EISCONN from the raw API into success. It is tracked as fixture defect [#24](https://github.com/skill215/sctp/issues/24).

The fix standardizes on lifecycleEndpoint: nonblocking connection setup, bounded readiness waits, real peer payload, and DATA_IO metadata to confirm the association ID. Immediate success still checks the ID returned by CONNECTX3. After setup, marker messages are drained and default events/timeouts restored. Lifecycle, PeelOff, status, send, and the new association-ID regressions share this fixture. New tests cover setup, PeelOff, and real payload under SIGURG. Every fd, process, and signal goroutine has an explicit cleanup path.

## Code organization recommendations

The code is maintainable, but `sctp.go` exceeds a thousand lines and combines ABI types, addresses, options, association queries, connection interfaces, and the wrapped connection. `sctp_linux.go` also combines syscalls, construction, send/receive, and waiting. This increases the chance of missing an adjacent contract when making changes.

Refactor by responsibility in small batches while retaining the sctp package and public signatures:

1. Extract wrapped_conn.go for the 32-byte adaptation format and forwarding methods.
2. Separate UAPI types/constants from socket-option methods; group association status, queries, PeelOff, and the new bounds checks in association-related files.
3. Then separate Linux socket lifecycles from message I/O. Keep socket_io_linux.go as the shared ownership/gate/deadline mechanism rather than reintroducing multiple closure/waiting implementations.
4. Remove unused setNumOstreams and listener.fd state; organize tests by feature and centralize shared fixtures, retaining reproduction links and before/after evidence.

Splitting files does not itself fix behavior. Each step should preserve behavior and receive a separate review; this batch does not simultaneously redesign the connection state machine, replace the syscall backend, or add platform support.

## Local candidate and subsequent gates

The local fixes cover #19–#24; #23 is association-target truncation and #24 is a fixture defect. Invalid example addresses and buffer configuration remain example defects and are not generalized into failures of ordinary library I/O. Complete commands, failure causes, and final results are in the [fix validation record](fix-validation-2026-11-06.md).

Passing local validation makes this candidate suitable for a commit/PR and remote validation. Actual publication still requires the existing two-Linux-kernel/two-Go matrix, architecture ABI checks, README execution, and independent API consumers. The five local race review rounds do not replace ten rounds per native release job. There is insufficient evidence to claim that the current version is “absolutely bug-free.”
