# v0.7.4 candidate review

Date: 2026-11-06. Baseline: v0.7.3 / e4154064. This review covers the local
#19–#24 fixes, their regression tests, and release preparation.

No additional actionable regression was found in this review. Descriptor
duplication remains inside a pinned source-file Control callback, atomically
sets CLOEXEC, and transfers only the duplicate into listener ownership. INITMSG
validation occurs before conversion or kernel mutation. Association validation
preserves signed 32-bit/default values and rejects both high-bit aliases before
queries or peeloff. Validation remains behind the existing fd entry point so
unsupported-target and closed-owner errors retain their established precedence.

The one-to-many fixture uses actual peer data and metadata for readiness,
checks an immediately successful connect's association ID, restores temporary
events/timeouts, and joins its signal goroutine. Source-close tests join their
reuse worker; CLI tests reap child processes and clean up client connections.
Example readbacks no longer mutate requested flags, and invalid address lists
fail before listening.

The submission-time targeted race check passed five repetitions across fd
imports, numeric boundaries, interrupted setup, peeloff/CLOEXEC and CLI server
regressions. Earlier complete local evidence remains in
[fix validation](fix-validation-2026-11-06.md) and
[architecture review](architecture-review-2026-11-06.md). Their unpublished/local
status fields describe those earlier snapshots; remote publication status is
determined by the candidate PR, release workflow and immutable tag.

The next patch version is v0.7.4. A PR must pass the existing complete native
and QEMU matrix before merge. Adding releases/v0.7.4.md then invokes the release
workflow on master, which repeats validation for the merged commit and permits
publication only after all required validation jobs succeed. No gate is removed
or weakened by this change. The resulting annotated tag and downloaded Go
module must point to that validated release commit.
