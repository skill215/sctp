# v0.8.0 I/O acceptance coverage audit

Audit date: 2026-11-10. Decision baseline:
[synchronous APIs over nonblocking descriptors](io-interface-design.md).
This bounded audit maps A1-A11 to existing implementation and tests, runs focused
checks, and distinguishes current coverage from future channel requirements.

**Outcome:** no implementation contract defect was reproduced in the inspected
paths and selected checks. Keep the existing interfaces and execution model.
Update the historical handoff's current-status notice; do not add a channel
API, worker layer, concurrent sample, or patch release on the strength of this
audit. This is not a claim that all deployments or all consumer code are verified.

## 1. Exact baselines and evidence

| Component | Audited baseline | Evidence scope |
| --- | --- | --- |
| SCTP library | Documentation checkout `f596dc87148f589421dad6b23a368ac6dab4f73e`; root Go files unchanged from v0.8.0 commit `bce1b3cf3ea32a32a86262c7f655d565bb40055c` | Fresh focused CGO0 and race runs; historical compatibility audit reused for version differences |
| diameterkit core | Frozen local batch74 source snapshot | Fresh selected core race tests; production candidate files checked against the batch74 manifest |
| diameterkit SCTP adapter | Frozen local Go/module snapshot requiring offline core `v0.0.0-diameterkit.batch74` and public SCTP `v0.8.0` | Fresh selected adapter race tests, no replace, offline module selection and verification |
| Historical consumer acceptance | Local batch67, batch72, batch73, and batch74 records | Context only; their full-suite results are not counted as fresh runs here |

The consumer workspace advanced from batch73 to batch74 while this audit was
running. Final consumer checks use a frozen copy, not a moving working tree or a
claim of a clean/public consumer commit. All 123 batch74 production-candidate
files matched the manifest at snapshot creation; the corresponding cached core
also matched. All 70 cached SCTP Go source files matched the recorded v0.8.0
source hashes. The final source snapshots were unchanged during validation.

Machine-readable records:

- [Results, commands, individual test outcomes, and limitations](evidence/io-acceptance-audit-2026-11-10/results.json).
- [SCTP source hashes](evidence/io-acceptance-audit-2026-11-10/library-sources.json).
- [Frozen consumer source hashes](evidence/io-acceptance-audit-2026-11-10/consumer-sources.json).

Consumer paths below are relative to `skill215/diameterkit`. They identify the
hashed local snapshot, not guaranteed content of that repository's mutable
default branch. No consumer source, dependency declaration, or test was edited.

## 2. Existing execution and ownership model

Inspection found an existing integration that does not need a new delivery layer:

- `adapters/sctp/listener_linux.go`: one admitted Accept caller, context-aware
  upstream accept, a final handoff check, and Incoming Establish/Close ownership.
  It creates no background accepted-connection queue.
- `adapters/sctp/conn_linux.go`: one ReadEvent path, fixed owned notification
  projection, no retained Tail, and a first terminal cause before socket Close.
- `reader.go`, `writer.go`, and `transport_events.go`: separate I/O execution;
  transport facts are applied synchronously between socket operations. Sequence
  and connection state reject stale events. Notifications do not count as
  Diameter liveness or proof of remote processing.
- `completion.go`: a bounded completion ring under the Node state lock.
  Channels signal state changes; they do not transport borrowed SCTP payloads.
  There is no blocking channel send that must succeed to record terminal state.
- `node.go` and `shutdown.go`: Close requests retirement; Wait tracks I/O and
  borrowers. Shutdown timeout can return earlier than actual retirement.

**Wait is not a claim that every business reservation is zero.** Unconsumed
completions remain owned by their CompletionPort and retain charges after Node
Wait. Receive or Port.Close releases them. Port.Close can itself return on
context expiry while a borrower still exists; later completion must still be
observed. The tests below explicitly preserve this distinction. Treating those
retained charges as a leak, or releasing them merely because Node.Wait returned,
would misread the current contract.

## 3. A1-A11 coverage and disposition

Status meanings: **covered** means the scoped behavior has inspected code and
fresh selected test evidence; **partial** identifies a remaining limit;
**not applicable** means the described optional mechanism is absent, not that
its tests passed; **missing** means evidence was not produced. These labels are
not certification of every interleaving, platform, or workload.

