# diameterkit Session: SCTP v0.8.0 Integration Handoff

## Current status update: 2026-11-10

The original handoff below describes the 2026-11-09 batch66 starting point; it
is retained as historical guidance, not a current list of unfinished work.
Local consumer records report official v0.8.0 pinning and batch64/65/66 regression
in batch67, and explicit OrderedMultiStream/PPID0 policies in batch72. Do not
repeat those migrations or infer that the consumer still requires a candidate
pseudo-version. These policies belong to the consumer and do not change SCTP's
transport-level APIs or automatically enable application modes.

The [I/O acceptance audit](io-acceptance-audit.md) froze a local batch74 consumer
snapshot while its workspace was advancing. It verified SCTP v0.8.0 with the
original sums and no replace, checked offline dependency/source manifests, and
ran focused core/adapter race checks. It also distinguishes Node.Wait's I/O and
borrower barrier from CompletionPort-owned results that remain charged until
Receive or Port.Close. The audit does not rerun the complete batch64/65 acceptance
matrix or certify real DRA/OCS interoperability, consumer publication, or RSS.
Use its exact hashes and coverage table for this round's evidence; inspect the
consumer's current workspace before planning later work.

## Historical handoff: 2026-11-09

Handoff date: 2026-11-09 (Asia/Shanghai). Objective: pin the independent SCTP adapter to published v0.8.0, review the existing thin-adapter migration, and rerun batch 64/65 runtime acceptance and batch 66 migration regressions against the official version. Baseline: Go 1.26, production CGO_ENABLED=0, pure Go, no persistence.

**Historical starting point: diameterkit had completed batch 66 candidate migration but had not switched to the official version.** At the time of writing, `adapters/sctp/go.mod` still uses `v0.7.5-0.20261109072213-bb739b612088`, and existing code already calls A–E APIs. Do not reimplement polling, syscalls, address algorithms, or continuity checks already removed, based on historical batch 64/65 documents. Reinspect the workspace when the next session starts and protect existing changes. This handoff neither modifies diameterkit nor runs its tests with the official version pinned.

## 1. Exact version and provenance

| Item | Final value |
| --- | --- |
| module | `github.com/skill215/sctp` |
| Official version | `v0.8.0` |
| release commit | `bce1b3cf3ea32a32a86262c7f655d565bb40055c` |
| Annotated tag object | `5b6374817a72dd51e630032ab1b64b1791dba6ed` |
| Candidate being replaced | `v0.7.5-0.20261109072213-bb739b612088` |
| Minimum Go / production build | Go1.26 / `CGO_ENABLED=0` |

Change the independent adapter's require entry to:

```go
require github.com/skill215/sctp v0.8.0
```

The two official `go.sum` entries must match the actual download:

```text
github.com/skill215/sctp v0.8.0 h1:7N92RMEO7s4+4IHzI8KYKUgyIPpIClIVAdgg93m+RFI=
github.com/skill215/sctp v0.8.0/go.mod h1:g/XAScdNEAqQa2cjZdtxXVkxIJZEy77f7/fmCkTD2gg=
```

Do not reuse a pseudo-version's module sum by changing its version label. Generate/verify sums through an actual download with `GOWORK=off`, no `replace`, and inspection of the version selected in the dependency graph. Do not add an SCTP dependency to the core module.

Upstream has completed final PR CI 14/14, the merge-commit release workflow 16/16 (14 validation jobs + publish + notes), native/race consumption of the official module, `go mod verify`, and hash comparison of all 70 Go source files. At publication, `@latest` resolved to v0.8.0. All 70 sources match the bb739b6 candidate consumed in batch 66, so this task primarily pins the official version and repeats acceptance; it does not assume another adapter redesign is needed. Use the exact version, not mutable `@latest`, in delivery requirements.

Sources:

