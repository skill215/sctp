## Why

Release 0.7.1 still has 14 defects or input-validation gaps confirmed by repeated reproduction and root-cause isolation, plus #13 as a P3 non-blocking consistency improvement. Issue #1 is a nonblocking-receive regression introduced in 0.5; most other issues date back to 0.3. Version v0.7.2 should focus on restoring reliable I/O, address, and fd-lifecycle behavior.

## What Changes

- Fix #1: after receive returns EAGAIN, distinguish a disconnected one-to-one socket from an active idle connection, preserving the poller, Close wakeups, and one timeout budget per operation.
- Fix #4, #5, #6, #9, and #12: define complete behavior for empty reads/writes, zero-payload control sends, EOF flags, wrapper headers, and returned counts.
- Fix #8: enable DATA_IO over existing subscriptions, serialize library subscription updates, and retain initialization failures.
- Fix #2, #7, #10, #11, and #14: keep caller addresses unchanged; validate high-level input before socket creation/Control; reject empty multihoming segments; query complete address lists with bounded dynamic buffers.
- Fix #15 and #16: align public APIs across platforms and provide explicit cleanup for fds accepted by unsupported constructors.
- Plan to normalize #13's Accept error result as well. It remains P3; deferring it separately does not change the 14 release blockers.
- Add permanent regressions, raw-syscall controls, downstream consumer builds, and architecture checks; publish canonical tag `v0.7.2` while retaining old tags.

This stage delivers design and tasks only. Public function signatures, the 32-byte wrapper header format, Go 1.26 minimum, and Go 1.26/1.27 test matrix remain compatible. It does not add ReadMsg, DialContext, or standard deadlines.

## Capabilities

### New Capabilities

These establish specification coverage for the first time; they do not imply new product features with the same names.

- `sctp-association-lifecycle`: receive semantics for association termination, idle waits, concurrent I/O, timeouts, and Close; #1 and #13.
- `sctp-message-io`: receive counts, empty payloads, control messages, metadata, and wrapper frames; #4, #5, #6, #9, and #12.
- `sctp-event-subscriptions`: preserving subscriptions, concurrent library updates, and propagating initialization failures; #8.
- `sctp-address-contracts`: input snapshots, validation, multihoming parsing, and dynamic address queries; #2, #7, #10, #11, and #14.
- `sctp-platform-compatibility`: public API consistency and unsupported-fd ownership; #15 and #16.
- `sctp-release-validation`: regression evidence, native/emulated coverage boundaries, release gates, and canonical version tags.

### Modified Capabilities

No existing OpenSpec specifications require changes.

## Impact

- Main implementation locations: `sctp.go`, `sctp_linux.go`, `socket_io_linux.go`, and `sctp_unsupported.go`, with new internal helpers and necessary platform files.
- Permanent tests extend lifecycle, timeout, codec, zone, and regression coverage. Historical .txt probes remain audit evidence rather than being copied wholesale into the suite.
- Update gates and `v0.*` support in .github/workflows/ci.yaml, arch.yaml, and release.yaml. Release notes document corrected behavior and explicit resource boundaries.
- No new third-party runtime dependencies. Status, progress, and existing evidence link to [GitHub #3](https://github.com/skill215/sctp/issues/3).
