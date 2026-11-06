# v0.7.4 validation and publication recovery

Date: 2026-11-06. The #19–#24 library/example/test fixes passed all 14 PR jobs
in [PR #25](https://github.com/skill215/sctp/pull/25). The merged tree at
0ebac5e was identical to that reviewed candidate.

The subsequent [release run](https://github.com/skill215/sctp/actions/runs/42144113334)
stopped on Ubuntu 24.04 / Go 1.26.10: FuzzCodec completed 320471 executions in
about ten seconds, then returned `context deadline exceeded`. It reported no
target assertion, panic or crash input. Publish and notes jobs were skipped;
no v0.7.4 tag or release was created. This failed attempt remains part of the
record rather than being relabeled or retried until green.

## Fuzz gate (#26)

The exact local ten-second Codec retry passed. Independent no-op controls
passed 40 short duration-based and 40 count-based sessions. Those controls did
not independently reproduce the deadline race. The Codec target has no context
or deadline operation; the observed error comes from fuzz infrastructure.
Local Go coordinator/context source shows a cancellation-ordering window at
duration shutdown, but attribution to that particular window remains an
inference. This is not reported as a reproduced SCTP payload defect.

The revised gate requires each of the four existing targets to complete
1000000 executions, with a 100-execution minimization budget and a separate
180-second hard failure timeout. It does not retry or ignore any error. This
gives an explicit executed-input budget instead of relying on a wall-clock
deadline to signal successful completion. Both normal code paths and minimizer
invocations remain actual target executions. A crash still fails the gate.

Local Codec and RawSockaddr sessions completed their full one-million budgets.
Go 1.27 accepted the count-based options in an independent control. A deliberately
failing generated-input control returned nonzero and wrote its crash input,
confirming that the revised policy does not suppress target failures.

## Pending publication (#27)

After the failed validation, releases/v0.7.4.md already existed but the tag did
not. The previous additions-only note selection could not publish a corrected
candidate that modified those notes. The offline policy test reproduced this:
the new-note case passed, while the pending-note case failed because no tag or
release-create call was made.

The publisher now considers explicitly modified canonical notes as well as new
ones. If a modified note already has a release tag, the tag is retained and is
not republished; a newly added note colliding with an existing tag still fails.
An explicitly edited untagged candidate may be tagged only after the complete
validation dependency succeeds. The tag is created at that validated commit.

The new standard-library Python test extracts the actual publication shell
from the workflow and runs it in temporary local Git repositories with offline
gh/go mocks. New candidate, pending candidate, existing released notes and new
note/tag collision all pass, including target commit and immutable tag checks.
The test runs inside the existing native validation script. It does not contact
GitHub or create a real release.

## Shared test ports (#29)

The first recovery PR run exposed a separate pre-existing isolation defect:
Ubuntu 22.04 / Go 1.26 failed race round 2 with EADDRINUSE in two fixtures that
bind port 54321. An independent parent process reserving that SCTP port made all
four affected fixtures fail deterministically. The actual busy-port owner on
the remote runner is unknown; this is not attributed to the library's transport.

Notification, RawConn and listener-from-file tests now bind port zero and use
the assigned address. Dial fixtures explicitly accept and clean up peer sockets;
cleanup is registered before further assertions can fail. The generic listener
name table's unnecessary fixed 7777 case also becomes dynamic. Parser-only
numeric-port tests are retained. The permanent child-process regression keeps
legacy ports unavailable and runs the affected tests; an already-busy legacy
port does not skip those child checks. Reserved-port probes passed three times,
and Go 1.27 passed ten shuffled race repetitions of the affected fixtures.

## Final-candidate requirements

Only test fixtures and validation/publication machinery changed in this recovery;
Go library and example implementation source is unchanged. The
native two-kernel/two-toolchain matrix, ten race processes per native job,
vet/checkptr/low-GOGC, namespace regressions, README/consumer checks, unsupported
stubs and six QEMU ABI jobs remain required. The corrected PR and its merged
commit must each complete that full matrix. Only then may v0.7.4 publish.
