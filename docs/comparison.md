# Go SCTP library comparison

Comparison date: 2026-10-29.

**Historical snapshot:** the comparisons below describe that date, including
then-missing APIs. As of v0.8.0, this fork supports standard deadlines, context
setup/accept, and explicit message receive flags and typed notifications. For
current capabilities and upgrade constraints, use the [README](../README.md)
and [compatibility guide](compatibility-v0.4-v0.8.0.md). This note does not
reassess the other libraries or claim identical behavior to the upstream API.

`skill215/sctp` suits projects that need to retain the `ishidawataru/sctp` calling model, use native Linux SCTP, and accept a Go 1.26 minimum. This fork's main strengths are low-level correctness fixes and regression validation; its high-level networking API is still less complete than `georgeyanev/go-sctp`. For new projects without compatibility constraints, evaluate the latter in parallel; for WebRTC DataChannels, prefer evaluating the Pion ecosystem.

## Implementation models and use cases

These libraries fall into two categories:

- **Kernel SCTP wrappers**: this library, `ishidawataru/sctp`, `free5gc/sctp`, and `georgeyanev/go-sctp`. Go calls Linux socket APIs, while the kernel handles retransmission, congestion control, and path management. Applications and deployment environments need kernel SCTP support.
- **Userspace SCTP stack**: `pion/sctp`. Protocol logic is implemented in Go over a caller-provided `NetConn`, mainly for WebRTC. Higher layers normally supply DTLS in WebRTC; `pion/sctp` itself does not provide all of ICE and DTLS and cannot directly replace this library's Linux SCTP socket API. [Pion configuration source](https://github.com/pion/sctp/blob/6ec762ebc1106ae8c5ce1a69208a98dfcfdddaaf/association.go#L407-L444)

“No cgo” and “implements its own protocol stack” are separate properties. This library uses no cgo but still relies on the Linux kernel for SCTP. First decide whether the application needs native SCTP sockets or a userspace stack over an application-provided transport.

| Library | Implementation and role | Main strengths | Main limits |
| --- | --- | --- | --- |
| `skill215/sctp` | Linux kernel wrapper retaining the main upstream API | fd lifecycle, poller, timeout, and ABI regressions; no external Go module dependencies | Linux only, excluding Linux/386; lacks standard deadlines, Context dialing, and receive flags |
| `ishidawataru/sctp` | This library's upstream; Linux kernel wrapper | Simple API; supports older Go versions | Older low-level implementation; RawConn Read and Write callbacks still panic |
| `free5gc/sctp` | Related Linux kernel wrapper with telecom configuration | RTO, association parameters, MaxSeg, and some typed notifications | Deadlines remain unsupported; `SCTPRead` has a different signature |
| `georgeyanev/go-sctp` | Linux kernel wrapper closer to Go's standard networking API | Context, deadlines, receive flags, event parsing, dynamic binding, and more socket settings | Still Linux-dependent; migration requires API changes |
| `pion/sctp` | Go userspace stack focused on WebRTC | No kernel SCTP dependency; cross-platform; independent Stream objects and partial reliability APIs | Requires a transport; its model differs from kernel socket APIs |

