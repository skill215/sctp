# v0.7.2 implementation evidence

Baseline: 70b338ac419dff6aa745460bb379d68208d615e0 (library identical to 0.7.1).
Branch: codex/fix-v0-7-2-stability. Existing historical audit fixtures and
release-plan edits were present before implementation and are preserved.

## Batch A — receive counts, metadata, event subscriptions

Regression file: sctp_stability_io_linux_test.go. Before the fix, the new tests
observed empty SCTPRead returning 1 and consuming input, reset reads returning
-1, and io.ReadAll panicking. Existing audit evidence covers the independent
missing-metadata and overwritten-subscription triggers.

After the fix: focused race suite passed three repetitions; full race suite
passed with Go 1.26.10 and Go 1.27.2, Linux/amd64 kernel
6.6.114.1-microsoft-standard-WSL2. Existing Read/Write/Accept/Connect timeout,
notification-budget, lifecycle, metadata and codec tests passed.

Review: eventsMu covers only configuration syscalls; wrapper construction uses
one read-modify-write operation. No lock spans handlers or blocking I/O. Setup
failure returns before I/O but Close still delegates. Zero reads validate the
object first. Error reads report zero; successful bytes plus metadata errors
retain a fully initialized frame. Input buffers remain untouched on short reads.

The SCTP_STATUS ABI/scope probe passes three local repetitions. Additional
native-kernel evidence is still required before batch B is implemented.
