# Compatibility and migration: 0.4 through v0.8.0

Reviewed against the immutable Git tags on 2026-11-10. The starting tag is
**`0.4`**, not `v0.4`; the destination is **`v0.8.0`**. This document covers
public source compatibility, runtime behavior, fd ownership, errors, ABI/wire
values, configuration, platform requirements, and shipped examples. It separates
changes to existing APIs from behavior selected only by adopting new APIs.
Fixing an old bug can still require a consumer migration.

**The nonblocking change starts in 0.5.** Library-owned descriptors become
nonblocking, including accepted sockets. Public `Accept`/`AcceptSCTP` still wait
for a connection through Go's runtime poller. Code using raw descriptors, shared
duplicates, blocking syscalls, or SO timeouts needs explicit review. Keeping old
function names is not a promise of identical 0.4 behavior.

The [I/O interface design](io-interface-design.md) records the decision to retain
these interfaces and defines when upper-layer channels are appropriate, with
ownership, ordering, shutdown, resource, and acceptance requirements.

## Version boundaries

| First version | Compatibility changes |
| --- | --- |
| `0.4` | Baseline: blocking sockets, direct `Accept4(fd, 0)`, unpinned raw Control callbacks, unimplemented raw Read/Write, Go 1.18 module directive |
| `0.5` | Nonblocking descriptors and runtime poller; pinned ownership and Close wakeups; accepted CLOEXEC; status/peer-state constants corrected; raw Read/Write implemented. Introduces a SO-timeout regression and a consumed-reset receive regression. |
| `0.6` | Restores SO-timeout behavior through poller budgets; serializes each I/O direction and counts gate waiting against its budget. The consumed-reset regression remains. |
| `0.7` | Go minimum becomes 1.26; one timeout budget across legacy notification reads; default-send PPID, socklen_t, mixed-address decoding, wrapper alignment/counts, peeled CLOEXEC, network validation, and numeric zones corrected |
| `0.7.1` | Test/namespace handling changes; no production-library difference from `0.7` |
| `v0.7.2` | Fixes consumed-reset liveness, empty I/O, EOF flag, wrapper counts/headers/subscriptions, address copies/validation/query capacity, nil Accept error results, and unsupported-platform ownership; first canonical `v`-prefixed release in this interval |
| `v0.7.3` | Example stream selection and scope documentation; no production-library difference from `v0.7.2` |
| `v0.7.4` | Pins FileListener source during duplication; validates INITMSG values and association-ID widths; fixes example validation/buffer handling and test setup |
| `v0.8.0` | Standard deadlines take effect even in legacy mode; context/bounded setup, cancellable accept, bind snapshots, SO-timeout API, and opt-in message/notification/resource APIs; `SocketConfig.Messages` changes the public struct shape |

Exact tag commits, exported API differences, and measured observations are in
[the audit results](repro/compatibility-v0.4-v0.8.0/audit.json).
The version boundaries above describe this repository, not other SCTP forks.

## 1. Accept, nonblocking descriptors, and raw I/O

### What changed, and what did not

In 0.4, listeners are blocking and `AcceptSCTP` directly calls
`syscall.Accept4(ln.fd, 0)`. In 0.5, `socketFile` sets `O_NONBLOCK` before
registering the descriptor with `os.File`/the runtime poller. Acceptance runs
inside a raw-read callback and uses `SOCK_NONBLOCK | SOCK_CLOEXEC` for the child.
v0.8.0 retains that mechanism through `AcceptSCTPContext`.

The flags passed to **accept4 describe the new child**. Whether accept4 waits
for an arrival depends on **the listening descriptor's `O_NONBLOCK` state**.
Both changed in 0.5. On an empty queue, the kernel call returns EAGAIN; the
library asks the poller to wait instead of returning that transient error to a
normal caller. This does not turn public Accept into a polling API.

