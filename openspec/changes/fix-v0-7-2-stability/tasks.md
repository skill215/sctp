## 1. Fix the implementation baseline and acceptance evidence

- [x] 1.1 Prepare implementation on codex/fix-v0-7-2-stability, record the starting commit and existing uncommitted audit evidence, and preserve historical tags and evidence.
- [x] 1.2 Map all 14 blockers to formal test names; record 0.7.1 failure causes and normal controls, excluding observation assertions that expect the old erroneous behavior.
- [x] 1.3 Verify the valid SCTP_STATUS structure size, single-association EINVAL boundary, and PeelOff type locally and on another native Linux kernel; retain rerunnable probes for the design criteria.
- [x] 1.4 Confirm explicit expectations for empty reads, real empty-write syscalls, default control flags, header-only frames, partial data with errors, and object-error precedence. Update the design first if they differ.

## 2. Batch A: reads and wrapper integrity

- [x] 2.1 Add #4 non-consumption regressions for nil/empty and 32-byte wrapper buffers, notifications, and queued data; verify the subsequent marker payload remains complete.
- [x] 2.2 Implement zero-capacity receive short-circuiting and short-wrapper-buffer errors, preserving errors for nil/closed/initialization-failed objects.
- [x] 2.3 Add #12 regressions for real reset, EOF, timeout, Close, and io.ReadAll; eliminate negative/fictitious SCTPRead and wrapper counts while retaining valid partial data.
- [x] 2.4 Add #9 SENDER_DRY, buffer-reuse, and normal-metadata controls; zero the entire header in frames without metadata.
- [x] 2.5 Add #8 regressions for preserved events, repeated wrapping, concurrent configuration, and actual handler delivery; implement eventsMu and internal configuration helpers.
- [x] 2.6 Retain and propagate wrapper subscription initialization errors; verify Read/Write perform no hidden I/O and Close still releases underlying resources.
- [x] 2.7 Run affected I/O, notification, codec, timeout, and lifecycle race tests under both Go versions; independently review #4/#8/#9/#12 fix boundaries.

## 3. Batch B: association termination and receive liveness

- [x] 3.1 Cover #1 through Read/SCTPRead/wrapped entry points, including unconsumed reset, reset consumed by Write/raw I/O, already-blocked reads, concurrent reads/writes, and repeated reads.
- [x] 3.2 Preserve single-association provenance for Dial/Accept/PeelOff; verify NewSCTPConn classification of connected raw fds and conservative handling of unknown endpoints.
- [x] 3.3 Query SCTP_STATUS with a valid ABI in the pinned-fd callback only after receive EAGAIN. Return ENOTCONN when the association disappears; never disguise other errors as EOF.
- [x] 3.4 Verify controls for live idle connections, one-to-many endpoints with no/other associations, peeled connections, and buffered data; introduce no busy waiting or premature closure.
- [x] 3.5 Regress the single notification budget, queued I/O budget, Close wakeup, normal SHUTDOWN, and Read/Write/Accept/Connect timeouts.
- [x] 3.6 Validate batch B on at least two native Linux kernels with recorded versions and both Go versions; separately review this regression fix.

## 4. Batch C: empty payloads and control sends

- [x] 4.1 Add #5 regressions for empty ordinary writes, empty metadata writes, and header-only data frames; use raw sendmsg/marker payloads to prove no byte injection.
- [x] 4.2 Add a private Linux sendmsg helper for all empty payloads within the existing Write gate, EINTR/EAGAIN, and timeout flow; preserve ancillary data and object lifetimes.
- [x] 4.3 Preserve kernel default send parameters; verify explicit controls, info=nil default ABORT, rejected ordinary empty messages, and zero wrapper counts on failure. Do not silently turn empty writes into no-ops.
- [x] 4.4 Define EOF=0x200 explicitly for #6; verify one-to-many EOF, ABORT, abort reasons, and kernel errors for control combinations/inapplicable sockets.
- [x] 4.5 Verify unchanged caller SndRcvInfo, PPID byte order, zero failed-send counts, and wrapped-frame retries.
- [x] 4.6 Verify zero-iov/ancillary ABI on 32-bit and big-endian targets, including alignment, using non-SCTP sockets; then run native SCTP control-message regressions.

## 5. Batch D: address preparation and queries

