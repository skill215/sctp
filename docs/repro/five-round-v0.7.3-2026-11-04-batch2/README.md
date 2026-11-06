# Second five-round review evidence

These records cover a fresh review of v0.7.3 on Linux/amd64 using Go 1.26.10
and 1.27.3. See results.json for scope and checks.json for every final command.
Commands run from the SCTP repository root. Supply your own paths for variables
such as ${GO_1_27_TOOLCHAIN}, ${GOCACHE} and ${REVIEW_DIR}.

The final round 4 and 5 records use distinct verified-* logs from serial
verification. The earlier RawSockaddr deadline failure is retained separately
in initial-fuzz-timeout.json and its log; it has not been converted to a pass.
The six raw-repeats sessions used default minimization and 20-second fuzz budgets.
The two raw-minimize sessions used 30 seconds and -fuzzminimizetime=1s.
Neither group wrote a crash input. The latter sessions resumed sustained
execution; the original termination's root cause remains unclassified.

extra-probes.json runs the temporary Go probes three times per toolchain under
-race. To rerun them without modifying the repository, copy
extra_probes_test.go.txt to an external ${REVIEW_DIR}/extra_probes_test.go and
write an overlay JSON whose Replace map points from the absolute repository
path review_batch2_test.go to that copied file. Then run:

```sh
go test -race -overlay "${REVIEW_DIR}/overlay.json" -run '^TestReview' -v -count=3 -timeout=60s .
```

The probes assert preservation of each of the four extended kernel event bytes
through two wrapper constructions; Read, SCTPRead and wrapped Read timeout
changes and clearing; and Accept after clearing a prior socket timeout.
They pass on the tested baseline and do not implement a bug fix.

Local paths and private addresses are redacted in retained evidence.
log-manifest.json hashes the redacted files stored here.
