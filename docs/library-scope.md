# Library scope and release boundary

This repository implements Go bindings for Linux kernel SCTP sockets. Changes,
bug reports and release acceptance must be evaluated against this library's
public API and documented socket behavior.

## Responsibilities

| Area | Owned here |
| --- | --- |
| Socket lifecycle | Listen, Dial, Accept, Close, descriptor ownership, FileListener and PeelOff |
| Message I/O | Read/Write, SCTPRead/SCTPWrite, streams, PPID and ancillary metadata |
| Association state | Kernel notifications, event subscriptions, status and peer/address queries |
| Addressing | IPv4/IPv6, multihoming, interface zones and bind/connect encoding |
| Waiting and errors | Runtime poller integration, relative socket timeouts, close wakeups and syscall errors |
| ABI | Structure sizes, alignment, native byte order and supported Linux architectures |
| Delivery | Package documentation, examples, module compatibility, CI, tags and releases |

Application startup/shutdown, business queues and caches, RPC/HTTP servers,
upper-layer message parsing, capability exchange, heartbeats and reconnect
scheduling belong to their respective applications or adapters. Their repairs,
deployments and releases are outside this repository's work and release gates.

An upper-layer failure is a lead for investigation. Before classifying it as a
library defect, reproduce it with a focused test using this package and, where
needed, an independent kernel/syscall control. Adapter failures alone do not
justify changing the SCTP implementation or publishing a library patch release.

## Existing API limits

The minimum Go version remains 1.26. Linux with kernel SCTP is the supported
platform; Linux/386 is excluded. Existing unsupported-target stubs and cleanup
checks are compatibility safeguards, not additional supported platforms.
Solaris/illumos gaps remain documented and deferred.

Standard SetDeadline/SetReadDeadline/SetWriteDeadline currently return
EOPNOTSUPP. Relative SO_RCVTIMEO/SO_SNDTIMEO semantics are documented in the
README. DialContext, standard deadlines and additional message/notification APIs
require separate feature design; application workarounds must not silently
redefine these contracts.

## Library release acceptance

1. Each included defect has a library-level before/after regression, with review
   of adjacent descriptor, concurrency, timeout and error behavior.
2. The existing native matrix passes on Go 1.26.x/1.27.x and two Linux kernels:
   ten independent race/shuffle processes, vet, strict checkptr, low-GOGC tests,
   the four fuzz targets and required namespace regressions.
3. Architecture checks exercise the affected ABI/encoding paths. QEMU checks
   are not represented as full SCTP association validation on those targets.
4. Examples and independent API compilation pass. The synthetic consumer in
   `.github/scripts/consumer.py` imports only this package and standard-library
   APIs; it does not check out or build a business repository.
5. The final candidate is reviewed, documented and published through the
   existing validated release workflow, with immutable tags and module-download
   verification.

Application acceptance, adapter publication permissions and consumer dependency
upgrades are not SCTP library release gates. No new release is warranted solely
because an external application's code changed. This scope document does not
claim that a fresh release candidate has passed the full matrix.

## Scan on 2026-11-03

The module declares no external dependencies. Package, example and test import
graphs contain only this module and the Go standard library. The scan found no
business framework implementation in the Go source or tests. Previously added
consumer-specific plans, patches, logs and the separate consumer workflow were
removed from this working tree and preserved outside the repository.

The library implementation, regression tests and release workflow were not
changed by this scope cleanup. Future work in this repository follows the
responsibility and acceptance boundaries above.