RawConn behavior is documented in the [upstream implementation](https://github.com/ishidawataru/sctp/blob/19ddcbc6aae2b602d6384cc4048e5f91f0e84e5d/sctp_linux.go#L64-L87) and [free5GC implementation](https://github.com/free5gc/sctp/blob/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd/sctp_linux.go#L67-L82). See free5GC's [SocketConfig](https://github.com/free5gc/sctp/blob/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd/sctp.go#L952-L986) and [Notification](https://github.com/free5gc/sctp/blob/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd/sctp_notification.go), and go-sctp's [connection API](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/sctp_conn.go) and [Dialer](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/dial.go).

## Strengths of this library

### Retaining the upstream API

The library retains key interfaces such as `DialSCTP`, `ListenSCTP`, `SCTPRead`, `SCTPWrite`, `SndRcvInfo`, and wrapped connections. Existing ishidawataru users can keep their call structure while checking changes in Go requirements, fd ownership, timeouts, and PPID handling. [Library API](https://github.com/skill215/sctp/blob/bd2fa276b5be3bdebef3a4a9756cae7567505359/sctp.go)

This differs from switching libraries outright. For example, free5GC adds a notification return value to `SCTPRead`; go-sctp uses `ReadMsg`, `SndInfo`, and `RcvInfo`; Pion uses Association and Stream objects. Each requires adapting calls and semantics.

### Regression tests for low-level fixes

Tests have been added for:

- fd ownership, descriptor reuse, concurrent closure, and waking blocked or queued I/O after closure.
- Keeping fds valid during RawConn callbacks and waiting for readability/writability through the runtime poller.
- Read, Write, Accept, and connection timeouts, including a shared timeout budget while consuming notifications.
- Big-endian `socklen_t`, unaligned metadata decoding, PPID byte order, mixed address lists, and numeric interface-name/index conflicts.

See [fd lifecycle tests](../sctp_lifecycle_linux_test.go), [timeout tests](../sctp_timeout_linux_test.go), [ABI/encoding regressions](../sctp_regression_linux_test.go), and [interface-name conflict tests](../sctp_zone_linux_test.go). These support conclusions about specific fixes, not a reliability ranking across all production workloads.

### Small dependency and implementation scope

The library's [go.mod](../go.mod) has no external module dependencies. The kernel processes the protocol; Go mainly integrates socket APIs, buffers, descriptors, and the runtime poller.

This reduces the burden of maintaining a userspace protocol state machine, but kernel versions, settings, and deployment networks still affect behavior. Delegating protocol processing to the kernel is shared by other kernel wrappers and is not unique to this library.

## Main gaps

### Standard deadlines and Context

Although the library implements `net.Conn`, `SetDeadline`, `SetReadDeadline`, and `SetWriteDeadline` still return `syscall.EOPNOTSUPP`, and there is no `DialContext`. Existing `SO_RCVTIMEO` and `SO_SNDTIMEO` provide relative per-operation timeouts sampled at operation start; they do not provide the full behavior of updating a standard deadline to affect an already blocked operation. [Deadline API](https://github.com/skill215/sctp/blob/bd2fa276b5be3bdebef3a4a9756cae7567505359/sctp.go#L829-L839), [timeout implementation](../socket_io_linux.go)

go-sctp implements connection deadlines through `os.File`, provides a Context-aware Dialer, and supplies more consistent `net.OpError` context. These can reduce adaptation in Go services that need timeouts, cancellation, and consistent error handling. [Connection implementation](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/conn.go), [dialing API](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/dial.go)

### Message boundaries and receive flags

This library's `SCTPRead` returns only a length, `SndRcvInfo`, and an error. It does not expose recvmsg flags such as `MSG_EOR`, `MSG_NOTIFICATION`, or `MSG_CTRUNC`. When the receive buffer cannot hold an entire message, callers have limited ability to determine message boundaries, distinguish notifications, or handle ancillary truncation. The wrapped connection adds a local metadata header, which cannot supply those receive flags. [Read implementation](https://github.com/skill215/sctp/blob/bd2fa276b5be3bdebef3a4a9756cae7567505359/sctp_linux.go#L181-L231)

go-sctp's `ReadMsg/ReadMsgExt` return receive flags and document EOR-based completeness checks; `ReadMsgExt` also permits oob-buffer reuse. This is a useful design reference for adding functionality without breaking the existing `SCTPRead` signature. [ReadMsg API](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/sctp_conn.go#L91-L138)

### Notification parsing and common configuration

This library primarily handles notifications through raw-byte callbacks, and less common settings often require direct socket-option handling. free5GC supplies some typed notifications plus RTO, association parameters, and MaxSeg settings; go-sctp provides more event parsing, heartbeat settings, dynamic address binding, and half-close APIs. [free5GC configuration](https://github.com/free5gc/sctp/blob/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd/sctp.go), [go-sctp event parsing](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/event.go)

Low-level socket options enable extensions but require callers to understand structure layout, size, byte order, and timing. Typed interfaces can centralize this work and reduce duplicate implementations.

### Go version and platform constraints

The support scope was clarified on 2026-10-31: **only Linux with kernel SCTP is supported, excluding Linux/386.** Windows, macOS, BSD, Solaris, and illumos are outside scope; compilation, execution, and passing tests on them are not guaranteed.

Existing unsupported stubs, `ErrUnsupported`, cross-platform API declarations, and non-Linux compatibility checks do not constitute a support promise. Non-Linux test-compilation failures are recorded as out-of-scope compatibility issues, not evidence of Linux SCTP regressions. Linux/386 descriptor-cleanup tests likewise do not establish SCTP I/O support on that architecture.

Known out-of-scope issue: v0.7.2 Solaris/illumos test compilation fails because fd regressions reference `syscall.Dup2`, although the library itself builds. On 2026-10-31, the current application was confirmed to use only Linux, so this remains documented, deferred, and non-blocking for release. Reproduction and controls are in the [review results](repro/v0.7.2-review-results.json).

Native SCTP integration checks currently run on Linux/amd64. QEMU checks for other Linux architectures cover ABI, byte order, and encoding rather than complete protocol validation. See [README testing](../README.md#testing) for the exact scope.

The minimum Go version is 1.26. Compared with libraries requiring older versions, this increases upgrade costs for legacy projects. A newer Go baseline helps implementation and maintenance but does not itself add SCTP protocol capabilities.

| Library | go.mod declaration in the comparison snapshot |
| --- | --- |
| [skill215/sctp](https://github.com/skill215/sctp/blob/bd2fa276b5be3bdebef3a4a9756cae7567505359/go.mod) | Go 1.26.0 |
| [ishidawataru/sctp](https://github.com/ishidawataru/sctp/blob/19ddcbc6aae2b602d6384cc4048e5f91f0e84e5d/go.mod) | Go 1.12 |
| [free5gc/sctp](https://github.com/free5gc/sctp/blob/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd/go.mod) | Go 1.26.2 |
| [georgeyanev/go-sctp](https://github.com/georgeyanev/go-sctp/blob/a06447f4a276a8d3c79b807215728c74324dee1d/go.mod) | Go 1.20 |
| [pion/sctp](https://github.com/pion/sctp/blob/6ec762ebc1106ae8c5ce1a69208a98dfcfdddaaf/go.mod) | Go 1.24.0 |

## Test coverage and performance assessment

CI runs Linux/amd64 integration tests with Go 1.26 and 1.27. Release checks require ten race rounds per Go version and actual interface-name conflict tests in isolated network namespaces. QEMU checks on ARM, ARM64, MIPS, MIPS64, PPC64, and s390x cover socket-option ABI, alignment, and encoding, not complete SCTP associations. [CI configuration](../.github/workflows/ci.yaml), [architecture checks](../.github/workflows/arch.yaml)

Pion CI includes Windows, macOS, wasm, and periodic fuzzing. It validates a userspace stack, so its scope cannot be equated directly with kernel-wrapper tests. [Pion platform checks](https://github.com/pion/sctp/blob/6ec762ebc1106ae8c5ce1a69208a98dfcfdddaaf/.github/workflows/test.yaml), [Pion fuzzing](https://github.com/pion/sctp/blob/6ec762ebc1106ae8c5ce1a69208a98dfcfdddaaf/.github/workflows/fuzz.yaml)

As of 2026-10-31, v0.7.2 has four fuzz targets: sockaddr, ancillary, codec, and address round trips. Benchmarks and application-integration stress tests remain to be added. This comparison has no performance data from identical environments and does not rank throughput, latency, memory, or CPU. Performance testing should fix hardware, kernel, socket settings, and workload, then compare message sizes, concurrent associations, stream counts, packet loss, allocations, and tail latency. Userspace/kernel comparisons must also state the underlying transport and the scope of process-memory and kernel-resource accounting.

## Release version conventions

Update, 2026-10-31: canonical annotated tag `v0.7.2` is published. An independent module verified that both `go get @v0.7.2` and `@latest` resolve to `v0.7.2`. The historical comparison-date observations below are retained; tag naming is resolved and is no longer a current gap.

At the comparison date, tags such as `0.7.1` lacked the `v` prefix, and the release workflow matched only `0.*`. GitHub Releases existed, but these tags were not canonical Go semantic versions. Go could resolve a tag query to a pseudo-version; formal version discovery and upgrades needed improvement. [Release workflow at that snapshot](https://github.com/skill215/sctp/blob/bd2fa276b5be3bdebef3a4a9756cae7567505359/.github/workflows/release.yaml#L3-L10), [Go module version rules](https://go.dev/ref/mod#versions)

At comparison time, `go list -m -json github.com/skill215/sctp@0.7.1` returned `v0.0.0-20261029085721-e864ee06b6c5`; `go list -m -versions github.com/skill215/sctp` listed no canonical semantic versions. Subsequent releases should use tags in the form `v0.7.1` and update workflow triggers accordingly.

## Improvement priorities

| Priority | Recommendation | Purpose |
| --- | --- | --- |
| 1 | Canonical `v`-prefixed tags and release workflow | Improve version discovery, pinned dependencies, and upgrades |
| 2 | Add `ReadMsg/ReadMsgExt` while retaining existing I/O APIs | Expose message boundaries, notification and ancillary-truncation flags, and buffer reuse |
| 3 | Implement standard deadlines and `DialContext` | Improve cancellation, timeouts, and Go networking integration |
| 4 | Add typed notification parsing and common socket settings | Reduce application-level raw-byte and ABI handling |
| 5 | Establish benchmarks, fuzzing, and cross-kernel interoperability tests | Quantify performance and expand input/kernel/protocol coverage |

These are future recommendations, not a claim that the compared version already has the capabilities.

## Source comparison baselines

These pinned commits define the comparison scope. Recheck target-version APIs, dependencies, and test results before later library selection.

| Library | Source snapshot |
| --- | --- |
| skill215/sctp | [bd2fa27](https://github.com/skill215/sctp/commit/bd2fa276b5be3bdebef3a4a9756cae7567505359) |
| ishidawataru/sctp | [19ddcbc](https://github.com/ishidawataru/sctp/commit/19ddcbc6aae2b602d6384cc4048e5f91f0e84e5d) |
| free5gc/sctp | [e86160f](https://github.com/free5gc/sctp/commit/e86160f55c756c02d7bdd1ab7c852fc3676c6dbd) |
| georgeyanev/go-sctp | [a06447f](https://github.com/georgeyanev/go-sctp/commit/a06447f4a276a8d3c79b807215728c74324dee1d) |
| pion/sctp | [6ec762e](https://github.com/pion/sctp/commit/6ec762ebc1106ae8c5ce1a69208a98dfcfdddaaf) |
