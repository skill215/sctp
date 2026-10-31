# Regression mapping

Baseline: 0.7.1 / e864ee06b6c5b0c77661c4715814a75576c23237;
master 70b338ac419dff6aa745460bb379d68208d615e0 has identical library code.
Tests assert corrected behavior, not the audit's expected broken observations.

| Issue | Production regression | Baseline failure / independent control |
| --- | --- | --- |
| #1 | TestStabilityReadAfterConsumedReset, TestStabilityPeeledConsumedReset | All six read/consumer combinations and peeled reads keep waiting; raw status/kernel controls and live endpoint tests isolate termination. |
| #2 | TestStabilityAddressInputs, TestStabilityAddressFailureSnapshots | Shared wildcard input and spare backing storage mutate/race; independent inputs and explicit IP snapshots are controls. |
| #4 | TestStabilityEmptyReadsPreserveInput | Empty SCTPRead returns 1 and consumes a byte; following marker read must remain complete. |
| #5 | TestStabilityEmptySends | Metadata empty send returns 0,nil and wrapper reports 32,nil; raw zero send rejects with EINVAL and following marker must have no prefix. |
| #6 | TestStabilitySendControls, TestStabilityControlCombinations | EOF is 0x10 rather than 0x200; applicable one-to-many EOF and raw control combinations verify actual shutdown. |
| #7 | TestStabilityInvalidEndpointInputs | Out-of-range ports reach Control or kernel rather than returning AddrError; legal ephemeral ports remain functional. |
| #8 | TestStabilityWrappingPreservesEvents | Wrapping replaces SHUTDOWN with DATA_IO; handler delivery/reentrant configuration and concurrent wrapping are controls. |
| #9 | TestStabilityNotificationHeader | SENDER_DRY without metadata leaves the previous header; normal Stream=1/PPID=42 payload establishes the reused buffer first. |
| #10 | TestStabilityInvalidEndpointInputs | Nil remote addresses panic instead of returning AddrError before Control; nil local address with valid peer remains supported. |
| #11 | TestStabilityMultihomingEmptyHosts | Empty multi-host segments are accepted and explicit prefixes discarded; standalone wildcard and explicit IPv4/IPv6 forms remain valid. |
| #12 | TestStabilityResetReadCounts, TestStabilityWrappedTerminalCounts | Real reset returns -1 and io.ReadAll panics; normal EOF/timeout/Close and valid partial frames retain legal counts. |
| #14 | TestStabilityLargeAddressQuery, TestAddressQueryGrowth, TestAddressQueryBudgetAndErrors | 300-address listener binds correctly but Addr=nil; raw 16 KiB query independently returns 300 addresses. |
| #15 | sctp_portability_test.go, .github/scripts/consumer.py | Missing ErrUnsupported on Linux/amd64 and Listener.SyscallConn on stub targets break independent consumers; five targets now compile on both Go versions. |
| #16 | TestUnsupportedOwnsSocket, TestUnsupportedOwnsFDZero, TestUnsupportedCleanupError, TestUnsupportedOwnsWinsock | Baseline ownership tests fail after isolating #15's compile failure; real SCTP/WinSock resources, exact Unix fd reuse and native close errors are checked. |

#13 is a separately implemented P3 improvement: TestStabilityAcceptErrorNil.
The old interface contained a typed nil; the new error result is a nil interface.

Integration review found and fixed the post-recv/status graceful-shutdown race.
TestStabilityShutdownRacesStatusQuery checks 50 actual concurrent drains per run.
