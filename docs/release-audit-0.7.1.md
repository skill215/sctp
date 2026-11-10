# SCTP 0.7.1 release audit

Audit date: 2026-10-30.

**Conclusion: 0.7.1 has two reproduced defects and cannot be considered free of known bugs.** Existing tests pass, but additional abnormal-disconnect and shared-address probes fail. The next version should prioritize these two release blockers; see the [v0.7.2 goals](release-plan-v0.7.2.md).

This report records the first audit. Subsequent review confirmed eight more defect categories, registered as #4–#11. The complete blocker list and reproduction entry points are in the [v0.7.2 goals](release-plan-v0.7.2.md) and [GitHub release plan #3](https://github.com/skill215/sctp/issues/3).

## Audit baseline

- Formal release: [0.7.1](https://github.com/skill215/sctp/releases/tag/0.7.1).
- Tagged commit: [e864ee0](https://github.com/skill215/sctp/commit/e864ee06b6c5b0c77661c4715814a75576c23237).
- Tests ran in an independent archive of the tag; master `bd2fa27` has identical Go sources and go.mod.
- Environment: Linux/amd64, kernel `6.6.114.1-microsoft-standard-WSL2`, Go 1.26.10 and Go 1.27.2.

Results cover this environment and test scope; they do not guarantee freedom from defects for every input, kernel, or deployment.

## Validation results

| Check | Result | Scope |
| --- | --- | --- |
| Existing full suite, race, shuffled order, ten rounds | Passed on both Go versions | Go 1.26.10: 71.618s; Go 1.27.2: 65.070s |
| go vet | Passed on both versions | Current release sources |
| Strict checkptr and low GOGC | Passed, 10.244s | Go 1.26.10; three rounds of key I/O, notification, lifecycle, ABI, and codec tests |
| Interface-name/numeric-index conflict | Three actual runs passed | Go 1.26.10; SCTP_TEST_REQUIRE_NETNS=1, skips forbidden |
| Test-binary cross-compilation for six architectures | All passed | ARM, ARM64, MIPS, MIPS64, PPC64, s390x; Go 1.26.10; CGO disabled |
| GitHub Actions at publication | All successful; records rechecked | Go 1.26/1.27, six QEMU architecture checks, and publish; earlier publication evidence |
| New peer-ABORT probes | Failed on both versions | Read, SCTPRead, and wrapped Read, three runs each |
| New shared-wildcard-address probes | Failed on both versions | Input mutation and data race, three runs each |

Earlier publication evidence is in the [release workflow](https://github.com/skill215/sctp/actions/runs/40845986390). This audit reran cross-compilation only, not QEMU. QEMU ABI/encoding checks do not replace native architecture testing of complete SCTP associations.

Existing test commands:

```sh
go test -race -shuffle=on -count=10 -timeout=180s ./...
go vet ./...
```

Strict pointer and low-GOGC checks:

```sh
GOGC=20 go test -race -gcflags=all=-d=checkptr=2 \
  -run '^Test(NativeCodec.*|ParseSndRcvInfo.*|.*SocketTimeout.*|.*Notification.*|.*RawConn.*|.*PeelOff.*|.*Wrapped.*|.*NumericIPv6.*|.*Mixed.*|.*FD.*)$' \
  -count=3 -timeout=120s ./...
```

## P1: reads keep waiting after peer ABORT

Tracking issue: [#1](https://github.com/skill215/sctp/issues/1), linked to [v0.7.2 release plan #3](https://github.com/skill215/sctp/issues/3).

### Trigger and result

1. Establish an SCTP client/server association.
2. The server sets `SO_LINGER={Onoff:1,Linger:0}` and closes, sending ABORT.
3. One client Write consumes `ECONNRESET`.
4. The client calls Read, SCTPRead, or wrapped Read without a receive timeout.

The association is terminated, so reading should promptly return EOF or an identifiable terminal error. All three entry points remained blocked during the 250ms observation window and returned only after explicit Close. This path has no configured timeout and may wait indefinitely in the source implementation.

Representative output:

```text
TestReleaseAuditReadAfterPeerAbort/Read: read blocked after Write consumed ECONNRESET
TestReleaseAuditReadAfterPeerAbort/SCTPRead: read blocked after Write consumed ECONNRESET
TestReleaseAuditReadAfterPeerAbort/wrapped_Read: read blocked after Write consumed ECONNRESET
```

### Cause and required fix

After Recvmsg returns EAGAIN, SCTPRead waits on the runtime poller without checking whether the SCTP association still exists. Another I/O operation can consume Linux's reset error, so a subsequent read need not receive ECONNRESET again. [Release read implementation](https://github.com/skill215/sctp/blob/e864ee06b6c5b0c77661c4715814a75576c23237/sctp_linux.go#L181-L231)

The fix must distinguish an active but idle association from a terminated one while preserving buffered data, notifications, the timeout budget, and Close wakeups. A temporary caller workaround is to close promptly after reset or another association-termination error; receive timeouts only bound the wait.

## P2: concurrent listeners mutate a shared wildcard address

Tracking issue: [#2](https://github.com/skill215/sctp/issues/2), linked to [v0.7.2 release plan #3](https://github.com/skill215/sctp/issues/3).

### Trigger and result

Multiple goroutines share `addr := &SCTPAddr{Port: 0}` when calling `ListenSCTP("sctp4", addr)`.

The calls change the caller's initially empty IPAddrs into an IPv4 wildcard address. Concurrent reads/writes of this slice trigger the race detector between listenSCTPExtConfig and ToRawSockAddrBuf.

```text
WARNING: DATA RACE
ListenSCTP mutated caller's wildcard address: 0.0.0.0:0
```

### Cause and required fix

The listener appends directly to laddr.IPAddrs to supply a wildcard address. Dialing has a similar in-place mutation path; this audit's dynamic reproduction covers listening. [Listen path](https://github.com/skill215/sctp/blob/e864ee06b6c5b0c77661c4715814a75576c23237/sctp_linux.go#L323-L334), [dial path](https://github.com/skill215/sctp/blob/e864ee06b6c5b0c77661c4715814a75576c23237/sctp_linux.go#L479-L490)

Prepare internal address copies without mutating caller structures, slices, or shared backing storage. Regressions should cover Listen and Dial, IPv4 and IPv6, and success/failure paths. A temporary caller workaround is to construct a separate address for every call instead of sharing a wildcard SCTPAddr.

## Reproducing the additional defects

The [reproduction source](repro/0.7.1-regressions_test.go.txt) uses the release's existing lifecyclePair helper. It is stored as .txt for explicit copying into a release archive, rather than included in the normal master Go test package. The fix should move the corresponding assertions into permanent regressions.

Run from the repository root:

```sh
sctp_audit_dir="$(mktemp -d)"
git archive 0.7.1 | tar -x -C "$sctp_audit_dir"
cp docs/repro/0.7.1-regressions_test.go.txt "$sctp_audit_dir/release_audit_linux_test.go"
(
  cd "$sctp_audit_dir"
  go test -race -run '^TestReleaseAudit' -v -count=3 -timeout=30s .
)
```

Linux kernel SCTP support and local socket permissions are required. The expected exit code on unfixed 0.7.1 is nonzero. These tests confirmed that the client had consumed ECONNRESET; failures were not caused by an environment unable to establish an association.
