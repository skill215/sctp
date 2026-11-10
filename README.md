# SCTP for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/skill215/sctp.svg)](https://pkg.go.dev/github.com/skill215/sctp)
[![Release](https://img.shields.io/github/v/release/skill215/sctp)](https://github.com/skill215/sctp/releases)

Pure-Go Linux bindings for Stream Control Transmission Protocol (SCTP), with
multiple streams, multihoming, per-message metadata, and kernel socket options.

**[v0.8.0](https://github.com/skill215/sctp/releases/tag/v0.8.0)** adds explicit
message I/O, typed notifications, context-aware setup and accept, standard
deadlines, and queryable resource requirements. These APIs are released and
available now. Existing payload-only, metadata, and wrapper APIs remain available
in legacy mode.

This is a fork of [ishidawataru/sctp](https://github.com/ishidawataru/sctp).
Use `github.com/skill215/sctp` as both module and import path.

## Install

- **Go 1.26 or later.**
- **Linux with kernel SCTP enabled; Linux/386 is unsupported.**
- Both peers and the network path must support SCTP.
- No C library or `lksctp-tools` dependency is needed; production builds support
  `CGO_ENABLED=0`. Containers use the host kernel. If SCTP is a loadable module,
  enable it on the host with `sudo modprobe sctp`.

```sh
go get github.com/skill215/sctp@v0.8.0
```

Windows, macOS, BSD, Solaris, and illumos are outside the supported runtime scope.
Stub builds and cross-platform checks do not expand that commitment.
See the [platform policy](docs/comparison.md#go-version-and-platform-constraints).

## Choose an API

| Task | v0.8.0 API |
| --- | --- |
| Bound address input and share one setup budget | `SetupConfig`, `AddressLimits`, `NewDialer`, `Dialer.DialContext`, `ListenContext` |
| Cancel an idle accept or set its deadline | `AcceptSCTPContext`, `SCTPListener.SetDeadline` |
| Read data fragments and notifications explicitly | `SocketConfig.Messages`, `ReadEvent`, `ReadResult.EndOfRecord` |
| Send one message with stream and PPID | `WriteMsgContext` |
| Decode five notification families | `ParseNotification` |
| Query buffer sizes and logical resource charges | `MessageIORequirements`, `SetupRequirements` |
| Inspect effective options and negotiated streams | `GetMessageOptions`, `GetStatus` |
| Inspect the explicit bind or clear SO timeouts | `BoundAddr`, `SetSocketTimeouts` |
| Integrate existing payload/wrapper code | Leave `SocketConfig.Messages` nil; use the [legacy APIs](docs/legacy-api.md) |

Use **keyed struct literals** when upgrading from v0.7.4: `SocketConfig` now has
a `Messages` field. Non-nil `Messages` selects immutable message mode. Do not mix
it with `Read`/`Write`, `SCTPRead`/`SCTPWrite`, wrappers, or notification
callbacks. The bounded setup API rejects `Control` and `NotificationHandler`;
use its public configuration methods instead.

Upgrading from an older release? Read the
[0.4 to v0.8.0 compatibility and migration guide](docs/compatibility-v0.4-v0.8.0.md).
Nonblocking descriptors started in 0.5; public Accept still waits. The guide
covers raw fd integration, deadlines, changed constants, PPID, and wrapper behavior.

The [I/O interface design](docs/io-interface-design.md) retains synchronous APIs
over nonblocking descriptors and defines upper-layer channel boundaries,
ownership, resource accounting, and acceptance requirements.
The [acceptance coverage audit](docs/io-acceptance-audit.md) maps these requirements
to existing tests and records verification results and remaining limits.

## Run the message example

From a checkout of this repository:

```sh
# Terminal 1: bounded setup, typed notifications, serial echo service
go run ./example/message -server -addr 127.0.0.1:9000

# Terminal 2: verify payload, stream and PPID across fragmented receives
go run ./example/message -addr 127.0.0.1:9000 -stream 1 -ppid 46 -chunk 4 -count 3

# PPID zero is also valid
go run ./example/message -addr 127.0.0.1:9000 -ppid 0
```

The client prints `payload="hello SCTP" stream=1 ppid=46 eor=true` for each
successful round trip. Ctrl+C cancels an idle accept or active I/O. The server
handles one association at a time, keeping example concurrency bounded.

The [complete example](example/message/main.go) and its
[receive loop](example/message/receive.go) demonstrate:

- setup cancellation detached from successfully handed-off connections;
- actual negotiated stream checks, host-order PPID, and one-send writes;
- bounded reassembly through EOR, with one deadline across fragments/notifications;
- parsing notifications on the same reader, including terminal facts before closure;
- fixed notification buffers and application size/count limits.

Use `-network sctp6 -addr '[::1]:9000'` for IPv6, `-local 127.0.0.1:0` for
an explicit client bind, and `-timeout`, `-max-message`, or `-chunk` to change
the example's limits. Run `go run ./example/message -h` for all flags.

## Minimal message client

Run this client against the example server above. In a separate directory,
initialize a module and pin the release:

```sh
go mod init example.com/sctp-client
go get github.com/skill215/sctp@v0.8.0
```

Save the following as `main.go`, then run `go run .`. This minimal client
subscribes to no notifications; the complete example shows notification handling.

```go
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/skill215/sctp"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg := sctp.SetupConfig{
		Socket: sctp.SocketConfig{
			InitMsg: sctp.InitMsg{NumOstreams: 2, MaxInstreams: 2},
			Messages: &sctp.MessageOptions{
				ReceiveInfo: sctp.ReceiveInfoSNDRCV,
				NoDelay:     true, AllowFragmentation: true,
			},
		},
		Limits: sctp.AddressLimits{
			MaxAddresses: 16, MaxZoneBytes: 256, MaxAddressBytes: 4096,
		},
	}
	req, err := sctp.SetupRequirements(cfg) // no DNS or socket I/O
	if err != nil {
		return err
	}
	dialer, err := sctp.NewDialer("sctp4", "", "127.0.0.1:9000", cfg)
	if err != nil {
		return err
	}
	setup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, err := dialer.DialContext(setup)
	cancel() // successful handoff leaves conn open
	if err != nil {
		return err
	}
	defer conn.Close()

	// Standard deadlines affect current/future I/O; zero clears them.
	// This single budget covers the send and every receive fragment.
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	payload := []byte("hello SCTP")
	n, err := conn.WriteMsgContext(context.Background(), payload,
		sctp.SndInfo{SID: 0, PPID: 42})
	if err != nil {
		return fmt.Errorf("send committed %d bytes (no retry): %w", n, err)
	}
	if n != len(payload) {
		return io.ErrShortWrite // never resend the tail as another message
	}

	data := make([]byte, 4) // deliberately receive in fragments
	notification := make([]byte, req.IORequirements.NotificationMin)
	oob := make([]byte, req.IORequirements.OOBMin)
	reply := make([]byte, 0, 64*1024)
	for {
		r, err := conn.ReadEvent(context.Background(), data, notification, oob)
		if err != nil {
			return err // defer closes on timeout, EOF, or boundary failure
		}
		if r.CloseRequired || r.Kind != sctp.ReadData || !r.InfoPresent {
			return fmt.Errorf("unexpected receive result: %+v", r)
		}
		if r.Info.Stream != 0 || r.Info.PPID != 42 {
			return fmt.Errorf("unexpected stream/PPID: %+v", r.Info)
		}
		if r.N > cap(reply)-len(reply) {
			return fmt.Errorf("reply exceeds application limit")
		}
		reply = append(reply, data[:r.N]...)
		if r.EndOfRecord() {
			break
		}
	}
	if string(reply) != string(payload) {
		return fmt.Errorf("echo payload mismatch")
	}
	fmt.Printf("payload=%q stream=0 ppid=42 eor=true\n", reply)
	return nil
}
```

## Message boundaries, streams, and notifications

`ReadEvent` returns one data fragment, one raw notification, or EOF in separate
caller-owned data/notification/OOB buffers. Allocate at least the queried
`NotificationMin` and `OOBMin`; the three buffers must not overlap. A data
buffer may be smaller than a message: accumulate within your own limit until
`EndOfRecord()`. EOR marks an SCTP message boundary, not application-protocol
validity. The library does not buffer a complete application message for you.

Message mode checks association, stream, PPID, ordering, and ordered SSN across
fragments. Notifications do not reset that cursor. Stream IDs are zero-based:
outgoing SID must be below actual `GetStatus().Ostreams`, incoming stream below
`Instreams`. Requested `InitMsg` counts are not negotiated counts.
PPID uses host byte order, including zero.

`WriteMsgContext` sends one nonempty message. It preserves actual byte counts
with errors and returns `io.ErrShortWrite` for short success. Do not automatically
resend a tail or infer business delivery from a successful kernel send.

Subscribe through `MessageOptions.Events` before setup:

| Subscription | Parsed kind |
| --- | --- |
| `SCTP_EVENT_ASSOCIATION` | `NotificationAssociationChange` |
| `SCTP_EVENT_ADDRESS` | `NotificationPeerAddressChange` |
| `SCTP_EVENT_SEND_FAILURE` | `NotificationSendFailure` |
| `SCTP_EVENT_SHUTDOWN` | `NotificationShutdown` |
| `SCTP_EVENT_PARTIAL_DELIVERY` | `NotificationPartialDelivery` |

Pass consumed, complete notification bytes to `ParseNotification`. Fixed fields
are owned values; `Tail` borrows the input buffer and must be copied within an
application budget if retained. Unknown types remain raw. Old SEND_FAILED and
new SEND_FAILED_EVENT are not interchangeable.

A valid RESTART may arrive with both `ErrAssociationChanged` and
`CloseRequired`; an active-message partial-abort may carry `CloseRequired`
without an error. Preserve those valid notification facts **before** closing.
Malformed/unconsumed records must not be treated as successful notifications.
After `CloseRequired`, close the connection; do not continue reading a generation
whose boundaries or negotiated streams are no longer valid.

`ErrNotificationTooLarge` reports `RequiredNotificationBytes` without consuming
the notification. Either retry with a larger buffer within an explicit cap or
close. Never allocate blindly from the supplied length. The example chooses a
fixed 4096-byte notification buffer and closes on overflow.

## Contexts, deadlines, and ownership

- `Dialer.DialContext` shares one context across resolution and connection.
  `ListenContext` similarly covers resolution, bind/listen, and publication.
- `AcceptSCTPContext` cancels waiting without a polling loop. A successful
  accepted connection is independent of later listener closure or setup-context
  cancellation. The caller owns it and must close it.
- Connection `SetDeadline`/`SetReadDeadline`/`SetWriteDeadline` and listener
  `SetDeadline` affect current/future operations; zero clears them. Listener
  deadlines do not transfer to accepted connections.
- Cancellation matches `context.Canceled`/`context.DeadlineExceeded`;
  standard deadline expiry matches `os.ErrDeadlineExceeded`; closure matches
  `net.ErrClosed`. Inspect errors with `errors.Is`.
- `SetSocketTimeouts(read, write)` controls independent SO timeouts.
  `SetSocketTimeouts(0, 0)` clears them during exclusive takeover; use
  `SetDeadline(time.Time{})` separately to clear standard deadlines.
  A `*SocketTimeoutUpdateError` can represent a partial update: close after a
  failed takeover. Fresh bounded-setup sockets already start with zero SO timeouts.
- Keep one budget across an entire application frame, including notifications;
  creating a new timeout for every fragment would let partial traffic extend it.

For already-resolved addresses, `SocketConfig.DialContext` remains available.
Pair it with `ResolveSCTPAddrContext` using the same context. Low-level descriptor
ownership and legacy SO-timeout behavior are documented in [legacy APIs](docs/legacy-api.md).

## Addresses and multihoming

Bounded setup accepts `sctp`, `sctp4`, and `sctp6`, numeric ports, and explicit
slash-separated multihoming:

| Endpoint | Text |
| --- | --- |
| IPv4 | `127.0.0.1:9000` |
| IPv6 | `[::1]:9000` |
| Multiple IPv4 addresses | `192.0.2.10/192.0.2.11:9000` |
| Multiple IPv6 addresses | `[2001:db8::10]/[2001:db8::11]:9000` |
| Scoped IPv6 | `[fe80::1%eth0]:9000` |
| Wildcard listener | `:9000` |

Local addresses must belong to the host. Local port zero requests an ephemeral
port; a remote endpoint must have a host and nonzero port. An empty local dial
string means no explicit local bind. DNS selects one suitable IP per explicit
host, without automatically turning DNS answers into multihoming.

Set all `AddressLimits` explicitly. N counts addresses across local+remote,
Z counts zone bytes, and D counts combined endpoint-text bytes. Supported ranges
are N=1..64, Z=0..4096, D=1..16384; 16/256/4096 is a useful starting point.
Zero is not an unlimited setting. Inputs are copied, but callers must not mutate
them concurrently during construction.

`BoundAddr()` returns an owned explicit-bind snapshot with the actual port.
Wildcards remain wildcards, not advertised routable addresses. Imported
`FileListener` instances without a snapshot return `ErrBoundAddrUnavailable`.
Dynamic kernel address queries and their 1 MiB budget remain available through
the [legacy address APIs](docs/legacy-api.md#addresses-and-multihoming).

## Resource requirements

Query `SetupRequirements(cfg)` before constructing objects and use its
`LedgerVersion` and actual platform values. The query performs no DNS/socket I/O
and does not probe whether the host kernel supports SCTP.

| Reservation | Composition |
| --- | --- |
| Per listener | `ListenerBytes`; accepted connections are separate |
| Per dial candidate | `DialerBytes + max(EstablishBytes, ConnectionBytes)` |
| Existing connection | `ConnectionBytes` |
| Application buffers/state | Add data, OOB, notification, reassembly, retained tails, and business allowances separately |

`EstablishBytes` already includes the establishing connection.
`ConnectionBytes` already includes I/O scratch and one fixed parsed notification.
Do not add the historical 8192-byte reserve or copy architecture-specific formulas.
Retiring old connections, new candidates, and listeners can coexist; cancellation
requests do not release their reservations until operations/hooks actually return.

The ledger assumes one reader, one writer, one Accept caller, no queued upstream
I/O, controlled contexts, and bounded serial deadline updates. It excludes Go
runtime/GC/allocator/stacks, kernel queues, and resolver/interface internals.
It is a logical integration budget, **not an RSS cap**.
See the [complete contracts and ledger](docs/bounded-setup.md).

## Legacy compatibility and scope

The original `go run ./example -server -ip 127.0.0.1 -port 9000` and
`go run ./example -ip 127.0.0.1 -port 9000` demonstrate legacy wrapper/metadata
I/O. See [legacy APIs and timeout configuration](docs/legacy-api.md) for their
semantics, including empty I/O, wrapper headers, raw descriptors, and address
queries. New message-oriented applications should start with `example/message`.

v0.8.0 message mode uses one-to-one associations and interleave level 0. It does
not implement dynamic stream reset, automatic reconnect/replay, application
protocols, or the separate v1 redesign proposal.
The [library scope](docs/library-scope.md) defines the project boundary.

## Validation and further reading

On a Linux host with SCTP support:

```sh
CGO_ENABLED=0 go test ./...
go test -race ./...
go vet ./...
python3 .github/scripts/readme.py
python3 .github/scripts/readme.py --race
```

The README check builds this page's Go client, runs the message server and clients,
and executes the standalone bounded consumer against this checkout.
It uses a temporary development replace, not a published-module verification.

Release CI covers Go 1.26/1.27 on two native Linux/amd64 kernel environments,
ten independent race/shuffle rounds, vet, strict checkptr, low-GC tests, five fuzz
targets, consumers/examples, and mandatory namespace tests. Six QEMU architectures
cover ABI/encoding rather than native SCTP networking. Windows/Linux386 resource
checks do not establish supported SCTP operation on those platforms.

- [v0.8.0 Release Notes](https://github.com/skill215/sctp/releases/tag/v0.8.0)
- [Package reference](https://pkg.go.dev/github.com/skill215/sctp@v0.8.0)
- [Standalone bounded consumer](docs/examples/bounded-consumer/main.go.txt)
- [Publication evidence](docs/evidence/v0.8.0/publication-result.md)
- [diameterkit integration handoff](docs/diameterkit-v0.8.0-handoff.md)
- [Go SCTP library comparison](docs/comparison.md)
- [Historical v0.7.2 validation](docs/release-plan-v0.7.2.md)

## License

[Apache License 2.0](LICENSE).
