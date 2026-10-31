#!/usr/bin/env bash
set -euo pipefail
uname -a
go version
# Independent processes give each round its own 120-second budget.
for round in $(seq 1 10); do
  seed=$((7272000 + round))
  echo "race round=$round seed=$seed"
  go test -race -shuffle="$seed" -count=1 -timeout=120s ./...
done
go vet ./...
go test -gcflags=all=-d=checkptr=2 -count=1 -timeout=120s ./...
GOGC=1 go test -race -count=1 -timeout=120s ./...
for target in RawSockaddr Ancillary Codec AddressRoundTrip; do
  go test -run '^$' -fuzz="^Fuzz${target}$" -fuzztime=10s -parallel=2 -timeout=60s .
done