| Gate | Owner and inspected/tested evidence | Status and disposition |
| --- | --- | --- |
| A1: waiting without polling | SCTP `setup_socket_linux.go`, `socket_deadline.go`; TestAcceptContextIdleCancellationAndReuse, TestRawConnReadWaitForReadiness, TestRawConnWriteWaitForReadiness, TestMessageDeadlinesCancelAndClose | **Partial.** Real readiness/cancellation and bounded idle-Accept syscall-count observation pass. No general CPU/thread scaling benchmark was run; timing observations do not prove an unlimited-duration performance property. |
| A2: Accept publication races | SCTP TestAcceptContextPublicationBarriers and TestAcceptContextPreflightAndQueuedIsolation; consumer TestListenerCanceledArrivalAndFreshAccept, TestListenerCloseUnblocksAndIncomingCloseRace, TestServeLateAcceptRetainsCandidateAndWait | **Covered for existing handoff.** Native calls plus controlled race windows prove cleanup, listener reuse, and ownership retirement. Buffered connection claim/lease protocols are **not applicable** because none is present. |
| A3: deadlines and timeout identities | SCTP TestMutableDeadline* and TestAcceptContextConcurrentCallersAndTimeoutSnapshot; TestReadSocketTimeoutIncludesNotifications; adapter TestFragmentsNeverRefreshDeadlineOrBorrowAfterReturn | **Covered for the existing wait/frame paths.** Modify/clear/queued deadlines, timeout snapshots, and frame budget retention are exercised. The adapter test's name is broader than its assertion: its expired-frame test alone does not prove asynchronous buffer lifetime. |
| A4: full/abandoned consumer queue | Consumer `completion.go`, `node.go`; TestControlProgressWithFullCompletionPort, TestNodeCloseLeavesChargedCompletionsAndWaitBarrier, TestStoppedNodeReleasesFixedReserveAfterPortClose | **Partial.** Existing full-port control progress and explicit completion ownership pass on controlled transports. A generic SCTP data/event channel with stall deadlines is **not applicable**; no such queue exists. Native end-to-end saturation/performance is not established by these core tests. |
| A5: claims and shutdown races | Consumer TestListenerCloseUnblocksAndIncomingCloseRace, TestPortCloseWaitsForBorrowedWriter, TestManagedSCTPWaitRetainsEstablishBorrower, TestShutdownReturnsAtConfiguredBoundWhileWaitTracksBorrower | **Covered for existing Incoming/Port/Wait ownership.** Close intent and actual retirement remain separate. New buffered-connection claims and buffer-lease return channels are **not applicable** and must be accepted separately if introduced. |
| A6: data/notification/terminal ordering | SCTP TestMessageContinuityNotificationPositionsAndSwitches, TestMessageRestartInvalidatesSnapshot, TestMessageContinuityPartialAbortLatches, TestNotificationNative*; consumer TestTransportTerminalEventSettlesOnceAndRetires and TestUpstreamTerminalRecordsPreserveFacts | **Covered for existing single-reader and generation handling.** Real notification production and controlled terminal projection are distinguished. Restart fault injection is not represented as native restart interoperability. |
| A7: buffer lifetime | SCTP TestNotificationFiveLayouts mutates parser input to prove owned fixed fields and borrowed Tail; consumer `conn.event` projects only owned fields; TestTransportFactsAreBoundedAndNotLiveness proves snapshot ownership | **Covered for current synchronous projection; optional async leases not applicable.** Race runs support exercised paths. No evidence exists for an unimplemented queue carrying borrowed payloads; do not infer it from parser or frame-deadline tests. |
| A8: progress with cancellation/Close | SCTP TestMessageCommittedWriteEvidenceUnderCancelClose and TestMessageWriteSingleAttemptFaults; adapter TestWriteEvidenceAndNoTailRetry, TestRealSCTPBlockedWriteCancellationAndClose, TestUpstreamRestartWriteLatchPreservesDelivery | **Covered for selected send paths.** Counts, accompanying errors, first cause, and one-send behavior remain visible. Controlled short-write injection and real blocked-write cancellation provide different evidence. |
| A9: admission and retirement ledger | SCTP TestU5ResourceLedger, TestU5MessageCancellationRetiresOwners, TestSetupOwnershipAndNoIOQuery; consumer TestManagedSCTPAdmissionAndIdleStops, TestManagedSCTPWaitRetainsEstablishBorrower, and the port-borrow tests | **Partial.** Selected logical admission and delayed-retirement paths pass. All application budget combinations, allocator/RSS behavior, and hypothetical queue charges are outside this focused run. Batch64/65/67/74 full acceptance remains separate historical evidence. |
| A10: compatibility | Root Go files are identical to v0.8.0; fresh Accept/mode tests and the previous exact-tag API audit | **Covered for retaining the current library interface.** No production Go change or consumer migration was made. The 0.4-to-v0.8.0 behavioral changes remain documented and are not undone. |
| A11: performance | No asynchronous optimization is proposed or implemented | **Missing performance evidence.** Define target workloads and thresholds before a dedicated benchmark. There is no basis here to claim channels are faster or to add them as an optimization. |

