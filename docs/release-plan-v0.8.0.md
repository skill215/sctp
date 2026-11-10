# v0.8.0 Release Plan

Prepared: 2026-11-09. Historical release-candidate snapshot: execution was authorized; see [GitHub #31](https://github.com/skill215/sctp/issues/31) and the [publication record](evidence/v0.8.0/publication-result.md) for the completed release.
GitHub tracker: [#31](https://github.com/skill215/sctp/issues/31).
On 2026-11-09, the user authorized merging, tagging, and publication after validation. The tracker retains final SHAs, workflows, module checksums, and consumer evidence.

## Version and baseline

- The latest official remote Release has been verified as [v0.7.4](https://github.com/skill215/sctp/releases/tag/v0.7.4); no v0.8 tag exists at this stage.
- Recommended target: **v0.8.0**. Relative to v0.7.4, it adds the complete U2–U5 and A–E capabilities, including standard deadline behavior and the SocketConfig.Messages field. The v0.7.2 plan also identified Context/deadline/message/notification support as future feature work; that does not require completing all other items in that older plan.
- Candidate: PR #30, head `bb739b612088d46bc16025c361ee49f9c58ed1a2`, base master `62ad388be958f0bd19d29d63dfc05743726779a1`. The PR is currently open/draft.
- Downloadable candidate pseudo-version: `v0.7.5-0.20261109072213-bb739b612088`. A pseudo-version derives from existing tags and commit position; it does not require the official release to be v0.7.5. No additional v0.7.5 release is planned.

## Release scope and compatibility

Scope is frozen to PR #30: opt-in message I/O, five typed notifications, standard connection deadlines and Context-aware resolution/dialing, plus A–E: AcceptContext, bounded establishment APIs, socket timeouts/bound-address snapshots, resource queries, message continuity, and negotiated stream boundaries.

Release Notes must explicitly state:

1. Relative to official v0.7.4, SocketConfig adds the Messages field; existing unkeyed literals must become keyed literals. The statement that “A–E added no fields to existing structures” applies only to the U2–U5 candidate baseline, not to the entire release.
2. nil Messages preserves the legacy path. Non-nil mode rejects conflicting callbacks, legacy I/O, or option changes. Missing/inconsistent metadata, out-of-range streams, and restart events are rejected or require closure according to the contract.
3. Standard deadlines take effect on supported platforms. SetSocketTimeouts independently controls SO timeouts; a partial failure requires closing the connection whose ownership is being transferred.
4. Go >=1.26, production CGO_ENABLED=0, and Linux kernel SCTP are required (excluding Linux/386). Stub/ABI checks on other platforms do not expand the support commitment.
5. Accounting version: bounded-setup-v2/linux/<GOARCH>. For N16/Z256/D4096 on Go1.26.10/Linux amd64: Dialer=8960, Listener=48524, Establish=78380, Connection=9516; candidate=87340, listener+old connection+candidate=145380. Query the actual runtime platform. These are not RSS guarantees; add caller buffers and business budgets.

Excluded: the v1 rewrite, new platforms, dynamic stream reset, automatic reconnect/replay, the Diameter protocol, and diameterkit production migration.

## Current evidence

| Item | Result |
| --- | --- |
| Review | Fixed one P2: after fragmentation, a successful peek followed by a zero-progress terminal consume error omitted the close requirement; no remaining blockers |
| Full local native / race | 352 pass events each, 0 fail/skip |
| Lifecycle and continuity race ×20 | 660 pass events, 0 fail/skip |
| Remote candidate CI | [run 42579273839](https://github.com/skill215/sctp/actions/runs/42579273839), 14/14 jobs successful |
| Native matrix | Two Ubuntu versions × two Go versions; each job runs 10 race rounds, checkptr, low GC, 5×1 million fuzz executions, mandatory namespace tests, README/consumer checks |
| Architecture and support boundaries | 6 QEMU targets + Linux/386/Windows on two Go versions; 6 explicit skips in total for 64-bit-int-only tests on ARM/MIPS |
| Exact remote module consumption | GOWORK=off, no replace, fresh cache; native and race SCTP examples passed, all 70 source hashes matched, go mod verify passed |

Candidate go.sum (do not copy these as the official version's checksums):
```
github.com/skill215/sctp v0.7.5-0.20261109072213-bb739b612088 h1:3TLlV+PUP7wbICn6l80EtGodrDiK31rIqHV3q28Ri8w=
github.com/skill215/sctp v0.7.5-0.20261109072213-bb739b612088/go.mod h1:g/XAScdNEAqQa2cjZdtxXVkxIJZEy77f7/fmCkTD2gg=
```

Evidence: [candidate consumption record](https://github.com/skill215/sctp/pull/30#issuecomment-6734645807), [final CI record](https://github.com/skill215/sctp/pull/30#issuecomment-6734763054).

## Execution sequence and gates

- [x] Verify the existing official version, candidate, scope, compatibility risks, and workflow.
- [x] Prepare draft Release Notes and record the plan on GitHub.
- [x] Receive publication instructions and recheck PR/head/base and remote v0.8.0 tag/release state. If the baseline changed, review the diff and update candidate evidence.
- [x] Add the official `releases/v0.8.0.md` to the PR #30 branch using the reviewed draft. Update delivery documents with candidate/release status and links, and record this plan. Protect uncommitted work and v1 files in the original workspace.
- [ ] Complete CI on the final PR head including the new notes. Even for documentation-only changes, use checks for that head. Source changes require corresponding regression checks; results from an older SHA cannot substitute.
- [ ] Mark the PR ready; merge into master after confirming no blocking reviews and successful latest checks. Record the actual merge SHA.
- [ ] The notes change on master triggers the existing release workflow, which reruns the full validation matrix at that merge SHA. After all checks pass, the publish job creates an annotated v0.8.0 tag at that SHA, creates the GitHub Release, and checks exact-version module retrieval.
- [ ] Wait for the entire release workflow, including the notes job, to succeed. Verify that the tag object is annotated, its dereferenced commit matches the validated merge SHA, and the Release body is complete. The mutable target_commitish field alone is insufficient.
- [ ] Download `github.com/skill215/sctp@v0.8.0` in an independent new module with GOWORK=off, no replace, and a fresh cache. Record the actual version, source SHA, and module/go.mod sums. Run native/race consumers and go mod verify; compare released sources with candidate Go files. Verify the actual @latest result.
- [ ] Complete the GitHub record with PR/merge SHA/tag/Release URL, release workflow, skips, official go.sum, and consumer results; then close the release tracker.
- [ ] In a separate diameterkit session, pin the official version, migrate the thin adapter, and complete batch 64/65 acceptance. This is a downstream handoff and cannot be counted as completed upstream validation.

### Workflow considerations

The inspected `.github/workflows/release.yaml` validates and publishes when `releases/*.md` changes on master. This release candidate includes the official notes, so merging starts the publication process. Execution follows notes→merge→validate→tag/release; no preliminary manual tag is needed, and the official tag must not be placed on an earlier candidate SHA.

The existing workflow already prevents overwriting old tags and verifies annotated tag type and target SHA. This plan requires no publication mechanism rewrite. The notes job synchronizes existing Release bodies, so the release PR must not include incidental edits to historical releases.

### Failure handling

- If PR or post-merge validation fails, stop publication, retain the failure record, fix the cause, and validate the new SHA. Do not rerun repeatedly until green while hiding failures.
- Before tag creation, unpublished notes/code may be corrected and revalidated through the existing pending-note recovery process.
- If the tag exists or the Release is partially complete, first inspect actual remote tag, Release, and workflow state. Do not move/delete published tags or blindly rerun non-idempotent release-create operations. Recover only missing records, Release creation, or validation steps. Library fixes require a new patch version.
- Consumers encountering regressions may revert to their previously pinned dependency. Subsequent official fixes use a new version; v0.8.0 is never reused.

## Definition of done

Official publication requires evidence for all upstream release steps above; final status is recorded in GitHub #31. diameterkit migration is a separate downstream task and is not a condition for completing the upstream release.