- [Official Release](https://github.com/skill215/sctp/releases/tag/v0.8.0)
- [Publication and official go.sum evidence in #31](https://github.com/skill215/sctp/issues/31#issuecomment-6735592144)
- [Final PR CI](https://github.com/skill215/sctp/actions/runs/42583623648)
- [Release workflow](https://github.com/skill215/sctp/actions/runs/42584877736)
- [v0.8.0 API/resource contracts](https://github.com/skill215/sctp/blob/v0.8.0/docs/bounded-setup.md)
- [Independent upstream consumer source](https://github.com/skill215/sctp/blob/v0.8.0/docs/examples/bounded-consumer/main.go.txt)

Each upstream matrix round explicitly skips three 64-bit-int-only tests on ARM and three on MIPS; these are not passes. QEMU provides ABI evidence, not native network acceptance on those architectures. Upstream success does not replace diameterkit business/lifecycle acceptance.

## 2. Session startup and scope

In the diameterkit repository, first read applicable AGENTS.md files, git status, and:

- `adapters/sctp/{go.mod,go.sum,README.md,config.go,setup.go,listener_linux.go,conn_linux.go}`.
- `adapters/sctp/{setup_linux_test.go,conn_linux_test.go,integration_linux_test.go,managed_linux_test.go,migration_linux_test.go}`.
- `spec/batch-64.md`, `spec/batch-65.md`, `spec/batch-66.md`, and their `spec/evidence/` records.
- `doc/diameterkit-runtime-contracts.md` §3.5 and batch 64–66 notes, `doc/diameterkit-sctp-upstream-contract.md`, and `doc/sctp-upstream-improvement-plan.md`.
- `spec/reviews/2026-11-09-sctp-upstream-handoff.json` as the original handoff requirements; its old candidate status is not the current publication state.

Record this round through the project's current batch process without overwriting historical evidence. Update the independent adapter version, README/current contracts, and new evidence. Retain historical versions/results in old batches, adding superseded notes if needed. Fix only concrete integration issues exposed by official-version acceptance. Do not add a v1 rewrite, DTLS, discovery/routing frameworks, automatic business replay, OrderedMultiStream, or a named PPID0 mode.

## 3. API replacement and review checklist

The table maps old implementations to v0.8.0. Batch 66 reports these migrations complete; the next session should verify and retain them rather than repeat the work.

| Previous adapter responsibility/implementation | v0.8.0 public API | Responsibility retained by the adapter |
| --- | --- | --- |
| 20ms SO_RCVTIMEO, EAGAIN loops, AcceptPollInterval | `ln.AcceptSCTPContext(ctx)`, `ln.SetDeadline(t)` | Single Accept caller, ErrConcurrentAccept, transport handoff gate, one Incoming.Establish/Close |
| validateEndpoints/resolveEndpoint/validateResolved, address freezing/copying | `AddressLimits`, `SetupConfig`, `NewDialer(...).DialContext(ctx)`, `ListenContext(ctx,...)` | Configuration mapping, Requirements before admission, absolute Node Handshake budget, thin Dialer/Listener wrappers |
| Clearing SO timeouts directly through SyscallConn | `conn.SetSocketTimeouts(0,0)` | Exclusive takeover, Close on failure; separately clear old standard deadlines with `SetDeadline(time.Time{})` |
| getsockname, private Addr snapshots/all-interface queries | `ln.BoundAddr()` | External address use/advertisement policy; wildcard addresses are not routable addresses |
| UpstreamReserve=8192, copied N/Z/D address formulas | `SetupRequirements(cfg)` and returned `IORequirements` | Owned state/buffers, Node candidate/listener budgets, core event/control/business allowances |
| Native MSG_EOR and cross-fragment assoc/stream/PPID/ordered/SSN cursors | `ReadResult.EndOfRecord()` and `ReadEvent` continuity checks | Diameter length/EOR correspondence, prefix/body/discard, PPID46 policy, frame deadline, notification limits |
| Requested stream counts used as negotiated counts | Actual `Instreams/Ostreams` from `GetStatus()`; upstream directional send/receive checks | Current named modes, stream selection, and admission policy; do not automatically enable multistream modes |
| Separate notification reader/callback, retained variable-length tails | The same `ReadEvent` read path, `ParseNotification` | Fixed transport.Event mapping, Sequence/ConnectionID, first-cause gate, Peer actions |
| Retrying short-write tails / cancellation overwriting actual send results | One send through `WriteMsgContext`, actual n/error | Written/Uncertain business evidence, one completion; no automatic tail resend |

Keep `Messages` non-nil, explicit SNDRCV, five notifications, NoDelay, permitted network fragmentation, and interleave=0. Reuse the adapter's current `SocketConfig()`; do not switch the current Diameter mode to multistream merely because an upstream example uses stream1. Relative to v0.7.4, the added `SocketConfig.Messages` field affects unkeyed literals; consistently use keyed literals. The new bounded API rejects Control/NotificationHandler.

Configure N/Z/D explicitly for the combined local+remote set. Defaults are 16/256/4096; supported ranges are N=1..64, Z=0..4096, D=1..16384. Require numeric ports and a nonempty remote endpoint with a nonzero port; multihoming uses explicit slash-separated input. DNS selects one suitable IP per host without automatic discovery or expansion into multihoming. Resolution and connection share the original ctx; do not restart the full timeout for each stage.

### Integration details that must be preserved

- After successful Accept handoff, canceling the old ctx or closing the listener does not close the returned connection. The adapter must still protect its own wrapping/Incoming handoff window. A cancellation request does not mean resources have retired.
- Listener deadlines affect current/subsequent Accept calls only; zero clears them, and connections do not inherit them. Distinguish `context.Canceled/DeadlineExceeded`, `os.ErrDeadlineExceeded`, and `net.ErrClosed`; do not wrap all of them as retryable timeouts.
- The two setsockopt calls in `SetSocketTimeouts` are not transactional. A `SocketTimeoutUpdateError` may leave a partial update; takeover failure requires closure. SO timeouts do not replace standard deadlines. BoundAddr returns an owned copy and preserves wildcards; FileListener without a snapshot returns `ErrBoundAddrUnavailable`.
- **Do not discard a valid terminal notification merely because err or CloseRequired is present.** RESTART can carry a valid notification, `ErrAssociationChanged`, and CloseRequired together. A partial-abort during a message may be a successful notification with CloseRequired. After validating consumption state, length, type, and error combination, retain the fixed terminal-event fact and accumulated Progress before closing the connection generation. Erroneous consumption, malformed records, and unconsumed records must not fabricate continuation. Preserve the corresponding batch 66 fixes.
- A writer may observe the restart latch first. Preserve actual n and `ErrAssociationChanged`/`transport.ErrAssociationClosed` identity; do not invent a receive Sequence or overwrite an already committed local-budget/protocol first cause.
- Losing message boundaries after consumption requires closure; `ErrReadUnusable` forbids further reads. Cancellation before consumption must not clear the cursor or invent EOR. Notifications neither refresh the whole-frame deadline nor reset the current user message.
- Upstream validity of PPID0 and SID>0 does not enable adapter PPID0 compatibility or OrderedMultiStream. Preserve the current PPID46, ordered control stream0, and CEA/DWR barrier policies.

## 4. Resource accounting and admission

Machine queries are the sole source of upstream charges:

```go
r, err := upstream.SetupRequirements(upstream.SetupConfig{
    Socket: adapter.SocketConfig(),
    Limits: upstream.AddressLimits{
        MaxAddresses: 16, MaxZoneBytes: 256, MaxAddressBytes: 4096,
    },
})
// Handle err and record r.LedgerVersion; this query performs no DNS,
// socket I/O, or kernel capability probing.
```

Here, `upstream` means `github.com/skill215/sctp`, and `adapter` means the independent diameterkit SCTP module. Application wiring should prefer the adapter's existing `Config.Requirements()`/`SetupConfig.Requirements()`, avoiding another copy of its composition algorithm.

**Upstream example:** Go1.26.10 / Linux amd64, N16/Z256/D4096, `LedgerVersion=bounded-setup-v2/linux/amd64`.

| Returned quantity/combination | Logical bytes |
| --- | ---: |
| DialerBytes | 8960 |
| ListenerBytes (excluding child connections) | 48524 |
| EstablishBytes (including the connection being established) | 78380 |
| ConnectionBytes | 9516 |
| DialerBytes + max(EstablishBytes,ConnectionBytes) | 87340 |
| Upstream listener + old connection + new candidate | 145380 |

`ConnectionBytes` already includes peek/read/write scratch 8/480/280 and one fixed parsed Notification; do not add them again. OOBMin=48; NotificationMin=148 for the five notifications. Caller-owned OOB and NotificationMax buffers are charged separately. Do not add the old 8192 allowance or address formula to the new totals.

**Current adapter example:** NotificationMax=4096, StateReserve=4096, SetupStateReserve=4096. The latter two are diameterkit-owned logical reserves; keep them there rather than moving them upstream.

| Adapter declaration | Composition formula | Logical bytes |
| --- | --- | ---: |
| Established-socket takeover `Config.Requirements().BufferBytes` | ConnectionBytes + StateReserve + NotificationMax + OOBMin | 17756 |
| Per listener `SetupConfig.Requirements().BufferBytes` | ListenerBytes + SetupStateReserve | 52620 |
| Per physical candidate `ConnectionBufferBytes` / Dialer.Info / Incoming.Info | DialerBytes + max(EstablishBytes,ConnectionBytes) + SetupStateReserve + StateReserve + NotificationMax + OOBMin | 99676 |
| Core events/facts per candidate | Existing independent core allowance; do not duplicate it in adapter Info | 2048 |

Wiring: for pre-established socket takeover, configure `Resources.AdapterBytesPerConnection` from the query yielding 17756. Managed Dial/Maintain/Serve uses `ConnectionBufferBytes`; Serve additionally sets `Resources.ListenerBytesPerPeer=BufferBytes`. Derive all values from the current query. Table values are a local baseline, not cross-architecture constants.

Add the listener and every physical candidate, including waiting Accept, active/passive candidates, election losers, retiring old generations, and Establish borrowers that have not exited. After handoff, a managed candidate conservatively retains 99676; do not prematurely reduce it to the established-takeover value 17756. Example: one listener plus two managed candidates still holding slots costs 251972; adding 2048 of event allowance per candidate gives 256068. Control/emergency/business/pending allowances remain separate. This is not the entire Node budget formula.

Assumptions: one reader, one writer, one Accept caller, no upstream queuing, controlled contexts, and bounded serial deadline updates. Arbitrary waiters/context graphs, Go runtime/GC/allocator/stacks, kernel queues, and DNS/zone infrastructure are outside this logical ledger; it is not an RSS guarantee. Release reservations only after calls and hooks/borrows actually exit, not when cancel or Close is merely requested.

Batch 64's 16432 and batch 65's 57728/61872 belong to old candidate accounting. Batch 66's 17756/52620/99676 are this round's review baseline. Upstream 145380 is a bare-upstream-object example and cannot replace the Node physical-candidate allowances above.

## 5. Batch 64/65 and migration regression acceptance

Execute everything against the same final adapter candidate pinned to v0.8.0. Label real kernel SCTP, controlled injection, pure unit tests, and cross-builds separately. Unavailable/not-run/skipped checks are not passes. Use barriers/fault injection to prove race windows and real I/O to prove unblocking/retirement, rather than relying only on sleeps or global goroutine counts.

| Scope | Behavior that must hold again | Existing test entry examples |
| --- | --- | --- |
| 64: takeover/budgets | Verify options and actual inbound/outbound streams; clear SO timeouts and old deadlines; New takes ownership on success and failure; reject insufficient budgets before CER | `TestResourceLedgerAndConstructor`, `TestSCTPNodeReserveRejectedBeforeHandshake`, `TestUpstreamSocketTimeoutTakeover` |
| 64: frame boundaries/fragmentation | 20-byte header and 24-bit length, early/missing EOR, short/coalesced frames, PPID policy, asymmetric inbound/outbound streams; prefix/body/discard without a second full-frame copy | `TestRealSCTPFramesDiscardAndMetadata`, `TestRealSCTPInvalidMessageBoundary`, `TestContinuationInEveryReceiveStage` |
| 64: notifications/deadlines | Continuations in all three stages retain Progress and the same dst; five fixed mappings; close on size/rate overflow; retain no tail and never refresh the absolute FrameRead deadline | `TestNotificationLimitAndTypedTerminal`, `TestFragmentsNeverRefreshDeadlineOrBorrowAfterReturn`, `TestNotificationBoundsAndUpstreamFragmentErrors` |
| 64: Base/S6c | Real CER/CEA, SRR/SRA success/experimental failure/E, DPR/DPA; active/passive CEA/DWR barriers, matching DWA unlock, silent timeout | `TestSCTPNodesBaseS6cSuccessFailureAndShutdown`, `TestSCTPResponderOrderedBarrierAndTimeout` |
| 64: sends/termination | One send, no short-write tail retry; retain actual write evidence with positive counts plus errors or cancellation/Close; late answers after cancellation, dual Close, and association termination each complete once and return budgets correctly | `TestWriteEvidenceAndNoTailRetry`, `TestRealSCTPBlockedWriteCancellationAndClose`, `TestSCTPNodesCancelLateAnswerAndCloseRace`, `TestSCTPNodeAssociationTerminationSettlesPending` |
| 65: establishment/Accept | Combined N/Z/D bounds and owned inputs; reuse listener after cancellation/deadline, reject concurrent callers, close late arrivals, keep old ctx from affecting handed-off connections | `TestSetupInputBoundsAndLedger`, `TestListenerCancelDeadlineReuseAndOneOwner`, `TestListenerCanceledArrivalAndFreshAccept`, `TestManagedSCTPDialServeS6cAndHandoff` |
| 65: Close/Wait | Incoming.Establish occurs once and races safely with Close; Wait retains the slot while blocked Establish has not actually exited | `TestListenerCloseUnblocksAndIncomingCloseRace`, `TestManagedSCTPWaitRetainsEstablishBorrower` |
| 65: admission/deadlines/domains | Reject listener and active/passive candidate budgets short by one byte; idle Accept consumes no Handshake budget; absolute deadlines without CER/CEA; Serve cancellation retains established Peers, Maintain cancellation retires its own domain | `TestManagedSCTPAdmissionAndIdleStops`, `TestManagedSCTPHandshakeDeadlineAndIdleListener`, `TestManagedSCTPDialServeS6cAndHandoff` |
| 65: election/shutdown | Concurrent Dial/Serve election at both endpoints, retirement of losers/old generations, shutdown at both endpoints | `TestManagedSCTPCrossedElectionAndShutdown` |
| 65: backoff/REOPEN | Cancelable failure backoff; SRR written before a real disconnect completes once as WrittenNoAnswer; a new ConnectionID enters REOPEN and needs 3 DWAs before Ready; reject late old Inbound answers, with no pending migration/automatic business replay; an explicit new request succeeds | `TestManagedSCTPFailureBackoffAndCancel`, `TestManagedSCTPReconnectDoesNotReplay` |
| 66: new terminal semantics | Preserve valid RESTART/partial-abort facts in prefix/body/discard; malformed/unconsumed records cannot continue; a writer seeing the restart latch first retains first cause and actual bytes | `TestUpstreamTerminalRecordsPreserveFacts`, `TestUpstreamInvalidTerminalRecordCannotContinue`, `TestUpstreamRestartWriteLatchPreservesDelivery` |
| 66: binding/deadlines | Shorten/extend/clear listener deadlines; BoundAddr copy ownership, actual port, and wildcard semantics; no dependence on 20ms polling | `TestUpstreamListenerDeadlineAndOwnedBind` and upstream A/C regression evidence |

Do not copy upstream continuity algorithms into adapter fakes. Test adapter error propagation and termination, citing the corresponding v0.8.0 upstream E regressions. Injected RESTART/partial-abort projection tests must be labeled controlled evidence, not actual kernel events.

Retain Diameter headers/lengths, whole-frame deadlines, notification rate limits, transport.Event/Sequence/ConnectionID/first cause, CEA/DWR, control stream0, Peer/Watchdog/REOPEN/DPR/DPA, pending/attempt/Node budgets, and exactly-once completion. SEND_FAILED must not guess HBH from payload or interpret sent/unsent as confirmation of business processing. SHUTDOWN is not DPR and must not generate DPA.

## 6. Version switch and command templates

The subsequent diameterkit session executes these steps; this handoff session has not performed the changes or regressions. Run commands from the diameterkit root. First inspect the branch and changes, then record a new acceptance round under its workflow. Do not reset, overwrite other sessions' work, or bulk-upgrade unrelated dependencies.

The adapter also depends on unpublished core candidate `v0.0.0-diameterkit.batch64`. Publication of upstream v0.8.0 does not make that core or the adapter publicly installable. Existing offline core packages may support development acceptance, provided provenance is explicitly separated:

```sh
handoff_artifacts=$(mktemp -d /tmp/diameterkit-sctp-v080.XXXXXX)
python3 spec/tools/core_candidate.py . "$handoff_artifacts/core-proxy"
# Check core-manifest.json: reusing the batch 66 core requires 114 files
# and the original zip SHA256. If sources changed, record the new diff
# and provenance; do not present them as the original core evidence.
export GOWORK=off
export GOMODCACHE="$handoff_artifacts/modcache"
export GOPROXY="file://$handoff_artifacts/core-proxy,https://proxy.golang.org,direct"
export GONOSUMDB=github.com/skill215/diameterkit

go -C adapters/sctp mod edit -require=github.com/skill215/sctp@v0.8.0
go -C adapters/sctp mod tidy
go -C adapters/sctp mod download -json github.com/skill215/sctp@v0.8.0 > "$handoff_artifacts/upstream-download.json"
go -C adapters/sctp list -m -json github.com/skill215/sctp > "$handoff_artifacts/upstream-selected.json"
go -C adapters/sctp mod verify > "$handoff_artifacts/module-verify.log"
git diff -- adapters/sctp/go.mod adapters/sctp/go.sum
```

Old core zip SHA256: `548cad385780d79d7002d110f237ad09e420fde358fa7ab48e5fc8ab56136d54`. Use the local proxy and exact GONOSUMDB exception only for the core candidate. Do not disable upstream SCTP sumdb checks or place local SCTP sources in the proxy. Verify download Version/Origin.Hash/Ref and both sums, and confirm no Replace in `go.mod` or the selected module. Investigate changed core provenance or unrelated mod tidy upgrades before proceeding.

Main regressions (check each exit code; retain logs and fix any failure instead of unconditionally rerunning until green):

```sh
CGO_ENABLED=0 go test -mod=readonly -json -count=1 -timeout=15m ./... > "$handoff_artifacts/core-native.jsonl"
go test -mod=readonly -race -json -count=1 -timeout=15m ./... > "$handoff_artifacts/core-race.jsonl"
CGO_ENABLED=0 go -C adapters/sctp test -mod=readonly -json -count=1 -timeout=15m ./... > "$handoff_artifacts/adapter-native.jsonl"
go -C adapters/sctp test -mod=readonly -race -json -count=1 -timeout=15m ./... > "$handoff_artifacts/adapter-race.jsonl"
go -C adapters/sctp test -mod=readonly -race -json -count=20 -timeout=15m -run '^(TestListener.*|TestUpstream.*|TestManagedSCTP(AdmissionAndIdleStops|CrossedElectionAndShutdown|WaitRetainsEstablishBorrower)|TestSCTPNodesCancelLateAnswerAndCloseRace|TestWriteEvidenceAndNoTailRetry)$' ./... > "$handoff_artifacts/lifecycle-race.jsonl"
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go -C adapters/sctp vet ./...
CGO_ENABLED=0 go -C adapters/sctp build ./...
```

The full suite must include the real long-running REOPEN test; do not exclude it from final acceptance to accelerate repeated runs. The 20-round command above selects a bounded race set and is not equivalent to batch 66's 70 tests ×20. Root-module `go test ./...` excludes the nested adapter module; execute both separately. Retain core architecture/generated-file freshness checks and production dependency checks for no CgoFiles. If reusing historical race evidence for unchanged core sources, explicitly record the source SHA/file inventory and “not rerun this round”; do not include it in this round's pass count.

Run the existing Windows/amd64, Linux/386, and Linux/arm64 build matrix, recording it separately as build evidence. Unsupported paths on the first two do not establish SCTP support; Linux/arm64 cross-building is not native network validation. An independent temporary upstream consumer should require only v0.8.0, use public proxies, and rerun downloaded examples with CGO0 and race, verifying upstream provenance without the offline core.

Historical batch 66 counts are core CGO0 2603, adapter CGO0/race 71 each, and 70 tests ×20=1400 excluding long REOPEN. These are neither this round's results nor fixed targets to reproduce artificially. Parse current JSON output and count actual tests/subtests/seeds; list failures, skips, and unrun checks with reasons. Count packages without tests separately.

## 7. Deliverables and completion criteria

- Pin exact v0.8.0 with actual go.sum/Origin/dependency-graph evidence and no replace; report offline-core provenance separately.
- Provide an A–E replacement review distinguishing previously completed work from this round's fixes. Confirm production code has no Accept polling, direct socket syscalls, duplicated N/Z/D or continuity algorithms, or old 8192 formula.
- Record actual LedgerVersion/query results, adapter composition, Node configuration, and peak concurrent candidates. Supply evidence for one-byte-short rejection and actual retirement.
- Record results for the batch 64/65 matrix and batch 66 terminal-event fixes against the final v0.8.0 dependency, separating native/race/bounded repetition/vet/build/cross-target evidence.
- Update current README/contracts and new batch machine evidence while retaining historical batches. Do not continue labeling v0.8.0 as “planned only/unmerged.” Record commands, toolchain/kernel, candidate SHA, source/log hashes, and all skips.
- End with “official upstream integration and regression acceptance complete.” Do not equate this with official publication of diameterkit core/adapter, real DRA/OCS interoperability, OrderedMultiStream/PPID0 modes, Ro/SCAP1, or completion of the whole v1 effort.

Ready-to-use task for the next session: **First verify the completed batch 66 migration and preserve existing work. Pin the independent adapter from the bb739b6 pseudo-version to official sctp v0.8.0; review A–E APIs and resource composition; rerun the batch 64/65 and migration regressions in this document. Record provenance, go.sum, tests, and budget evidence for the same final candidate, then conclude this round.**
