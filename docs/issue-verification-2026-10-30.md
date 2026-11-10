# SCTP issue verification

Review date: 2026-10-30.

**Conclusion: all ten reported behaviors in #1, #2, and #4–#11 were reproduced again; none should be closed as a false-positive test.** Seven are functional or data-correctness defects. Issues #7, #10, and #11 concern validation and robustness for invalid or incomplete input, not inevitable failures of normal connection flows. Issue #3 is release planning and is excluded from the bug count.

All issues remain unfixed. Actual fix progress is tracked in [v0.7.2 release plan #3](https://github.com/skill215/sctp/issues/3).

## Method and scope

- Use an independent source archive of release tag `0.7.1`, commit `e864ee06b6c5b0c77661c4715814a75576c23237`.
- Environment: Linux/amd64, kernel `6.6.114.1-microsoft-standard-WSL2`.
- Run all registered reproductions five times each under Go 1.26.10 and Go 1.27.2 with the race detector.
- Run six control groups three times on each version, checking normal I/O, raw zero-payload sendmsg, whether reset is consumed, address sharing, public notification callbacks, and actual bound addresses.
- Check Go Reader semantics, standard-library address validation, Linux SCTP UAPI, and the socket types to which EOF applies.

The failing cases assert the intended post-fix behavior and continue to fail on unfixed 0.7.1. Controls rule out incorrect attribution; some deliberately describe unfixed behavior and cannot be reused unchanged as post-fix acceptance tests.

## Findings by issue

| Issue | Classification | Reconfirmed facts and controls |
| --- | --- | --- |
| [#1](https://github.com/skill215/sctp/issues/1) | Functional defect, P1 | Read receives the terminal error while reset is unconsumed. After Write consumes reset, Read remains blocked for over one second until Close wakes it. GetStatus was ESTABLISHED before disconnect and EINVAL afterward, ruling out a mere 250ms scheduling delay. |
| [#2](https://github.com/skill215/sctp/issues/2) | Concurrency defect, P2 | Sharing one wildcard address triggers a race and mutates input IPAddrs; separate address objects per call do not race. Dynamic evidence covers Listen; the similar in-place Dial path still needs a fix regression. |
| [#4](https://github.com/skill215/sctp/issues/4) | Data-correctness defect, P1 | Read(nil) returns 1 and loses the first byte; a 32-byte wrapper buffer returns 33. Normal-capacity payload/metadata controls pass. |
| [#5](https://github.com/skill215/sctp/issues/5) | Data-correctness defect, P1 | Empty payload with metadata injects 0x00 through the library; raw zero-payload sendmsg returns EINVAL without injecting bytes, and a subsequent normal send succeeds. |
| [#6](https://github.com/skill215/sctp/issues/6) | UAPI constant defect, P2 | On a one-to-many socket where EOF applies, raw sendmsg rejects 0x10, whereas the correct 0x200 delivers EOF to the peer. The test avoids Go SendmsgN's padding behavior. |
| [#7](https://github.com/skill215/sctp/issues/7) | Parameter validation, P2 | High-level Listen accepts 65536 and binds another port; Dial accepts 65536+p and connects to p. Go ListenTCP rejects invalid ports. |
| [#8](https://github.com/skill215/sctp/issues/8) | Functional defect, P2 | Using public SocketConfig.NotificationHandler, an unwrapped connection receives one SHUTDOWN callback; after wrapping, it receives none and returns EOF directly, proving an effect on actual notification delivery. |
| [#9](https://github.com/skill215/sctp/issues/9) | Data-correctness defect, P2 | A SENDER_DRY notification without metadata leaves stale 0xa5 bytes in the returned header. The normal-payload wrapper control overwrites the header correctly and reports stream=1, PPID=42. |
| [#10](https://github.com/skill215/sctp/issues/10) | Input robustness, P2 | nil raddr panics; Go DialTCP reports missing address for the equivalent input. Automatic binding with valid raddr and nil laddr works. |
| [#11](https://github.com/skill215/sctp/issues/11) | Input validation and semantic risk, P2 | 127.0.0.1/:0 resolves as wildcard and Getsockname reports 0.0.0.0. Valid 127.0.0.1:0 still binds 127.0.0.1, and standalone :0 retains valid wildcard behavior. |

## Trigger scope and record corrections

### Read counts have an explicit interface contract

Go Reader requires the returned count not to exceed the buffer length and specifies a zero count for a zero-length buffer. Issue #4 violates this contract through its count and byte loss; it is not normal behavior caused by the caller supplying an empty buffer. [Go Reader documentation](https://pkg.go.dev/io#Reader)

### Empty messages and shutdown flags need separate verification

Issue #5 does not require the kernel to support empty SCTP user messages. Rejecting an empty message is acceptable; silently inventing a payload byte is the defect.

Issue #6 checks the exported Linux constant, with dynamic verification on one-to-many sockets. It does not mean every SOCK_STREAM send supports SCTP_EOF, or that SCTPConn.Close generally fails to close. RFC 6458 describes zero-data EOF shutdown for one-to-many sockets, and Linux UAPI defines SCTP_EOF as MSG_FIN. [RFC 6458 section 3.3](https://www.rfc-editor.org/rfc/rfc6458.html#section-3.3), [Linux v6.6 UAPI](https://github.com/torvalds/linux/blob/v6.6/include/uapi/linux/sctp.h#L310-L319)

### The three parameter issues are not general I/O failures

- #7 concerns missing port-range validation at high-level networking entry points. Integer truncation in a low-level converter alone would not prove a bug; the evidence is that the actual listen/connect target changes without an error.
- #10 concerns a missing remote address. It does not imply a panic for valid addresses or establish a vulnerability directly triggerable by remote traffic. The Go standard-library control explicitly rejects nil raddr. [Go 1.26.10 DialTCP](https://github.com/golang/go/blob/go1.26.10/src/net/tcpsock.go#L317-L329)
- #11 concerns incomplete multihoming input, not general failure of valid multihoming. Pure parsing cases need neither the kernel SCTP module nor socket permissions; only the additional actual-binding controls require them. Narrow the original issue's generic environment description accordingly.

### Shared root causes were not registered twice

The plain Read and header-only wrapped Read cases in #4 are two entry points for one defect. Both #4 and #5 involve temporary bytes in Go syscall handling, but differ in direction, data loss versus injection, and fix boundaries. Issue #8 is a subscription side effect; #9 is incomplete output without metadata. They can expose one another but are independently testable.

## Reproducible tests

- [First two defects](repro/0.7.1-regressions_test.go.txt).
- [Eight subsequent defects](repro/0.7.1-additional-regressions_test.go.txt).
- [Verification controls](repro/0.7.1-issue-confirmation-controls_test.go.txt).

Prepare an independent release-source directory from the repository root:

```sh
sctp_verify_dir="$(mktemp -d)"
git archive 0.7.1 | tar -x -C "$sctp_verify_dir"
cp docs/repro/0.7.1-regressions_test.go.txt "$sctp_verify_dir/release_audit_linux_test.go"
cp docs/repro/0.7.1-additional-regressions_test.go.txt "$sctp_verify_dir/additional_regressions_test.go"
cp docs/repro/0.7.1-issue-confirmation-controls_test.go.txt "$sctp_verify_dir/confirmation_controls_linux_test.go"
```

On the unfixed release, controls should pass and defect assertions should fail. Run with each Go toolchain:

```sh
(
  cd "$sctp_verify_dir"
  go test -race -run '^TestConfirm' -v -count=3 -timeout=60s .
  go test -race -run '^Test(ReleaseAudit|Review)' -v -count=5 -timeout=60s .
)
```

These conclusions cover only the stated baseline, environment, and triggers. Confirming issue validity does not establish that the library has no other defects or that the fixes are complete.
