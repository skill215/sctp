# Five-round SCTP review

Date: 2026-11-04. Baseline: v0.7.3,
`e4154064dc9928274bcf4615c5382dd6fd81ad21`. This work covers only the SCTP
repository. The implementation and permanent tests were not changed.

## Completed rounds

Each round ran a fresh full race/shuffle process on Go 1.26.10 and 1.27.3.
Seeds were 7273801 through 7273805. Round 1's Go 1.26 full run completed before
the result harness started; its recorded duration is unavailable, not zero.

| Round | Review and focused testing | Result |
| --- | --- | --- |
| 1 | Construction, descriptor ownership, raw callbacks, close, peeloff and failed setup; focused tests repeated twice per Go version | Baseline pass |
| 2 | Read/write/accept/connect timeouts, queued operations, reset and notification handling; focused tests repeated twice per version | Baseline pass |
| 3 | Addresses, multihoming, IPv6 zones, input ranges and socket-option metadata; focused tests repeated twice per version | Baseline pass; new InitMsg range defect confirmed separately |
| 4 | Streams, send flags, PPID, wrapped I/O, ancillary and codec parsing; focused tests plus four ten-second fuzz targets per version | Baseline pass |
| 5 | Final full suite, vet, strict checkptr, GOGC=1 race, CLI lifecycle/configuration review | Baseline pass; new example buffer defect confirmed separately |

The main harness completed 32 successful command groups: ten full race/shuffle
processes, eight focused commands (each count=2), eight fuzz sessions, two vet,
two strict checkptr and two low-GOGC full race runs.

Additional checks passed three **required** numeric-interface namespace tests
per Go version and six architecture test compilations per version: ARM, ARM64,
MIPS, MIPS64, PPC64 and s390x. These were compilation checks, not fresh QEMU or
native SCTP association tests on those architectures. This round used one native
Linux/amd64 kernel: `6.6.114.1-microsoft-standard-WSL2`.

## New P2: SetInitMsg silently changes out-of-range values

[GitHub #21](https://github.com/skill215/sctp/issues/21).
`sctp.go:538` narrows signed int arguments to uint16 without checking range.
The probe resets valid kernel fields to (4,5,6,7) before every bad request and
reads SCTP_INITMSG back through getsockopt:

| Input in all fields | Error | Actual kernel fields |
| --- | --- | --- |
| -1 | nil | 65535,65535,65535,65535 |
| 65536 | nil | 4,5,6,7, because narrowing to zero retains previous values |
| 65537 | nil | 1,1,1,1 |

The valid (4,5,6,7) control succeeds and reads back exactly. Three independent
processes per Go version reproduce the invalid-input assertions. This is a
parameter-validation defect, not failure of valid network traffic or a claimed
remote exploit. Reject each out-of-range argument before narrowing or changing
the socket, while preserving valid zero/default semantics and public signatures.

Evidence: [probe](repro/five-round-v0.7.3-2026-11-04/new_probes_test.go.txt),
[six reproduction outcomes](repro/five-round-v0.7.3-2026-11-04/supplement.json),
[kernel readback log](repro/five-round-v0.7.3-2026-11-04/init-ranges-1.26.10-1.log).

## New P2: example buffer configuration grows across clients

[GitHub #22](https://github.com/skill215/sctp/issues/22).
`example/sctp.go:87` and `:91` store actual GetWriteBuffer/GetReadBuffer results
back into the sndbuf/rcvbuf flag variables. The next accepted connection uses
those results as new requested settings.

With unchanged `-sndbuf 4096 -rcvbuf 4096`, three sequential connections receive
8192/8192, then 16384/16384, then 32768/32768. All six fresh server processes
(three per Go version) reproduce this. Independent raw socket controls applying
4096 on each new socket consistently return 8192/8192. Kernel normalization is
working; feeding its result back into the next configuration is the defect.
Growth is subject to kernel limits; unbounded allocation was not established.

Use separate variables for requested flags and actual readbacks. Preserve unset
flags and add a multi-client regression. This is a shipped-example defect, not
a demonstrated defect in the public library buffer setters.

Evidence: [process probe](repro/five-round-v0.7.3-2026-11-04/buffer-probe.py.txt),
[six results and raw controls](repro/five-round-v0.7.3-2026-11-04/buffer-results.json).

## Known findings and interpretation

#19's FileListener/Close race and #20's invalid CLI address fallthrough remain
separate existing findings; they are not counted as new reports. No other
confirmed finding was produced in these five rounds. Documented API limits,
such as unsupported standard deadlines/DialContext, and deferred unsupported
platform gaps are not reclassified as new defects.

Passing the baseline suite does not override the additional failing probes.
The InitMsg reproduction is expected to fail on this version. Probes remain
temporary overlays/text artifacts, rather than installing failing permanent
tests in the repository. Both new issues include portable reproductions,
observed results, limitations and fix acceptance criteria.

Commands and outcomes are retained in
[main checks](repro/five-round-v0.7.3-2026-11-04/checks.json),
[supplemental checks](repro/five-round-v0.7.3-2026-11-04/supplement.json),
[summary](repro/five-round-v0.7.3-2026-11-04/results.json) and
[log manifest](repro/five-round-v0.7.3-2026-11-04/log-manifest.json).
Published-ready evidence redacts local paths and internal IPs. No fix or new
release was performed as part of this review.
