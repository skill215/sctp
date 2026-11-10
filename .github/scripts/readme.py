#!/usr/bin/env python3
"""Build README code and exercise the v0.8.0 examples against this checkout."""
import argparse
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import tempfile
import time

repo = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--race", action="store_true", help="build and run all examples with the Go race detector")
args = parser.parse_args()
blocks = re.findall(r"```go\n(.*?)\n```", (repo / "README.md").read_text(), re.S)
assert len(blocks) == 1, "review README extraction after adding/removing snippets"
env = dict(os.environ, GOWORK="off", CGO_ENABLED="1" if args.race else "0")
build_flags = ["-race"] if args.race else []
with tempfile.TemporaryDirectory(prefix="sctp-readme-") as directory:
    root = Path(directory)
    # Development validation intentionally uses this checkout. Publication
    # verification must instead download the exact release without replace.
    (root / "go.mod").write_text(
        "module example.com/sctp-readme\n\ngo 1.26.0\n"
        "require github.com/skill215/sctp v0.8.0\n"
        "replace github.com/skill215/sctp => " + json.dumps(str(repo)) + "\n"
    )
    server_binary = root / "message.bin"
    subprocess.run(
        ["go", "build", *build_flags, "-buildvcs=false", "-o", str(server_binary), "./example/message"],
        cwd=repo, env=env, check=True, timeout=180,
    )
    with (root / "server.log").open("w+") as log:
        # Bind port zero once and obtain the actual owned BoundAddr from the log.
        # Avoid reserving/releasing a port and racing a second bind.
        server = subprocess.Popen(
            [str(server_binary), "-server", "-addr", "127.0.0.1:0", "-chunk", "4"],
            stdout=log, stderr=log, env=env,
        )
        try:
            for _ in range(200):
                log.seek(0)
                output = log.read()
                match = re.search(r"listening on (127\.0\.0\.1:\d+)", output)
                if match:
                    address = match.group(1)
                    break
                if server.poll() is not None:
                    raise RuntimeError("message server exited before listening:\n" + output)
                time.sleep(0.05)
            else:
                raise RuntimeError("message server failed to become ready:\n" + output)

            sources = {
                "client": blocks[0].replace("127.0.0.1:9000", address),
                "bounded": (repo / "docs/examples/bounded-consumer/main.go.txt").read_text(),
            }
            for name, source in sources.items():
                path = root / name
                path.mkdir()
                (path / "main.go").write_text(source)
                subprocess.run(
                    ["go", "build", *build_flags, "-buildvcs=false", "-o", str(root / (name + ".bin")), "./" + name],
                    cwd=root, env=env, check=True, timeout=180,
                )
            client = subprocess.run(
                [str(root / "client.bin")], capture_output=True, text=True,
                env=env, check=True, timeout=15,
            )
            assert 'payload="hello SCTP" stream=0 ppid=42 eor=true' in client.stdout, client.stdout
            for ppid in [0, 46]:
                client = subprocess.run(
                    [str(server_binary), "-addr", address, "-stream", "1",
                     "-ppid", str(ppid), "-chunk", "4", "-count", "3"],
                    capture_output=True, text=True, env=env, check=True, timeout=15,
                )
                expected = f'payload="hello SCTP" stream=1 ppid={ppid} eor=true'
                assert client.stdout.count(expected) == 3, client.stdout
            # A malformed or oversized application message is closed, never echoed
            # as multiple records. The next client must still be accepted.
            oversized = subprocess.run(
                [str(server_binary), "-addr", address, "-message", "x" * (64 * 1024 + 1),
                 "-max-message", str(128 * 1024)],
                capture_output=True, text=True, env=env, timeout=15,
            )
            assert oversized.returncode != 0 and "eor=true" not in oversized.stdout, oversized.stdout
            log.seek(0)
            assert "message exceeds 65536-byte application limit" in log.read(), oversized.stderr
            client = subprocess.run(
                [str(root / "client.bin")], capture_output=True, text=True,
                env=env, check=True, timeout=15,
            )
            assert 'payload="hello SCTP"' in client.stdout, client.stdout

            bounded = subprocess.run(
                [str(root / "bounded.bin")], capture_output=True, text=True,
                env=env, check=True, timeout=15,
            )
            assert "cancellation and close: PASS" in bounded.stdout, bounded.stdout
            server.send_signal(signal.SIGINT)
            assert server.wait(timeout=10) == 0, "idle accept did not cancel cleanly"
            print("README client: payload/stream/PPID/EOR verified")
            print("Message CLI: fragmented stream1 PPID0/46, bounded rejection, listener reuse, SIGINT: PASS")
            print(bounded.stdout.strip())
        except Exception:
            log.seek(0)
            print(log.read(), flush=True)
            raise
        finally:
            if server.poll() is None:
                server.terminate()
                server.wait(timeout=10)