- [x] 5.1 Add #2 input-immutability/race regressions for shared Listen/Dial addresses, len=0/cap>0 aliases, IPv4/IPv6, and failure paths.
- [x] 5.2 Implement deep address copies and internal wildcard completion, preserving the original address-string semantics observed by Control and existing zone handling.
- [x] 5.3 Add #7/#10 regressions for negative/65536/large-integer ports, nil raddr, all high-level entry points, and low-level Bind/Connect; assert rejection before socket creation and Control.
- [x] 5.4 Implement net.AddrError validation in address preparation, preserving local port 0, nil laddr, and existing public signatures.
- [x] 5.5 Add #11 IPv4/IPv6, empty-host-position, multi-segment, and valid single-segment wildcard controls; parse without dropping prefixes.
- [x] 5.6 Implement #14 bounded queries from 4 KiB to 1 MiB, resetting headers/optlen and preserving local/peer length differences, errno, and integrity checks.
- [x] 5.7 Verify native queries with 300 IPv4 addresses, mixed families, IPv6 stride, growth boundaries, persistent ENOMEM, other errors, invalid counts, and truncated buffers.
- [x] 5.8 Run address-related race/fuzz tests and existing numeric-zone/network-name regressions under both Go versions; record observable address-query budget behavior.

## 6. Batch E: platform API and ownership

- [x] 6.1 Establish the #15 external-consumer compilation matrix, referencing ErrUnsupported and asserting that Conn/Listener implement syscall.Conn.
- [x] 6.2 Move ErrUnsupported to shared code and add unsupported Listener.SyscallConn, preserving the sentinel and normal Linux error behavior.
- [x] 6.3 Add #16 regressions for real Linux/386 SCTP fds, fd=0 in a subprocess, descriptor-number reuse, and concurrent/repeated Close.
- [x] 6.4 Immediately release owned fds on unsupported construction failure, set fd=-1 and initErr, and use correct Unix/Windows socket-close helpers without handle truncation or close retries.
- [x] 6.5 Verify real Winsock handle release on native Windows; verify cleanup errors are retained and errors.Is(ErrUnsupported) remains true.
- [x] 6.6 Address #13: prefer returning a nil interface with the original Accept error. If deferred, record its P3 non-blocking status without closing it as fixed.
- [x] 6.7 Independently review #15 and #16 acceptance, initialization-failure resource paths, and public API compatibility without adding protocol-support commitments.

## 7. Batch F: integration review and CI gates

- [x] 7.1 Independently review A–E: lock ordering, buffer/metadata counts, raw-pointer lifetimes, fd reuse, and error precedence; use normal controls for new concerns.
- [x] 7.2 Run ten full race/shuffle rounds per Go version in independent processes with timeout=120s per round, stopping on failure; record Go patch version, seed, and kernel.
- [x] 7.3 Pass vet, strict checkptr, low GOGC, and four fuzz smoke categories under both Go versions; retain seeds and failure reproduction procedures.
- [x] 7.4 Update six-architecture QEMU ABI gates with new sendmsg and address-growth boundaries; distinguish compilation, non-SCTP ABI checks, and native protocol evidence.
- [x] 7.5 Add native Linux/386 and Windows unsupported-resource tests plus five-target external-consumer compilation; required jobs must execute rather than skip.
- [x] 7.6 Actually run three rounds of numeric interface-name/index conflict tests in an isolated namespace; verify that missing required privileges fail the designated CI job.
- [x] 7.7 Compile and natively run README echo/timeout examples, checking payload, stream, and PPID; update fix evidence for all 14 issues and #13 status.

## 8. Release preparation and subsequent publication

- [x] 8.1 Update README, audit/release plans, and v0.7.2 notes with empty-operation semantics, wrapper headers, address budgets, platform cleanup, and #1 regression provenance.
- [x] 8.2 Make the release workflow accept v0.* tags, retain old tag targets, and require all newly added jobs and annotated-tag validation before publication.
- [x] 8.3 Verify candidate APIs, examples, and version availability in an independent module; confirm all 14 blocking conditions are satisfied and explicitly record any #13 deferral.
- [x] 8.4 After receiving publication instructions and passing all gates, create canonical annotated tag v0.7.2 and publish. Do not execute publication during the design stage.
- [x] 8.5 After publication, verify matching tag/release commits, complete notes, and canonical resolution of go get @v0.7.2. Preserve failure facts and use a new patch-version process if needed.
