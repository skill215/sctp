# v0.8.0 release completed

On 2026-11-09, validation, merge, the annotated tag, and the formal Release were completed as authorized, followed by independent acceptance of the formal module.

- **[Formal Release v0.8.0](https://github.com/skill215/sctp/releases/tag/v0.8.0)** (neither Draft nor prerelease; GitHub latest).
- Merged [PR #30](https://github.com/skill215/sctp/pull/30), final PR head `ca6239818ba8b0abc0b2bbaa988e44991f61ff3f`.
- Release commit: `bce1b3cf3ea32a32a86262c7f655d565bb40055c`; annotated tag object: `5b6374817a72dd51e630032ab1b64b1791dba6ed`.
- [Complete acceptance record and formal go.sum](https://github.com/skill215/sctp/issues/31#issuecomment-6735592144).
- [Executed release plan](https://github.com/skill215/sctp/blob/v0.8.0/docs/release-plan-v0.8.0.md), [formal notes](https://github.com/skill215/sctp/blob/v0.8.0/releases/v0.8.0.md), and [API/resource contracts](https://github.com/skill215/sctp/blob/v0.8.0/docs/bounded-setup.md). The repository plan preserves the release-candidate snapshot; this issue records the final completed status.

## Release acceptance checklist

- [x] Check the version, candidate, scope, and compatibility risks; select v0.8.0.
- [x] Commit formal Release Notes and delivery documentation; all 70 Go source files remain unchanged.
- [x] [Final PR CI](https://github.com/skill215/sctp/actions/runs/42583623648): 14/14 jobs successful; merge with no blocking review.
- [x] The merged Git tree matches the validated PR candidate.
- [x] [Release workflow on the merge commit](https://github.com/skill215/sctp/actions/runs/42584877736): 16/16 jobs successful, including 14 validation jobs, publish, and notes.
- [x] The annotated tag points to the validated merge SHA; all 11 historical tags are unchanged; tag annotation and Release body match the committed notes.
- [x] Download formal v0.8.0 in an independent new module with a fresh cache, GOWORK=off, and no replace; source SHA and all 70 source hashes match.
- [x] Formal-module native/race SCTP examples and go mod verify pass; `@latest` resolves to v0.8.0.
- [x] Record version, SHA, workflows, skips, go.sum, and consumer evidence on GitHub; close upstream release tracking.

Each matrix completed 20 million fuzz executions and 40 race rounds. The 64-bit-int-only ARM/MIPS test produced six explicit skips per matrix; they are recorded separately and excluded from pass counts. QEMU checks ABI and do not expand platform support.

```
github.com/skill215/sctp v0.8.0 h1:7N92RMEO7s4+4IHzI8KYKUgyIPpIClIVAdgg93m+RFI=
github.com/skill215/sctp v0.8.0/go.mod h1:g/XAScdNEAqQa2cjZdtxXVkxIJZEy77f7/fmCkTD2gg=
```

## Downstream handoff (separate task)

A later diameterkit session will pin v0.8.0, migrate the thin adapter, and rerun batch 64/65 acceptance. This release did not modify or upgrade diameterkit and does not claim its migration passed; that does not block the completed upstream release.

If a library defect is discovered after publication, retain the existing tag and evidence and fix it in a new patch version. Do not move or reuse v0.8.0.
