# v0.7.3 example fix and release validation

Date: 2026-11-04. Scope: this repository's CLI example, regression coverage and
documentation. Public library signatures, module dependencies and SCTP socket
implementation are unchanged.

## Defect and fix

The CLI example used one incrementing counter for both PPID and stream ID.
With a peer accepting one or two streams, the first out-of-range send failed
with EINVAL and terminated the client. The original executable reproduced the
two-stream case twice during the library review. The permanent regression also
fails against the original source with both one-stream and two-stream peers.

The example now reads GetStatus().Ostreams and selects stream IDs modulo that
negotiated count. PPID uses its own uint32 value and keeps increasing. Failure
to read status or a zero outbound-stream count is reported before sending.

The regression starts the actual CLI entry point in a test subprocess and uses
a real SCTP peer advertising one or two streams. Four messages must arrive
with increasing PPIDs and valid cyclic streams, and are echoed to let the CLI
continue. Tests own the listener, connection, child process and bounded waits.
This checks the original fatal-error path as well as the fixed send behavior.

Local before/after evidence is retained under
`docs/repro/v0.7.3-example/`. Three race-enabled rounds pass after the fix. Full race/shuffle and vet also
pass locally on Go 1.26.10 and 1.27.3; exact outcomes are in
[the checks](repro/v0.7.3-example/checks.json).

## Documentation and evidence

The release includes the Linux-only support clarification, the library scope
document and the preceding library review. Large passing-run payload logs are
kept outside the source tree; their original hashes, command outcomes and
per-run test counts remain recorded. Small reproduction logs and diagnostic
sources are included for review.

The preceding unmodified library passed 60 independent race/shuffle processes
across local and two remote native kernels, plus six architecture ABI jobs.
That evidence does not replace the final candidate's CI. Publication uses the
existing release workflow, which validates the merged candidate before creating
an annotated tag and verifies the published module version.

Published diagnostic records redact local absolute paths and internal IP
addresses. Original unredacted records remain outside the source tree.
