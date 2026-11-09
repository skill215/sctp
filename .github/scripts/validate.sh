#!/usr/bin/env bash
set -euo pipefail
uname -a
go version
python3 .github/scripts/release_test.py
# Production mode is independently exercised, not inferred from race builds.
CGO_ENABLED=0 go test -count=1 -timeout=120s ./...
# Independent processes give each round its own 120-second budget.
for round in $(seq 1 10); do
  seed=$((7272000 + round))
  echo "race round=$round seed=$seed"
  go test -race -shuffle="$seed" -count=1 -timeout=120s ./...
done
go vet ./...
go test -gcflags=all=-d=checkptr=2 -count=1 -timeout=120s ./...
GOGC=1 go test -race -count=1 -timeout=120s ./...
for target in RawSockaddr Ancillary Codec AddressRoundTrip ParseNotification; do
  # Execution counts avoid treating the duration coordinator's deadline as a
  # target failure. Every target must finish the full budget; no retries or
  # error filtering. The outer timeout still fails stalled runs.
  echo "fuzz target=$target executions=1000000 minimize=100x"
  go test -run '^$' -fuzz="^Fuzz${target}$" -fuzztime=1000000x -fuzzminimizetime=100x -parallel=2 -timeout=180s .
done
