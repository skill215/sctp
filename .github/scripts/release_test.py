#!/usr/bin/env python3
"""Exercise publication policy with local Git repositories and offline CLI mocks."""
import os
from pathlib import Path
import subprocess
import tempfile
import textwrap

repo = Path(__file__).resolve().parents[2]
workflow = (repo / ".github/workflows/release.yaml").read_text()
marker = "          set -euo pipefail\n"
assert workflow.count(marker) == 1, "review publication script extraction"
script = textwrap.dedent(marker + workflow.split(marker, 1)[1].split("\n  notes:", 1)[0])


def check_case(case):
    with tempfile.TemporaryDirectory(prefix="sctp-release-test-") as directory:
        root = Path(directory)
        work = root / "work"
        work.mkdir()

        def git(*args):
            return subprocess.check_output(["git", *args], cwd=work, stderr=subprocess.PIPE, text=True).strip()

        subprocess.run(["git", "init", "--bare", "-q", str(root / "remote.git")], check=True)
        git("init", "-q", "-b", "master")
        git("config", "user.name", "Release test")
        git("config", "user.email", "release-test@example.com")
        git("remote", "add", "origin", str(root / "remote.git"))
        (work / "README").write_text("fixture\n")
        git("add", ".")
        git("commit", "-qm", "initial")
        initial = git("rev-parse", "HEAD")
        notes = work / "releases/v0.7.4.md"
        notes.parent.mkdir()
        if case in {"pending", "released"}:
            notes.write_text("original notes\n")
            git("add", ".")
            git("commit", "-qm", "prepare notes")
        before = git("rev-parse", "HEAD")
        if case == "released":
            git("tag", "-a", "v0.7.4", "-m", "original immutable tag")
        elif case == "collision":
            git("tag", "-a", "v0.7.4", initial, "-m", "pre-existing tag")
        old_tag = git("rev-parse", "refs/tags/v0.7.4") if case in {"released", "collision"} else None
        notes.write_text("validated candidate notes\n")
        git("add", ".")
        git("commit", "-qm", "candidate")
        head = git("rev-parse", "HEAD")
        bindir = root / "bin"
        bindir.mkdir()
        (bindir / "gh").write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$GH_CALLS"\nprintf "{}\\n"\n')
        (bindir / "go").write_text('#!/bin/sh\nif [ "$1" = list ]; then printf "v0.7.4\\n"; fi\n')
        for path in bindir.iterdir():
            path.chmod(0o755)
        env = dict(os.environ, PATH=str(bindir) + os.pathsep + os.environ["PATH"],
                   GITHUB_REF_TYPE="branch", GITHUB_REF_NAME="master", GITHUB_SHA=head,
                   BEFORE_SHA=before, RUNNER_TEMP=str(root), TMPDIR=str(root),
                   GITHUB_REPOSITORY="fixture/sctp", GH_CALLS=str(root / "gh-calls"))
        result = subprocess.run(["bash", "-c", script], cwd=work, env=env,
                                capture_output=True, text=True, timeout=30)
        if case == "collision":
            assert result.returncode != 0, "new notes must not reuse an existing tag"
        else:
            assert result.returncode == 0, result.stdout + result.stderr
        if old_tag:
            assert git("rev-parse", "refs/tags/v0.7.4") == old_tag, "existing tag moved"
            assert not (root / "gh-calls").exists(), "existing release was republished"
        else:
            found = subprocess.run(["git", "show-ref", "--verify", "refs/tags/v0.7.4"], cwd=work, capture_output=True)
            assert found.returncode == 0, f"{case}: untagged candidate was not published"
            assert git("cat-file", "-t", "refs/tags/v0.7.4") == "tag"
            assert git("rev-parse", "refs/tags/v0.7.4^{commit}") == head
            assert (root / "gh-calls").read_text().count("release create v0.7.4 ") == 1
        print(f"publication policy {case}: PASS", flush=True)


for case in ["new", "pending", "released", "collision"]:
    check_case(case)