| Integration point | 0.4 | v0.8.0 | Migration |
| --- | --- | --- | --- |
| Ordinary `Accept`/`AcceptSCTP` | Kernel call waits | Go poller waits; Close/deadline/socket-timeout can end the wait | Keep the normal accept loop; no EAGAIN spin loop is needed |
| Listener, dialed, accepted, or `NewSCTPConn` fd | Generally blocking; imported fd was not reconfigured by NewSCTPConn | Nonblocking while library-owned | Do not assume a direct syscall will wait |
| Accepted descriptor inheritance | No CLOEXEC requested | CLOEXEC requested atomically | Programs intentionally passing accepted fds through exec must arrange explicit descriptor transfer |
| Direct `accept4`/`recvmsg`/`sendmsg` on the fd | Could block in the kernel | May immediately return EAGAIN/EWOULDBLOCK | Prefer library I/O; otherwise obey RawConn readiness rules |
| `SocketConfig.Control` in legacy setup | Sees a blocking fd | Sees an already nonblocking fd | Restrict it to short configuration calls; do not use it as a blocking I/O hook |
| `RawConn.Control` | Callback held only a saved integer, without lifetime pinning | Owner is pinned during callback; closed-owner access is rejected | Do not save the integer for later use or independently close it |
| `RawConn.Read` / `Write` | Panic with “not implemented” | Poller-backed callbacks | Return false when readiness is needed; retain syscall errors separately from the outer wait error |
| Same-direction operations | No library direction gate | Serialized since 0.6; reads and writes remain independently concurrent | Avoid reentering the same I/O direction inside a raw callback; reserve queued waiters separately |

Never call `SetNonblock(fd, false)` on a library-owned descriptor to emulate
0.4. A blocking syscall inside a poller callback can defeat cancellation,
deadlines, and Close wakeups. Do not call the owner's Close from its own raw
callback: the callback holds the fd alive. Legacy notification handlers run
outside fd callbacks and may close the connection; these are different cases.

For genuinely necessary raw reads, this is the readiness pattern. It is for a
**legacy-mode connection without notification subscriptions**, not a message-mode
reader; it deliberately omits ancillary/message-boundary interpretation.

```go
package example

import (
    "syscall"

    "github.com/skill215/sctp"
)

func readRawLegacy(conn *sctp.SCTPConn, data []byte) (int, error) {
    raw, err := conn.SyscallConn()
    if err != nil {
        return 0, err
    }
    var n int
    var receiveErr error
    waitErr := raw.Read(func(fd uintptr) bool {
        n, _, _, _, receiveErr = syscall.Recvmsg(int(fd), data, nil, 0)
        return receiveErr != syscall.EAGAIN &&
            receiveErr != syscall.EWOULDBLOCK && receiveErr != syscall.EINTR
    })
    if waitErr != nil {
        return 0, waitErr
    }
    return max(n, 0), receiveErr
}
```

Use `SCTPRead` or `ReadEvent` in ordinary applications. Raw access bypasses
message-mode guards and can invalidate their guarantees.

