# v0.7.2 implementation evidence

Baseline: 70b338ac419dff6aa745460bb379d68208d615e0 (library identical to 0.7.1).
Branch: codex/fix-v0-7-2-stability. Existing historical audit fixtures and
release-plan edits were present before implementation and are preserved.

## Batch A — receive counts, metadata, event subscriptions

Regression file: sctp_stability_io_linux_test.go. Before the fix, the new tests
observed empty SCTPRead returning 1 and consuming input, reset reads returning
-1, and io.ReadAll panicking. Existing audit evidence covers the independent
missing-metadata and overwritten-subscription triggers.

After the fix: focused race suite passed three repetitions; full race suite
passed with Go 1.26.10 and Go 1.27.2, Linux/amd64 kernel
6.6.114.1-microsoft-standard-WSL2. Existing Read/Write/Accept/Connect timeout,
notification-budget, lifecycle, metadata and codec tests passed.

Review: eventsMu covers only configuration syscalls; wrapper construction uses
one read-modify-write operation. No lock spans handlers or blocking I/O. Setup
failure returns before I/O but Close still delegates. Zero reads validate the
object first. Error reads report zero; successful bytes plus metadata errors
retain a fully initialized frame. Input buffers remain untouched on short reads.

The SCTP_STATUS ABI/scope probe passes three local repetitions. Additional
native-kernel evidence is still required before batch B is implemented.

## Native liveness design gate

Actions run 41162240590 passed all four probe jobs before implementing B:
Ubuntu 22.04 kernel 6.8.0-1070-azure and Ubuntu 24.04 kernel 7.0.0-1017-azure,
Go 1.26.x/1.27.x, three probe repetitions per job. Together with local WSL2
6.6 these confirm Status=176 bytes, single-association EINVAL, endpoint
exclusion, and SOCK_SEQPACKET peeled scope. See native-status-probes.log.

## Batch C — zero-payload sends and EOF

Before: metadata empty send returned 0,nil; wrapper empty frame returned 32,nil;
EOF was 0x10. After: three focused race repetitions and full race suites on
Go 1.26.10/1.27.2 passed. Actual EOF, ABORT, abort reason, wrapper EOF and
post-error marker payloads are covered. Non-SCTP SCM_RIGHTS zero-iov ABI test
passes natively; QEMU verification remains a later gate.

Review: the new raw sendmsg helper is only for empty payloads, within the
existing direction gate and retry/deadline loop. It retains ancillary memory
through KeepAlive and platform Msghdr layouts. Nonempty path and metadata
copies remain unchanged. Failed counts are normalized to zero.

Kernel nuance: zero-length sends with default ABORT are rejected on the local
kernel before defaults are applied; the test compares directly with raw sendmsg,
then compares nonempty default-parameter sends with syscall.SendmsgN as well.
The local kernel does not apply the non-defaultable ABORT bit in that path.
The tests do not assert unsupported controls or swallow EINVAL as a no-op.

## Batch B — terminated association liveness

The pre-fix suite failed all six Read/SCTPRead/wrapped × Write/raw-reset-consumer
cases, queued-reset and peeled-reset cases (continued waiting). After the fix,
full race suites pass on Go 1.26.10 and 1.27.2 locally. Explicit controls cover
idle, active/queued reads, concurrent reset, imported stream, shared endpoint,
peeled association, and buffered normal shutdown. Existing timeout tests pass.

Review found and corrected a classification error: SOCK_STREAM alone also
matches AF_UNIX sockets used by ABI tests. Imported sockets now require
SO_PROTOCOL=132 before enabling SCTP liveness queries. Known Dial/Accept and
PeelOff origins remain explicit. No SO_ERROR is consumed. Actual recvmsg data
is handled before querying; unknown status errors are returned without EOF
conversion. The original deadline spans all notifications and queries.

Cross-kernel post-fix validation remains pending (task 3.6).

## Batch D integration review and B race correction

A Go 1.27 full-suite run caught a B race during ordinary stream-test shutdown:
recvmsg initially returned EAGAIN, the association ended during the status
query, and the library returned ENOTCONN before consuming the now-available
normal EOF. The terminated branch now retries recvmsg once (restarting EINTR)
to deliver intervening data/EOF first; only a second EAGAIN becomes ENOTCONN.
A 50-connection concurrent shutdown/drain regression and five stream/stability
stress repetitions pass, followed by both complete Go race suites.

D's pre-fix regressions show shared-address races/mutation, invalid ports reaching
Control, nil-remote panics, empty multihoming host acceptance and Addr=nil for
300 addresses. After the fix, both full race suites pass. Raw 16 KiB query
controls confirm actual binding. Synthetic query replies test fresh retry
headers, local/peer optlen, mixed families, nine attempts/1 MiB, non-memory
errors, counts and truncation. Deep snapshots and CONNECTX3 KeepAlive were
reviewed. Query growth occurs only on ENOMEM and never parses failed replies.
