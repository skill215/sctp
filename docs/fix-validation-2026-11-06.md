# SCTP fix validation record

Date: 2026-11-06. Baseline: v0.7.3 / e4154064. The candidate is a local, unpublished working tree; scope is limited to the SCTP repository. Earlier review documents and evidence are retained.

## Fixes and regressions

| Issue | Fix | Validation focus |
| --- | --- | --- |
| [#19](https://github.com/skill215/sctp/issues/19) | FileListener duplicates the fd inside the source file's RawConn.Control | The pre-fix probe triggered the race detector; after the fix, concurrent Close/reuse either imports correctly or fails; source and duplicate close independently |
| [#20](https://github.com/skill215/sctp/issues/20) | Exit immediately on CLI address parsing failure | All-invalid and mixed valid/invalid inputs exit before listening, verified with actual subprocesses |
| [#21](https://github.com/skill215/sctp/issues/21) | Validate all four SetInitMsg integers before converting to uint16 | Negative and excessive values in every field return EINVAL without changing kernel readbacks; preserve 0 and 65535 |
| [#22](https://github.com/skill215/sctp/issues/22) | Separate example configuration variables from actual buffer readbacks | Three consecutive clients preserve both explicit and unset configurations; explicit settings have an independent socket control |
| [#23](https://github.com/skill215/sctp/issues/23) | Address queries and PeelOff reject association IDs not representable as signed 32-bit values | id±2^32 and int64 extremes cannot select another association; a valid ID still permits PeelOff and payload transfer after rejection; architecture unit tests confirm invalid queries never invoke the kernel callback |
| [#24](https://github.com/skill215/sctp/issues/24) | Share nonblocking one-to-many test setup and confirm readiness using real payload/metadata | Discovered under low GC and confirmed by SIGURG and independent syscalls; add setup/PeelOff/payload regressions with signals |

Parameter validation preserves ErrUnsupported precedence in unsupported stubs, with dedicated regressions. Public signatures, raw SCTPConnect errno behavior, and the contract that standard deadlines are unsupported remain unchanged.

## Final validation

- Go 1.26.10 and 1.27.3 each passed five independent full-library race/shuffle processes with seeds 72611061–72611065. Targeted regressions also ran with count=3 on each version.
- Both versions passed vet, strict checkptr, GOGC=1 full-library race, and RawSockaddr, Ancillary, Codec, and AddressRoundTrip fuzzing. Each fuzz target had a 10-second budget and a 1-second minimization budget. The main record contains 26 passed command groups.
- The final test sources cross-compiled for ARM, ARM64, MIPS, MIPS64, PPC64, and s390x using both versions: 12 compilation command groups. QEMU was not run locally.
- After the core fixes, both versions passed native Linux/386 stub tests, three mandatory namespace interface-name conflict runs, independent API consumer compilation, and README example execution: eight command groups. Later changes affected only Linux test fixtures, not the library code covered by these checks.
- The Solaris library built with Go 1.26; the known out-of-scope test-compilation gap remains deferred and unchanged.

Local testing used one Linux/amd64 kernel. These results do not establish a pass on a second native kernel, native Windows, QEMU, or the complete remote release matrix. Five review rounds do not replace the release requirement of ten rounds per native job.

## Retained failures and causes

An initial Go 1.26 low-GC check after the fixes returned EISCONN from the old blocking connection setup in TestPeelOffCloseOnExec. That failure was not overwritten as a success. An independent 100-run control without explicit signals passed. A subsequent SIGURG probe ran both the wrapper and direct CONNECTX3 1000 times each, reproducing EISCONN in both, while native peer queries and Accept proved that the association had been established.

This led to issue #24 and a fix to the fixture assumption, rather than changing the raw syscall interface to swallow arbitrary EISCONN. After unifying the fixture, three targeted GOGC=1/race repetitions and the final complete two-version validation passed. The final review confirmed no other independent bug.

## Evidence and delivery status

[Final commands](repro/fixes-2026-11-06/checks.json),
[architecture compilation](repro/fixes-2026-11-06/architecture.json),
[compatibility checks](repro/fixes-2026-11-06/compatibility.json),
[result summary](repro/fixes-2026-11-06/results.json),
[before/after evidence](repro/fixes-2026-11-06/README.md),
[source fingerprints](repro/fixes-2026-11-06/source-manifest.json).

Issues #23 and #24 were created in English and read back for verification. All issues remain open so that local fixes are not presented as merged or released. Code, regressions, and documentation are in the working tree; they have not been committed, pushed, or released. Architecture conclusions and next-stage organization/error-model recommendations are in the [architecture review](architecture-review-2026-11-06.md).
