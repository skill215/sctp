# Fix validation evidence, 2026-11-06

The baseline is v0.7.3/e4154064. The candidate is an unpublished local working
tree with fixes for #19-#24. Source hashes are in source-manifest.json.

checks.json contains 26 final passing commands: five full race/shuffle processes
per toolchain, focused regressions count=3, vet, strict checkptr, low-GC race and
four fuzz targets per toolchain. architecture.json contains 12 final test-binary
compilations for six Linux architectures on two Go versions. These binaries were
not executed in QEMU. compatibility.json contains eight passing checks after the
core fixes: Linux/386 native stubs, required namespace tests count=3, synthetic
consumer compilation and README execution per toolchain. Later changes affected
only Linux test fixtures. The consumer fixture imports only this library and the
standard library; it is not a business repository.

before-core.log and before-example.log demonstrate the pre-fix regressions.
The original pre-fixture low-GC failure and its command record are retained;
final/checks results have not overwritten or relabeled that failure. The signal
probe summary and source demonstrate the independent kernel control behind #24.
The targeted fixture-after-focused run passed count=3 under GOGC=1 and -race.
The 100-count non-signal peeloff retry also passed, before the fixture repair.

The public association demonstration shows the old behavior and is expected to
stop at EINVAL with the fixed candidate. Run it against v0.7.3 to reproduce #23.
Permanent regression tests in the source tree assert the corrected behavior.
Issue drafts are retained for review; the GitHub issues were created and read
back. Issues remain open because no commit or published release is claimed.

Use your own paths for ${REPO_ROOT}, ${REVIEW_DIR}, ${GO_1_27_TOOLCHAIN} and
${GOCACHE} in commands. Run command arrays from the repository root. The Python
runner text is retained to show orchestration; checks.json is the authoritative
record of each command and environment. Local paths/private addresses have been
redacted in these artifacts. log-manifest.json hashes the redacted stored files.