Sources: [0.4 socket implementation](https://github.com/skill215/sctp/blob/0.4/sctp_linux.go),
[0.5 transition](https://github.com/skill215/sctp/compare/0.4...0.5),
[v0.8.0 accept implementation](https://github.com/skill215/sctp/blob/v0.8.0/setup_socket_linux.go).

### Imported and duplicated descriptors

- **NewSCTPConn takes ownership.** Since 0.5 it configures the fd for polling,
  including `O_NONBLOCK`, and records initialization failures. Its signature has
  no error return, so inspect subsequent operations and always manage ownership.
  Failed initialization closes the owned fd. fd 0 is valid and is released;
  0.4's `fd > 0` Close condition leaked it.
- **FileListener duplicates ownership, not socket state.** 0.4 cleared
  nonblocking mode on the duplicate; 0.5 onward sets it. `dup` shares the open
  file description, including `O_NONBLOCK`, socket options, and queues, so the
  original descriptor's behavior changes too. Do not let another owner switch
  either duplicate back to blocking or independently consume the same queue.
- **FileListener source lifetime is pinned since v0.7.4.** It duplicates inside
  `file.SyscallConn().Control`, rather than using an unpinned `file.Fd()`.
  Nil/closed sources return errors; concurrent source Close cannot import a
  recycled descriptor. Close the original file separately. Calling `File.Fd()`
  on a pollable file can change blocking state; do not use it as a neutral query.
- **Close changed in 0.5.** Library Close releases its owned file and wakes
  poller waits, instead of the old explicit `shutdown(SHUT_RDWR)` plus raw close.
  Closing one duplicated owner does not imply shutdown of every duplicate.
  Do not rely on Close to send an application shutdown handshake or an explicit
  SCTP_EOF control. The old connection Close attempted a send after invalidating
  its stored fd and never provided a reliable graceful-handshake contract.
- **PeelOff:** 0.5 fixes the returned fd ABI, initializes its owner/poller, and
  preserves the handler; 0.7 adds CLOEXEC with a fork-safe fallback. v0.7.4
  rejects association IDs outside signed 32-bit range. v0.8.0 message-mode
  connections reject PeelOff with `ErrIOMode`; legacy use remains available.

Repeated Close is not guaranteed to return nil. Nil/uninitialized/closed objects
and raw methods can report different errors (`EINVAL`, `EBADF`, closed-owner
errors); do not reduce all ownership checks to one string comparison.

## 2. Timeouts, deadlines, cancellation, and terminal errors

| Behavior | Compatibility boundary | v0.8.0 contract / required action |
| --- | --- | --- |
| Kernel SO timeouts on blocking syscalls | 0.4 used kernel waiting; 0.5 switched to poller waits without translating these budgets | Do not use 0.5's loss of timeout as the intended contract. Translation was restored in 0.6. |
| SO_RCVTIMEO/SO_SNDTIMEO | Since 0.6, sampled into per-operation poller budgets | Queue waiting counts; changing a socket option does not retime an operation already in flight. Zero removes only this relative limit. |
| Notifications during legacy SCTPRead | 0.4 started another kernel receive after each handler; 0.6 could restart the poll budget; 0.7 fixes the whole-read budget | Notifications cannot extend a read indefinitely. A running synchronous legacy handler still cannot be interrupted by the I/O timer. |
| SetDeadline/SetReadDeadline/SetWriteDeadline | Returned EOPNOTSUPP through v0.7.4; implemented in v0.8.0 | Existing calls that ignored EOPNOTSUPP now install real deadlines, including on wrappers. Audit stale, expired, or zero timestamps and any fallback timeout code. |
| Mutable public deadlines | New in v0.8.0 | Affect current/future waits, including queued operations; zero clears. A listener deadline affects Accept only and is not inherited by its children. |
| Context-aware operations | New opt-in APIs in v0.8.0 | One original context covers setup or I/O; nil contexts are invalid. Cancellation after successful handoff does not close the returned object. Close it explicitly. |
| Accept error result | 0.4 returned an object even after accept failure; intermediate versions could return a typed-nil net.Conn | Since v0.7.2, Accept returns a genuinely nil interface on error. Test err before using the connection. |
| Reset consumed by another operation | Poller regression in 0.5 through 0.7.1 | Since v0.7.2, legacy reads on a known single association drain data/EOF first, then can return ENOTCONN instead of waiting forever. Do not require every termination to be EOF or ECONNRESET. |
| Readiness alone after CONNECTX | A nonblocking call can return EINPROGRESS; this is visible to raw callers | High-level Dial waits for completion. Public low-level SCTPConnect retains raw errno behavior and does not become a context-aware dial. Do not blindly repeat state-changing connect calls after EINTR. |
| Accept publication | v0.8.0 uses the context-aware accept path even for old AcceptSCTP | Cancellation/Close/deadline observed before final child publication can discard and close the accepted child. A completed accept4 syscall alone is not application ownership transfer. |

Use the following error identities, rather than text or a blanket retry-on-error:

| Path | Expiry/termination identity |
| --- | --- |
| Legacy SO-limited Read/Write/Accept, and public RawConn polling | `syscall.EAGAIN` on relative timeout |
| Legacy high-level connection wait with SO_SNDTIMEO | `syscall.EINPROGRESS` on its relative timeout; unrelated connection errors remain distinct |
| Standard connection/listener deadline | `errors.Is(err, os.ErrDeadlineExceeded)` |
| Context cancellation / expiry | `context.Canceled` / `context.DeadlineExceeded` |
| Context-aware message/accept operation's SO timeout | `ErrSocketTimeout`, also matching `syscall.EAGAIN` |
| Close of an active/queued wait | Closed-owner error, normally matching `net.ErrClosed`; invalid-object entry points may differ |
| Legacy known-association loss after a consumed reset | `syscall.ENOTCONN` |
| Message boundary/restart invalidation | Preserve ReadResult facts; honor CloseRequired, ErrReadUnusable, and ErrAssociationChanged |

`SetSocketTimeouts(0, 0)` does **not** clear standard deadlines; use
`SetDeadline(time.Time{})` separately. The two SO setsockopt calls are not a
transaction. `SocketTimeoutUpdateError.ReadApplied` identifies partial progress;
close a connection after failed exclusive takeover. Fresh bounded-setup sockets
start with zero SO timeouts; bounded listeners also clear them on accepted children.

The legacy consumed-reset fix is scoped to known single associations, including
library-created peeled sockets. An arbitrary one-to-many endpoint must remain
able to serve other associations; an assoc_id=0 query failure is not a generic
endpoint-termination signal.

Sources: [0.6 timeout restoration](https://github.com/skill215/sctp/compare/0.5...0.6),
[0.7 timeout/ABI changes](https://github.com/skill215/sctp/compare/0.6...0.7),
[v0.7.2 corrections](https://github.com/skill215/sctp/blob/v0.7.2/releases/v0.7.2.md),
[v0.8.0 wait controller](https://github.com/skill215/sctp/blob/v0.8.0/socket_deadline.go).

## 3. Source compatibility, constants, and ABI

The Linux/amd64 exported-API audit found **no removed exported names and no
changed signatures among existing exported functions/methods** between 0.4 and
v0.8.0. It found **14 changed existing constant values** and **one changed
exported struct-field list: SocketConfig**. Private object layouts grew and are
not a stable serialization/unsafe-layout interface. Do not copy live connection
or listener values; use their pointers.

`SocketConfig.Messages` is inserted before the old fields in v0.8.0. Old unkeyed
literals such as `SocketConfig{control, handler, init}` no longer compile. Use
`SocketConfig{Control: control, NotificationHandler: handler, InitMsg: init}`
for legacy mode; set Messages only when intentionally migrating I/O modes.
Reflection-based configuration and field-order assumptions also need review.
Nil configuration receivers now return ErrInvalidConfig instead of panicking.

### Changed numeric values

| Constant | 0.4 value | v0.8.0 value | Changed in |
| --- | ---: | ---: | --- |
| SCTP_INACTIVE | 2 | 0 | 0.5 |
| SCTP_ACTIVE | 1 | 2 | 0.5 |
| SCTP_UNCONFIRMED | 0 | 3 | 0.5 |
| SCTP_CLOSED | 0 | 1 | 0.5 |
| SCTP_BOUND | 1 | -1 | 0.5 |
| SCTP_LISTEN | 2 | -2 | 0.5 |
| SCTP_COOKIE_WAIT | 3 | 2 | 0.5 |
| SCTP_COOKIE_ECHOED | 4 | 3 | 0.5 |
| SCTP_ESTABLISHED | 5 | 4 | 0.5 |
| SCTP_SHUTDOWN_PENDING | 6 | 5 | 0.5 |
| SCTP_SHUTDOWN_SENT | 7 | 6 | 0.5 |
| SCTP_SHUTDOWN_RECEIVED | 8 | 7 | 0.5 |
| SCTP_SHUTDOWN_ACK_SENT | 9 | 8 | 0.5 |
| SCTP_EOF | 16 (0x10) | 512 (0x200) | v0.7.2 |

The state corrections match Linux UAPI. SCTP_BOUND/SCTP_LISTEN are deprecated
sentinels, not states returned by Linux SCTP_STATUS. New constants include
SCTP_EMPTY=0, SCTP_PF=1, and SCTP_UNKNOWN=65535. Do not confuse StatusState with
the separate SCTPState notification enum. Update persisted/configured numeric
values, fixtures, switches, and any hard-coded EOF flag. Already-correct Linux
wire values should not be mechanically remapped as if they were old Go constants.

### Metadata and ABI corrections

| Area | Change | Consumer impact |
| --- | --- | --- |
| Default-send PPID | GetDefaultSentParam/SetDefaultSentParam convert host/network order consistently since 0.7 | Remove manual byte-swapping workarounds for these methods. SCTPRead/SCTPWrite already exposed host-order PPID in 0.4; their intended order did not change. |
| Nil default-send metadata | Since 0.7, SetDefaultSentParam(nil) returns EINVAL instead of dereferencing nil | Handle the error; nil is not a request to reset defaults. |
| Caller SndRcvInfo | 0.5 copies before swapping PPID for a send | Shared read-only send metadata no longer undergoes a transient library mutation; callers still must not mutate it concurrently. |
| Ancillary decode | 0.7 checks length and decodes into owned aligned values | Truncated/unaligned input is rejected or decoded safely instead of panicking/aliasing raw memory. |
| Getsockopt length | 0.7 consistently uses a uint32 Linux socklen_t and removes the incorrect s390x pointer rewrite | Public uintptr signature is unchanged; pass a pointer to uint32, not a pointer to a machine-sized length. Remove architecture-specific workarounds for the old bug. |
| PeelOff result fd | 0.5 uses the kernel's fixed-width int32 field | Do not depend on the old machine-int layout. |
| Wrapper representation | Still a 32-byte native-endian SndRcvInfo header followed by payload | No new wire framing or endian-neutral serialized format is introduced. Header encoding is local to the wrapper. |

SCTP remains the Linux kernel protocol. There is no new application handshake,
automatic PPID negotiation, or version byte on the wire. Interoperability can
nevertheless change when a program relied on an incorrect PPID/EOF constant,
descriptor inheritance, message boundaries, or an old error path.

## 4. Existing I/O and wrapper behavior

| Operation | Old behavior corrected | Current behavior / migration |
| --- | --- | --- |
| Read/SCTPRead with an empty buffer | Could consume/drop queued payload or notifications | Since v0.7.2, a live connection returns zero without consumption. Nil/closed/failed objects still return errors; empty reads are not liveness probes. |
| Wrapper Read with exactly 32 bytes | Could consume data without room for payload | Since v0.7.2, buffers of at most 32 bytes return 0, EINVAL without consumption. |
| Empty legacy Write/SCTPWrite | Go's ancillary send helper could inject a byte | Since v0.7.2, perform a real zero-payload kernel send. Ordinary empty messages can fail; valid EOF/ABORT controls remain subject to kernel/socket applicability. Do not treat empty writes as universal no-ops. |
| Header-only wrapper Write | Could count a rejected header as consumed | Count 32 only after a successful zero-payload control send; return 0 on a failed send without progress. |
| Read/write error counts | Could expose negative counts or fictitious wrapper headers | Nonnegative counts; no header-only success on EOF/error; valid partial receive data remains observable with its error. Check both n and err. |
| Unaligned wrapper input | Unsafe casts could fail on strict-alignment targets | Safe decoding since 0.7; the header size and native-endian format are unchanged. |
| Missing wrapper metadata | Reused buffers could retain stale header bytes | Since v0.7.2, clear the entire header. A zero header does not distinguish a notification from valid stream0/PPID0 data. |
| Wrapper construction | Replaced existing subscriptions and ignored initialization failure | Since v0.7.2, merge DATA_IO under the configuration lock. Save initialization errors for subsequent I/O; Close still delegates ownership cleanup. Reconstruct after fixing configuration. |
| Public SubscribeEvents | Replaces the event mask | Still replacement, not additive subscription. Only the wrapper's internal DATA_IO operation merges. |
| Legacy notifications | Raw notification bytes or synchronous handler | Still the legacy contract. They do not automatically become typed events after upgrading the module. |

The legacy wrapper does not gain message reassembly or EOR visibility. A zero
count on failure is an I/O fact, not permission to replay an application request
whose processing status is unknown.

## 5. Address handling and stricter input validation

| Input/query | First change | Current contract / migration |
| --- | --- | --- |
| Empty/unsupported network names | 0.7 | Legacy Listen/Dial normalize empty to sctp; invalid names fail before socket creation instead of suffix-based acceptance or an empty-string panic. Resolve already accepted empty network names. Bounded setup requires an explicit sctp/sctp4/sctp6. |
| IPv6 zone indexes | 0.7 | Existing interface names, including numeric names, take precedence; otherwise parse a uint32 index. Returned scope IDs without a name retain a numeric string. Old unresolved numeric zones could become scope 0. |
| Mixed IPv4/IPv6 address queries | 0.7 | Decode each sockaddr's family/length and validate bounds; do not assume fixed-family strides or identical local/peer optlen conventions. |
| Listen/Dial address mutation | v0.7.2 | Copy SCTPAddr, IPAddrs backing storage, and IP bytes before wildcard completion. Code relying on the caller's empty IPAddrs being filled must instead query the returned object. |
| Structured ports/IPs | v0.7.2 | Negative/>65535 ports and malformed nonempty IPs return net.AddrError before setup/Control. Local port 0 remains valid; legacy remote port 0 retains kernel behavior. |
| Nil required address | v0.7.2 | High-level Dial and low-level SCTPConnect/SCTPBind reject missing required addresses instead of panicking. A nil local address remains valid for high-level setup. |
| Empty multihoming hosts | v0.7.2 | Reject leading/intermediate/trailing empty hosts instead of silently dropping explicit prefixes. Single :port wildcards remain valid. |
| Complete address-query capacity | v0.7.2 | Grow 4 KiB to a 1 MiB address-area budget on ENOMEM, at most nine attempts; return an error wrapping ENOMEM at the cap, not a partial list. Do not assume the old fixed 4 KiB allocation. |
| SetInitMsg integer arguments | v0.7.4 | Each must be 0..65535; invalid live-socket input returns EINVAL instead of uint16 wrapping. Zero retains the kernel's existing/default value. |
| Address-query/PeelOff association IDs | v0.7.4 | Reject values outside the signed 32-bit ABI instead of truncating to a different association. Negative values that fit int32 remain subject to kernel semantics. |
| BoundAddr snapshot | v0.8.0 | Owned explicit binding plus actual port, available after Close; does not enumerate interfaces or turn wildcards into routable addresses. Imported FileListener returns ErrBoundAddrUnavailable. |

`ToRawSockAddrBuf` keeps its no-error serialization signature; networking entry
points perform validation. `Addr`/`LocalAddr`/`RemoteAddr` still cannot return an
error and return nil on query failure. Use the error-returning address methods
when failure diagnosis matters. Old Listen paths also capture a binding snapshot
in v0.8.0 and can fail if that setup query fails; this does not make them bounded
input APIs or change their dynamic Addr query into BoundAddr.

## 6. Opt-in v0.8.0 APIs are not interchangeable legacy calls

### Message mode

Leave `SocketConfig.Messages` nil to retain legacy I/O. A non-nil pointer selects
a copied, immutable configuration before setup; a pointer to a zero-value
MessageOptions is invalid because ReceiveInfoSNDRCV must be explicit.

| Migration choice | Consequence |
| --- | --- |
| Enable Messages | Use ReadEvent/WriteMsgContext. Read/Write, SCTPRead/SCTPWrite, and wrapper I/O return ErrIOMode. Both modes still use SCTPConn; implementing net.Conn does not make byte-stream adapters valid in message mode. |
| Reconfigure subscriptions/default send options | SubscribeEvents, wrapper DATA_IO initialization, SetDefaultSentParam, public Setsockopt, and PeelOff reject message mode with ErrIOMode. Configure before setup. |
| Raw descriptor access | SyscallConn remains exposed; bypassing the guards is outside immutable-message guarantees, not a supported workaround. |
| Set notification events | Only association, peer-address, old send-failure, shutdown, and partial-delivery subscriptions are accepted. DATA_IO is enabled internally; do not pass SCTP_EVENT_ALL or the DATA_IO bit in MessageOptions.Events. |
| Default options | FragmentInterleave must be 0. NoDelay and AllowFragmentation are explicit booleans; false AllowFragmentation disables network fragmentation and can cause large sends to fail. Requested options are verified, not silently downgraded. |
| Receive records | ReadEvent separates data/notification buffers, exposes Consumed/InfoPresent/EOR, and requires nonoverlapping buffers sized from MessageIORequirements. It returns fragments, not whole application frames. |
| Continuity and streams | Missing/inconsistent metadata, truncation, and out-of-range incoming streams fail closed. Use actual negotiated Instreams/Ostreams; there is no dynamic stream reset/expansion support. |
| Write controls | WriteMsgContext requires nonempty payload, valid SID/association, and only the supported unordered flag. It is not a replacement for legacy EOF/ABORT control sends or TTL/PR-SCTP semantics. |
| Short/uncertain writes | One message send, no automatic tail retransmission. Preserve positive n with errors; short success reports io.ErrShortWrite. |
| Typed notification lifetime | ParseNotification owns fixed fields but borrows Tail. Unknown types remain raw; newer SEND_FAILED_EVENT is not decoded as old SEND_FAILED. Copy retained tails within an explicit application budget. |
| Terminal notifications | Preserve valid consumed RESTART/partial-abort facts before acting on accompanying errors/CloseRequired. Reject malformed/unconsumed records as events. Close the invalid generation; subsequent reads may return ErrReadUnusable. |
| Oversized notifications | ErrNotificationTooLarge leaves the record unconsumed and reports RequiredNotificationBytes. Retry only within a chosen cap, or close; do not allocate without a bound. |

No background reader drains notifications. Keep one reader per association and
subscribe to association changes if restart detection is required. PPID0 and
streams above zero are transport capabilities, not automatic activation of an
application's optional protocol modes. NewSCTPConn/FileListener do not retrofit
this message configuration onto arbitrary imported sockets.

### Context and bounded setup

`SocketConfig.DialContext` accepts pre-resolved addresses and rejects Control,
including in legacy mode. A legacy NotificationHandler remains allowed when
Messages is nil, but its synchronous execution is not cancellable. Pair
ResolveSCTPAddrContext and DialContext with the same original context. The new
resolver uses the Go resolver and selects one suitable IP per explicit host;
it is not a DNS-derived multihoming/retry algorithm.

`SetupConfig`/NewDialer/ListenContext add stricter rules beyond those legacy calls:

- Explicit combined local+remote bounds: N=1..64 addresses, Z=0..4096 zone bytes,
  D=1..16384 endpoint-text bytes. No zero-as-unlimited convention.
- Numeric ports only; nonempty remote host and nonzero remote port. Legacy
  ResolveSCTPAddr and ResolveSCTPAddrContext retain service-port lookup.
- No Control or NotificationHandler, even with Messages=nil. No implicit
  fallback, reconnect, alternate-host discovery, or replay.
- Owned endpoint/configuration copies and one original setup context; new
  context/bounded IPv6 setup checks required IPV6_V6ONLY operations rather than
  inheriting the legacy helper's ignored error.
- Returned children/listeners/connections require explicit Close; canceling the
  old setup context after handoff is not a lifetime-management mechanism.

SetupRequirements is a new versioned logical ledger, not a Go allocator/RSS
guarantee or kernel capability probe. It already includes I/O scratch and a fixed
notification in ConnectionBytes; EstablishBytes includes the connection being
established. Add caller buffers/state and overlapping old/new candidates. The
fixed ledger assumes one reader, one writer, one Accept caller, no queued I/O,
controlled contexts, and bounded serial deadline updates. Do not apply the old
intermediate 8192-byte candidate reserve or these figures to arbitrary concurrency.
See [the full bounded setup contract](bounded-setup.md).

## 7. Toolchains, platforms, module versions, and examples

- The module/import path remains `github.com/skill215/sctp`; this is a v0 module,
  without a `/v0` suffix. No third-party module or C-library dependency is added.
- The Go directive changes from **1.18 to 1.26.0 at 0.7**, not at v0.8.0.
  Upgrade toolchains/build images before migrating; copying old binaries or
  relying on the old minimum does not validate new source compatibility.
- Linux excluding 386 remains the implemented SCTP runtime across these tags.
  Non-Linux/386 stubs do not become native SCTP support. Since v0.7.2,
  ErrUnsupported is exported on supported builds too, and unsupported Listener
  has SyscallConn. Unsupported NewSCTPConn now immediately releases the owned
  socket with the platform close primitive, including fd 0, and retains cleanup
  errors while matching errors.Is(ErrUnsupported). Do not double-close that fd.
- Historical `0.4` through `0.7.1` tags lack Go's canonical `v` prefix and may
  resolve through tag queries as pseudo-versions. Canonical releases start at
  v0.7.2 in this interval. Pin `github.com/skill215/sctp@v0.8.0` and regenerate
  go.sum through an actual download; do not rename an old pseudo-version's sum.
- v0.7.3 changes the legacy CLI's stream selection to cycle within negotiated
  Ostreams while PPID increases independently. v0.7.4 rejects invalid example
  address lists and stops kernel buffer readbacks from altering later clients'
  requested configuration. Scripts scraping example logs or depending on its
  old failure behavior need adjustment; these are example changes, not a new
  SCTP wire protocol.
- The current README/message example was refreshed after the v0.8.0 tag. That
  documentation commit does not retag the release or change its library code.
  Do not mistake a newer checkout's example files for files in the immutable tag.

## 8. Consumer migration checklist

1. Pin the exact module version and update the Go toolchain. Replace unkeyed
   SocketConfig literals. Review unsafe layouts, serialized constants, and
   byte-swapping workarounds.
2. Inventory raw fd users: direct accept/recv/send, FileListener, NewSCTPConn,
   dup/exec, SetNonblock, File.Fd, and foreign event loops. Establish one clear
   owner and keep library-owned descriptors nonblocking.
3. Choose legacy or message mode deliberately. Do not enable Messages underneath
   code that still calls net.Conn.Read/Write, wrappers, or notification callbacks.
4. Audit every SetDeadline call, including ones that previously ignored
   EOPNOTSUPP. Distinguish standard, context, SO timeout, Close, reset, and boundary
   errors. Use one absolute budget across a frame and all its notifications.
5. Check address input/ranges and snapshot assumptions; preserve wildcard versus
   routable-address policy. Use queried stream counts and resource requirements.
6. Verify empty I/O, wrapper counts/headers, notification ownership, short writes,
   and valid terminal facts. Do not add automatic application replay as a fix.
7. Run native integration checks for idle Accept, arrival, cancellation, Close,
   SO/public deadlines, imported fd aliases, consumed resets, payload/stream/PPID,
   fragmented EOR, and application-budget retirement on your actual kernel.

## 9. Evidence and limits

The [audit JSON](repro/compatibility-v0.4-v0.8.0/audit.json) records exact tag SHAs,
API changes, and native results. The [API scanner](repro/compatibility-v0.4-v0.8.0/api.go.txt)
type-checks exported names, signatures, public struct fields, and constant values.
The [runtime probe](repro/compatibility-v0.4-v0.8.0/probe.go.txt) uses real loopback
SCTP in separate bounded processes. Reproduce with the
[runner](repro/compatibility-v0.4-v0.8.0/compare.py.txt):

```sh
python3 docs/repro/compatibility-v0.4-v0.8.0/compare.py.txt
```

Observed on Go1.26.10, Linux/amd64, kernel 6.6.114.1-microsoft-standard-WSL2:

| Observation | 0.4 | 0.5 | 0.6 | 0.7 | v0.7.2 / v0.7.4 | v0.8.0 |
| --- | --- | --- | --- | --- | --- | --- |
| Accepted O_NONBLOCK / CLOEXEC | no / no | yes / yes | yes / yes | yes / yes | yes / yes | yes / yes |
| Public Accept waits without an arrival | yes | yes | yes | yes | yes | yes |
| 80ms SO_RCVTIMEO ends idle Accept before 500ms safety Close | yes | **no** | yes | yes | yes | yes |
| Non-nil net.Conn interface on Accept error | yes | yes | yes | yes | no | no |
| Default PPID 0x01020304 received as requested | no (0x04030201) | no | no | yes | yes | yes |
| Clear standard deadline succeeds | no | no | no | no | no | yes |
| FileListener makes a blocking source alias nonblocking | no | yes | yes | yes | yes | yes |

All historical sources were compiled with the **same current Go1.26.10** using
local replaces and `-buildvcs=false`. These are behavioral comparisons of exact
tag sources, not original-toolchain certification or public module download
proof. The 0.5 timeout was stopped by the probe's safety Close and is recorded as
a reproduced regression, not a successful timeout. The 100ms no-arrival check is
a bounded observation; the waiting contract is additionally established by source
and the existing regression suite. No cross-architecture or second-kernel run is
claimed here. API counts are for Linux/amd64; unsupported-platform differences
were reviewed separately in source.

The raw-read example above compiled against v0.8.0. The following existing
regressions passed together with `-race -count=1` on the same native host:
TestAcceptedSocketCloseOnExec,
TestRawConnReadWaitForReadiness, TestRawConnPinsFDDuringCallback,
TestListenerCloseUnblocksAllAccepts, TestAcceptSocketTimeout,
TestReadSocketTimeoutIncludesNotifications, TestFileListenerSourceOwnership,
TestStabilityReadAfterConsumedReset, TestStabilityAcceptErrorNil,
TestMutableDeadlineRealSCTP, and TestAcceptContextPublicationBarriers.
The audit catalogs library-visible changes; it does not claim that every external
adapter or deployment is automatically compatible.
