# v0.7.3 review and reproduced findings

Date: 2026-11-04. Baseline: v0.7.3,
`e4154064dc9928274bcf4615c5382dd6fd81ad21`. Scope: the SCTP library and its
own example. This review changes no implementation and publishes no release.

## P2: FileListener races with source-file Close

`sctp_linux.go:400` calls File.Fd and then F_DUPFD_CLOEXEC without holding a
reference to the source file's descriptor. A caller still owns that original
file. Concurrent Close can write internal/poll.FD.Sysfd while FileListener's
File.Fd reads it. runtime.KeepAlive prevents finalization, but does not prevent
an explicit concurrent Close.

The reproduction creates a valid bound/listening SCTP socket, wraps it in an
os.File and races FileListener against closing that file and allocating a
replacement file. Each Go version, 1.26.10 and 1.27.3, reports a data race in
three separate processes. The stacks identify FileListener -> os.File.Fd on
one side and os.File.Close -> internal/poll.FD.destroy on the other.

An independent control obtains the source's SyscallConn and duplicates inside
RawConn.Control. All six control processes pass, with 30,000 attempts each.
The original processes allow up to 30,000 attempts each. The initial review
observed the race without an actual wrong-fd import. The issue-recording recheck
ran six new independent processes: all reported races, and two additionally
returned a non-socket descriptor (Go 1.26 attempt 14591; Go 1.27 attempt 24840).
Fstat succeeded in those cases. Six fresh pinned controls passed. These newer
observations confirm wrong-resource import; they do not establish remote
exploitability or arbitrary data corruption. See
[GitHub #19](https://github.com/skill215/sctp/issues/19).

The fix should duplicate while the original file's RawConn.Control pins the
descriptor, retaining CLOEXEC and closing the duplicate on initialization
failure. The relevant implementation is also present in v0.7.2; this is not
introduced by v0.7.3's example change.

Evidence: [probe source](repro/v0.7.3-review-2026-11-04/filelistener_race_test.go.txt),
[independent-process results](repro/v0.7.3-review-2026-11-04/fd-probes.json),
[Go 1.26 race](repro/v0.7.3-review-2026-11-04/fd-1.26.10-FileListenerCloseReuse-1.log),
[Go 1.27 race](repro/v0.7.3-review-2026-11-04/fd-1.27.3-FileListenerCloseReuse-1.log).

## P2: invalid example bind addresses silently widen the listener

`example/sctp.go:51` logs address-resolution errors and continues. If every
configured address is invalid, the empty IPAddrs slice reaches ListenSCTP,
where it selects wildcard listening. A mistyped explicit binding therefore
starts a server rather than failing configuration.

The unmodified example was run as `-server -ip '%' -port 0`. In all three
runs, it logged the resolution error, created a listener whose address query
contained five local addresses, and accepted a separately created loopback
SCTP connection. The reproduction owns and terminates every child process.
The source path predates the v0.7.3 stream fix. Three fresh recording rechecks
confirm the same result. See [GitHub #20](https://github.com/skill215/sctp/issues/20).

The example should fail when an explicitly supplied address cannot resolve,
rather than ignoring it and partially or entirely changing the configured
bind list. This is a shipped-example defect, not a new kernel I/O defect.

Evidence: [harness](repro/v0.7.3-review-2026-11-04/invalid-bind.py.txt),
[three results](repro/v0.7.3-review-2026-11-04/invalid-bind.json),
[first log](repro/v0.7.3-review-2026-11-04/invalid-bind-1.log).

## Baseline testing and limits

Fresh local validation used Linux/amd64 kernel
`6.6.114.1-microsoft-standard-WSL2`. On each of Go 1.26.10 and 1.27.3:

- Ten independent full race/shuffle processes, seeds 7273701 through 7273710.
- Full vet, strict checkptr and a GOGC=1 full race run.
- Four fuzz targets, ten seconds per target: RawSockaddr, Ancillary, Codec and
  AddressRoundTrip.

All [34 baseline checks](repro/v0.7.3-review-2026-11-04/checks.json) pass.
These passing suites lack the newly identified cases; they do not invalidate
the additional failing reproductions. This round did not repeat remote native
or QEMU jobs. The already-passing v0.7.3 release matrix is historical evidence
for the same source, not a new test result from this review.

The source review also covered the v0.7.3 stream change and its subprocess
cleanup, timeout gates, close wakeups, raw callbacks, accepted and peeled
descriptors, notification processing, wrapper counts, address validation and
ancillary encoding. No further confirmed finding was produced in those paths.
Standard deadlines, DialContext and out-of-scope platform gaps remain documented
API/support limits, not newly classified defects.

Reproductions are retained as text/overlay diagnostics for a later fix; no
failing permanent test was installed. Logs redact local machine paths and
internal IPs, with hashes in the
[manifest](repro/v0.7.3-review-2026-11-04/log-manifest.json).
The [result summary](repro/v0.7.3-review-2026-11-04/results.json) distinguishes
baseline passes, confirmed failures and the stronger fd-reuse observation from
the issue-recording recheck. Initial fd probe results/logs are preserved
separately from the recheck.
