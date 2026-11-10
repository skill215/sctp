# SCTP v0.7.2 Release Goals

Prepared: 2026-10-30. Design updated: 2026-10-31. Implementation baseline: 0.7.1, commit e864ee06b6c5b0c77661c4715814a75576c23237; the implementation at starting master 70b338a is identical.

**Status: [v0.7.2](https://github.com/skill215/sctp/releases/tag/v0.7.2) was published on 2026-10-31 after all gates passed.** v0.7.2 fixes 14 defects/input-validation gaps confirmed through reproduction and root-cause isolation, and implements the P3 non-blocking API consistency improvement in #13. It retains Go 1.26 as the minimum, public function signatures, the 32-byte wrapper header format, and existing tag targets.

The complete plan is in OpenSpec change **fix-v0-7-2-stability**:

- [Proposal](../openspec/changes/fix-v0-7-2-stability/proposal.md): scope, compatibility, and impact.
- [Technical design](../openspec/changes/fix-v0-7-2-stability/design.md): kernel criteria, empty payloads, address budgets, platform cleanup, and trade-offs.
- [Implementation checklist](../openspec/changes/fix-v0-7-2-stability/tasks.md): tasks in dependency order.
- [Behavior specifications](../openspec/changes/fix-v0-7-2-stability/specs/): verifiable scenarios for six capability areas.

Public progress and original reproductions are tracked in [GitHub #3](https://github.com/skill215/sctp/issues/3). The user authorized implementation and publication; v0.7.2 may be tagged and published only after all required validation passes.

## Current platform support (2026-10-31)

The library supports only Linux with kernel SCTP enabled. Linux/386 does not support SCTP I/O. Non-Linux platforms, including Windows, macOS, BSD, and Solaris/illumos, are outside the support scope; compilation, testing, and runtime compatibility are not guaranteed.

The records below retain the cross-platform compilation, stub, and resource-cleanup checks actually performed for v0.7.2. They do not expand platform support. Reproducible test-compilation issues on non-Linux targets are compatibility issues outside the support scope, not Linux SCTP runtime regressions or current release blockers. See [platform details](comparison.md#go-version-and-platform-constraints) for architecture and validation boundaries.

## Defects and fix boundaries

| Issue | Scope | Design choice |
| --- | --- | --- |
| [#1](https://github.com/skill215/sctp/issues/1) | P1, receive regression since 0.5 | Deliver data first; only after EAGAIN, query SCTP_STATUS with a valid ABI in a known single-association scope. Return ENOTCONN when the association is absent; idle and one-to-many endpoints continue waiting. Preserve the poller and original timeout budget. |
| [#2](https://github.com/skill215/sctp/issues/2) | P2, shared-address race | Copy addresses and IP storage before filling internal wildcard values; never mutate caller or aliased slices. |
| [#4](https://github.com/skill215/sctp/issues/4) | P1, empty receive loses data | Empty Read/SCTPRead on a live connection is a zero-count no-op. Wrapper buffers of at most 32 bytes return 0, EINVAL without receiving data. |
| [#5](https://github.com/skill215/sctp/issues/5) | P1, empty send injects a byte | All empty payloads use a real zero-payload sendmsg, retaining explicit/default controls and kernel errors. Rejected ordinary empty messages inject no byte and report no wrapper-header consumption. |
| [#6](https://github.com/skill215/sctp/issues/6) | P2, EOF constant | Define EOF=0x200 explicitly and validate it together with zero-payload control sends. |
| [#7](https://github.com/skill215/sctp/issues/7) | P2, structured port validation | Reject negative ports and ports above 65535 before kernel operations; retain local port 0. |
| [#8](https://github.com/skill215/sctp/issues/8) | P2, subscription overwrite | Merge the old mask with DATA_IO under a configuration lock. Constructors without an error return retain initialization errors for subsequent I/O. |
| [#9](https://github.com/skill215/sctp/issues/9) | P2, stale header without metadata | Initialize the complete returned frame header; zero it when metadata is absent and preserve the original notification payload. |
| [#10](https://github.com/skill215/sctp/issues/10) | P2, missing remote argument | High-level Dial returns an address error before socket creation/Control instead of panicking. |
| [#11](https://github.com/skill215/sctp/issues/11) | P2, incomplete multi-address input | Reject empty hosts in multi-address input without discarding explicit prefixes; retain valid single-segment wildcards. |
| [#12](https://github.com/skill215/sctp/issues/12) | P1, invalid wrapper counts | Eliminate negative counts and fictitious headers, retain valid partial data with errors, and prevent io.ReadAll panics. |
| [#14](https://github.com/skill215/sctp/issues/14) | P2, address-query capacity | Grow the address area from 4 KiB on ENOMEM up to an explicit 1 MiB budget. Decode complete lists only after success and retain local/peer length ABI differences. |
| [#15](https://github.com/skill215/sctp/issues/15) | P2, cross-platform API | Export ErrUnsupported in shared code and add SyscallConn to the unsupported Listener. |
| [#16](https://github.com/skill215/sctp/issues/16) | P2, unsupported fd cleanup | Immediately release an owned fd when construction fails, using the platform's socket-close primitive. Cover fd 0 and handle width; never close a reused number again. |
| [#13](https://github.com/skill215/sctp/issues/13) | P3, non-blocking improvement | Accept now returns a nil interface on error; retain its P3 non-blocking classification. |

#7, #10, and #11 concern validation and robustness for invalid/incomplete inputs. #15 and #16 are limited to portability and unsupported-platform resource lifecycles; they are not general failures of normal Linux I/O.

## Implementation sequence

| Batch | Content | Validation focus |
| --- | --- | --- |
| A | #4/#12, #9, #8 | Read counts, wrapper frame integrity, handler delivery, initialization failure |
| B | #1 | Prioritize the regression; single-association criteria, PeelOff, one-to-many endpoints, buffered data, timeouts, Close |
| C | #5/#6 | True zero payloads, default send controls, EOF/ABORT, ABI |
| D | #2/#7/#10/#11, #14 | Input snapshots, validation before side effects, complete address queries |
| E | #15/#16, optional #13 | External consumer compilation, Unix/Windows release and reuse |
| F | Integration review and release preparation | All blocking regressions, platform gates, canonical version tag |

For each batch, first record why formal regressions fail on the baseline, then implement the fix. Historical probe assertions expecting unfixed behavior cannot directly become post-fix acceptance criteria.

## Key design gates

- #1 must not use Getpeername as a liveness test: probes confirmed that cached addresses may survive association termination. SCTP_STATUS EINVAL establishes absence only with a valid ABI and known single-association scope; other errors must not become EOF.
- #5 must not silently turn all empty writes into no-ops. The kernel determines whether default controls apply; the tested kernel returns EINVAL for an empty default ABORT, so not every default control is guaranteed to work. A real zero-payload syscall retains the kernel's current parameters and error, avoiding byte injection and races from userspace default-parameter query/replay.
- #14's 1 MiB is an explicit resource budget. Linux ENOMEM can mean insufficient buffer capacity or an actual allocation failure. Stop at the budget, retain errno, and return no partial list.
- #16 must not close Winsock handles as ordinary files, truncate them to int32 first, or blindly retry close/EINTR.
- Handle raw endpoints with unknown models conservatively. Preserve provenance from this library's Dial/Accept/PeelOff. Confirm SO_PROTOCOL=132 for imported fds; after a status query finds no association, receive once more to avoid losing newly arrived data or EOF during normal shutdown races.

## Release acceptance

- [x] All 14 blockers have formal regressions, baseline failure evidence, post-fix pass evidence, and normal/raw-call controls.
- [x] Go1.26.x and 1.27.x each complete ten full race/shuffle rounds, each in an independent process with timeout=120s; record patch version, seed, and kernel.
- [x] #1 passes in at least two actual native Linux kernel environments; cross-compilation does not replace protocol execution.
- [x] Both Go versions pass vet, strict checkptr, low GOGC, and four fuzz smoke categories.
- [x] ARM, ARM64, MIPS, MIPS64, PPC64, and s390x cross-compilation and QEMU non-SCTP ABI/encoding checks pass, including the new zero-iov send path.
- [x] Linux/386 actually executes unsupported and real SCTP fd cleanup tests; native Windows/amd64 executes stub and Winsock resource tests.
- [x] External consumers compile for Linux/amd64, Linux/386, Darwin/amd64, FreeBSD/amd64, and Windows/amd64 under both Go versions.
- [x] Numeric interface-name/index conflicts execute for three rounds in an isolated namespace; required CI jobs must not substitute skips.
- [x] README examples compile and native echo validates payload, stream, and PPID; documentation records corrected empty-operation semantics, frame headers, resource budgets, and limitations.
- [x] Independent review finds no new confirmed blockers introduced by these fixes; #13 status is recorded accurately.
- [x] The release workflow supports v0.*, preserves old tags, makes publish depend on all required jobs, and validates annotated tags.
- [x] The publication stage uses canonical tag v0.7.2. Independently validate the candidate first; after publication, verify that go get @v0.7.2 resolves to the canonical version and tag/release commits agree.

Batch implementation and review evidence: [implementation validation record](../openspec/changes/fix-v0-7-2-stability/evidence/implementation.md). Acceptance status follows completed evidence. Historical checks: [verification audit](issue-verification-2026-10-30.md), [0.3 provenance](repro/0.3-history-results.json), and GitHub #3.

## Subsequent feature release

Design v0.8.0 separately for ReadMsg/ReadMsgExt, standard deadlines and DialContext, typed notifications, common socket configuration, benchmarks, and broader cross-kernel interoperability validation. See the [comparison with other Go SCTP libraries](comparison.md).

## Publication result (2026-10-31)

- [PR #17](https://github.com/skill215/sctp/pull/17) was merged. Canonical annotated tag `v0.7.2` points to `5359b448f9a2dbc21d779e485b92c5751f9f2237`; all previous tag references remain unchanged.
- All 14 [candidate CI](https://github.com/skill215/sctp/actions/runs/41163857034) jobs and the [release workflow](https://github.com/skill215/sctp/actions/runs/41164226789) passed.
- Linux 6.8 / 7.0 × Go 1.26.10 / 1.27.3 each completed ten independent-process race/shuffle rounds, plus vet, checkptr, low GOGC, four fuzz categories, three real namespace rounds, five-target consumers, and README examples. Windows, Linux/386, and six-architecture QEMU checks also have execution evidence.
- Independent-module go get @v0.7.2 and @latest both resolved to v0.7.2. Public APIs and real SCTP payload/stream/PPID passed using the downloaded version.
- English evidence was added to all 14 defect issues, #13, and tracker #3, which were then closed. OpenSpec is 50/50 complete.
- Git's default tag-annotation cleanup removed issue-reference lines beginning with #1; the Release body is complete. Published tags remain immutable. Future workflows use --cleanup=verbatim, verified in an isolated repository.