Library test entry points are in [setup/accept tests](../setup_socket_linux_test.go),
[deadline tests](../socket_deadline_linux_test.go),
[message tests](../message_linux_test.go),
[continuity tests](../message_continuity_linux_test.go),
[notification layout tests](../notification_linux_test.go),
[native notifications](../notification_native_linux_test.go), and
[resource tests](../resource_linux_test.go).

Consumer entry points are `runtime_test.go`, `serve_test.go`, `shutdown_test.go`,
`transport_events_test.go`, and
`adapters/sctp/{setup_linux_test.go,managed_linux_test.go,conn_linux_test.go,migration_linux_test.go}`.
The results JSON lists the exact executed tests, including subtests, so the
mapping above does not imply that every test in those files was rerun.

## 4. Fresh validation and unsuccessful attempts

All final runs used Go1.26.10 on Linux/amd64,
kernel `6.6.114.1-microsoft-standard-WSL2`, with `-count=1`, JSON output, and a
120-second Go test timeout per process.

| Final run | Top-level tests passed | Tests plus subtests passed | Failed / skipped |
| --- | ---: | ---: | --- |
| SCTP focused CGO_ENABLED=0 | 60 | 101 | 0 / 0 |
| SCTP same focused selection, race | 60 | 101 | 0 / 0 |
| Frozen consumer core selection, race | 8 | 14 | 0 / 0 |
| Frozen SCTP adapter selection, race | 12 | 39 | 0 / 0 |

The core selection uses controlled transports, not native SCTP. Library and
adapter selections combine real loopback SCTP, controlled injection, and pure
validation tests. A passing process does not make all its cases native tests.
Module selection retained SCTP v0.8.0 and the offline batch74 core, with GOWORK
off, no replace, GOPROXY off, and GOSUMDB off. `go mod verify` passed; existing
cache contents were checked against recorded manifests and go.sum. This is not
a fresh download or new public-module publication proof.

Two failed build attempts are retained in the evidence and excluded from passes:

1. The initial core link failed because the `/tmp` tmpfs had insufficient space.
   No test case executed. Compiler cache and temporary output were moved to a
   task-owned directory on the workspace filesystem before retrying.
2. The adapter's dependency advanced to batch74 while the first consumer check
   still used the batch73 offline cache. Module lookup was disabled, so setup
   failed before tests. A manifest-checked batch74 snapshot and matching cache
   resolved the baseline mismatch. An intermediate core pass on the moving
   workspace was superseded by the frozen run and is not counted twice.

The results JSON contains exact `go test` argument arrays and log hashes, along
with per-test outcomes. To reproduce, use the recorded SCTP commit and frozen
consumer source manifest, select the same dependency versions, set a writable
GOCACHE/GOTMPDIR with sufficient disk space, and run each recorded command from
its library/core/adapter module root. The consumer core is an offline development
candidate; a public checkout alone may not provide that module. Do not replace
it with a different core silently and label the result equivalent.

No full consumer suite, second-kernel run, cross-architecture matrix, performance
benchmark, or real DRA/OCS acceptance was performed in this round.

## 5. Findings and next actions

| Finding | Action / owner | Exit criterion |
| --- | --- | --- |
| Handoff still begins with a pre-release dependency state | SCTP documentation: add a dated current-status notice while preserving the historical batch66 instructions | Readers can distinguish batch67 official pinning, batch72 modes, and the audited batch74 snapshot from the old starting point |
| A1/A4/A9 do not establish scaling, native saturation, or RSS limits; A11 has no baseline | Future consumer/performance work, only against declared deployment needs | Fix connection count, payload sizes, queue limits, Go/kernel/hardware and thresholds before measuring CPU, threads, memory, throughput, tail/stop latency and retirement |
| Optional channel queues and leases have no implementation | No implementation task now | If a concrete consumer needs them, submit their ownership/queue/budget design and satisfy the affected A4-A9 gates before adoption |
| Node.Wait and completion-port ownership are easy to conflate | Preserve and document the existing two-stage retirement contract | I/O/borrowers retire before Wait; completion charges remain owned until Receive or Port.Close |
| Current consumer source may move independently | Freeze source and dependencies for future evidence | Every acceptance record identifies its actual source hashes and distinguishes historical results from fresh ones |

There is no reproduced SCTP code defect requiring a repair issue or v0.8.1 from
this round. Keep the existing serial message example: it is intentionally
bounded, and no demonstrated scheduling requirement justifies adding a second
sample or a new worker architecture. Target DRA/OCS interoperability and consumer
publication remain consumer-owned work, not reasons to reopen the SCTP API.
