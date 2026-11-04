# SCTP library review and validation

Date: 2026-11-03. Scope: this repository only, following
[the library boundary](library-scope.md). Baseline:
`2f7bfbc37ba6333405116b60910eb6257ebfa81a`. No library or example implementation
was changed, and no release was published.

## Confirmed finding

**P2: the example client exceeds the negotiated stream limit.**
In `example/sctp.go`, the send loop assigns both Stream and PPID from an
ever-increasing counter. PPID and the association's outbound stream range are
independent. With a valid peer advertising two inbound streams, sends on streams
0 and 1 succeed; the next send selects stream 2, returns EINVAL and terminates
the client through log.Fatalf.

Two runs of the unmodified, compiled example against an independent SCTP echo
peer reproduced this outcome. Both completed exactly two writes and exited
with status 1 on the third, approximately two seconds after connection setup.
The peer records negotiation of two streams and receipt on streams 0 and 1.

An independent control sends PPIDs 0 through 5 while selecting streams modulo
the negotiated Ostreams count. All messages arrive with correct payload,
stream and PPID. An explicit stream-2 send returns EINVAL, and a subsequent
stream-0 send still succeeds. This control passed three race-enabled runs.
The core library correctly rejects the invalid stream; the defect is the
example's selection policy. A fix should use a valid fixed stream or wrap the
stream counter within GetStatus().Ostreams while keeping PPID independent.

Reproduction evidence:

- [First result](repro/library-review-2026-11-03/example-result-1.json) and
  [second result](repro/library-review-2026-11-03/example-result.json).
- [Client log](repro/library-review-2026-11-03/example-client.log) and
  [independent peer log](repro/library-review-2026-11-03/example-server.log).
- [Two-stream peer](repro/library-review-2026-11-03/limited-peer.go.txt) and
  [process harness](repro/library-review-2026-11-03/example-probe.py.txt).
- [Library control](repro/library-review-2026-11-03/stream-boundary_test.go.txt)
  and [three passing runs](repro/library-review-2026-11-03/stream-control.log).

This review confirms one example defect and no new core-library defect.
The example remains unchanged for a separate fix and regression review.

## Code review coverage

The review inspected socket creation and failed setup, imported descriptors,
FileListener duplication, accepted and peeled sockets, close-on-exec, concurrent
Close and raw callbacks, read/write gates, queued waiters, close wakeups, timeout
snapshot/clearing, connect status/error handling, notification callbacks,
consumed resets, one-to-many associations, event subscription ownership,
wrapped I/O counts, sockaddr validation, IPv6 zones, address-query growth,
PPID byte order, ancillary decoding and socket-option ABI.

The documented lack of standard deadlines and DialContext is an existing API
limit. Unsupported platforms and Linux/386 do not become supported through
stub compilation checks. No application repositories were read or modified.

## Local validation

Native Linux/amd64 kernel: `6.6.114.1-microsoft-standard-WSL2`.
Go versions: 1.26.10 and 1.27.3.

- Ten independent full race/shuffle processes per version, seeds 7273501
  through 7273510. Each run passed 99 top-level tests, with no test skips.
- Explicit vet, full strict checkptr and a full GOGC=1 race run per version.
- RawSockaddr, Ancillary, Codec and AddressRoundTrip fuzz targets, ten seconds
  each per version. No new crashing corpus input was produced.
- Three required numeric-interface/name-index namespace regressions passed.
- Independent public-API fixtures compiled on five existing compatibility
  targets. This synthetic fixture imports this module and standard-library
  packages only.
- README examples built and verified actual payload, stream, PPID and the
  timeout example. These README programs are separate from the CLI example
  containing the finding above.

Exact [34 main checks](repro/library-review-2026-11-03/checks.json),
[per-run test counts](repro/library-review-2026-11-03/local-test-counts.json),
[source hashes](repro/library-review-2026-11-03/source-hashes.json),
[required namespace log](repro/library-review-2026-11-03/required-netns.log),
[API fixture builds](repro/library-review-2026-11-03/independent-api-builds.log)
and [README log](repro/library-review-2026-11-03/readme.log) are retained.
Large raw logs are archived outside the source tree. Committed test-count and
command summaries retain their outcomes; original hashes and archive filenames
are recorded in the [log manifest](repro/library-review-2026-11-03/log-manifest.json).

## Fresh remote native and architecture checks

[Run 41595802687](https://github.com/skill215/sctp/actions/runs/41595802687)
passed all ten jobs. The isolated branch
`codex/sctp-library-review-2026-11-03` adds only a review workflow to the baseline
commit; it does not modify library source or trigger publication.

| Native kernel | Go | Full race/shuffle processes |
| --- | --- | --- |
| 6.8.0-1070-azure | 1.26.10 | 10 |
| 6.8.0-1070-azure | 1.27.3 | 10 |
| 7.0.0-1017-azure | 1.26.10 | 10 |
| 7.0.0-1017-azure | 1.27.3 | 10 |

Each native job also passed vet, strict checkptr, low-GOGC, all four fuzz
targets, three required privileged namespace regressions, independent API
fixtures and README runtime checks. Six additional Go 1.26 architecture jobs
passed the existing ABI/alignment/encoding regressions under QEMU: ARM, ARM64,
MIPS, MIPS64, PPC64 and s390x. These are not full SCTP association tests on
the emulated architectures.

The [remote evidence](repro/library-review-2026-11-03/native-ci.json) records
actual kernels/toolchains, job steps, test names and outcomes. Passing suites
do not establish that every possible behavior is bug-free; the separately
reproduced CLI example defect is still open.

Follow-up on 2026-11-04: the example fix and its before/after subprocess
regressions are recorded in [the v0.7.3 validation](release-validation-v0.7.3.md).

Published diagnostic records redact local absolute paths and internal IP
addresses. Original unredacted records remain outside the source tree.
