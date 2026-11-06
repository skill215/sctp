# SCTP review: five additional rounds

Date: 2026-11-04. Baseline: v0.7.3,
`e4154064dc9928274bcf4615c5382dd6fd81ad21`. Only this SCTP repository was
reviewed and tested. Implementation and permanent tests were not changed.

## Result

No additional reproducible bug was confirmed in this batch. Existing findings
[#19](https://github.com/skill215/sctp/issues/19),
[#20](https://github.com/skill215/sctp/issues/20),
[#21](https://github.com/skill215/sctp/issues/21) and
[#22](https://github.com/skill215/sctp/issues/22) were excluded from the new-bug
count and were not fixed by this review. No new GitHub issue was created and
no release was performed.

## Completed rounds

Each round includes a fresh full `-race -shuffle` run on both Go 1.26.10 and
1.27.3; seeds are 7273901 through 7273905. The final evidence for rounds 4–5
comes from serial verification with distinct log names.

| Round | Review and tests | Final result |
| --- | --- | --- |
| 1 | Descriptor ownership, constructor failures, close, raw callbacks, FileListener and peeloff; focused tests count=2 per version | Pass |
| 2 | Read/Write/Accept/connect timeout budgets, notification loops, queued I/O and shutdown/reset; focused tests count=2 per version | Pass |
| 3 | Address validation, mixed families, multihoming, IPv6 zones, socket-option UAPI types and lengths; focused tests count=2 per version | Pass |
| 4 | Streams, PPID, send controls, wrapped metadata, ancillary and codec parsers; focused tests count=2 and four fuzz targets per version | Pass after separate investigation of an initial fuzz deadline termination |
| 5 | Full suite, vet, strict checkptr, GOGC=1 race, Linux/stub boundaries and CLI review | Pass |

The final main record contains 32 passing command groups: ten full race/shuffle
runs, eight focused commands, eight fuzz sessions, two vet checks, two strict
checkptr runs and two low-GOGC race runs. The final fuzz commands use ten-second
budgets and `-fuzzminimizetime=1s`. Commands and outputs are retained in
[checks.json](repro/five-round-v0.7.3-2026-11-04-batch2/checks.json).

## Investigated candidates

**Extended event subscriptions survive wrapping.** The ten-byte EventSubscribe
Go type initially suggested that adding DATA_IO might erase newer Linux event
fields. Native tests separately enabled each of kernel event bytes 10–13,
wrapped the same connection twice and read all 14 bytes back. Each extended
subscription remained enabled and DATA_IO was added. This candidate is excluded
as a false positive on the tested kernel.

**Timeout reconfiguration has no observed stale state.** For Read, SCTPRead and
wrapped Read, probes expired a 40 ms timeout, changed it to 120 ms, then cleared
it. A new read waited 200 ms without timing out and subsequently received peer
data. Accept likewise expired a timeout, cleared it, then waited for and accepted
a delayed connection. These probes and the event probes passed under `-race`
with `-count=3` on each Go version. See
[probe source](repro/five-round-v0.7.3-2026-11-04-batch2/extra_probes_test.go.txt)
and [outcomes](repro/five-round-v0.7.3-2026-11-04-batch2/extra-probes.json).

**One initial fuzz deadline termination remains unclassified.** The first
Go 1.26 RawSockaddr fuzz session terminated after about 11 seconds with
`context deadline exceeded`, without a panic, failing assertion or saved crash
input. This failure remains recorded in
[initial-fuzz-timeout.json](repro/five-round-v0.7.3-2026-11-04-batch2/initial-fuzz-timeout.json)
and its linked log. It is not included among the passing final commands.
Three independent 20-second retries on each Go version passed. Their execution
counters paused after the first few seconds. Two further 30-second sessions
with a one-second minimization budget passed with sustained execution. Shorter
minimization restoring throughput supports a minimization-related explanation
for the paused counters, but does not establish the original termination's root
cause. The independent final round 4 also passed. There is insufficient evidence
to classify this as a reproducible library bug or create an issue for it.
See [six default-budget retries](repro/five-round-v0.7.3-2026-11-04-batch2/raw-repeats.json)
and [two shorter-minimization runs](repro/five-round-v0.7.3-2026-11-04-batch2/raw-minimize.json).

## Limits and retained evidence

This batch used one native Linux/amd64 kernel,
`6.6.114.1-microsoft-standard-WSL2`. It does not add another native kernel,
QEMU, cross-architecture runtime coverage or business-repository validation.
Documented limits such as standard deadline/DialContext support and message
boundary flags were not reclassified as bugs. Passing tests do not establish
that the release is bug-free and do not resolve the four previously recorded
issues.

There are ten additional recorded passing command groups: six default-budget
fuzz retries, two shorter-minimization fuzz runs and two probe commands, each
probe command repeating its tests three times. The initial failure is retained
separately. Results and redacted evidence are indexed by
[results.json](repro/five-round-v0.7.3-2026-11-04-batch2/results.json),
[reproduction instructions](repro/five-round-v0.7.3-2026-11-04-batch2/README.md)
and [manifest](repro/five-round-v0.7.3-2026-11-04-batch2/log-manifest.json).
