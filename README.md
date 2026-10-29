# SCTP for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/skill215/sctp.svg)](https://pkg.go.dev/github.com/skill215/sctp)
[![Release](https://img.shields.io/github/v/release/skill215/sctp)](https://github.com/skill215/sctp/releases)

Go bindings for Linux's Stream Control Transmission Protocol (SCTP) sockets.
The package provides listeners and connections, multiple streams per association,
multihoming, per-message metadata, and access to SCTP socket options without cgo.

This is a fork of [ishidawataru/sctp](https://github.com/ishidawataru/sctp).
Use `github.com/skill215/sctp` as the module and import path.

## Requirements and installation

- **Go 1.26 or later.**
- **Linux with kernel SCTP support.** Linux/386 and non-Linux targets use
  unsupported socket stubs that return `ErrUnsupported`.
- SCTP traffic must be allowed between peers. Both peers need SCTP support.

No C library or `lksctp-tools` dependency is required. If SCTP is available as a
kernel module, load it on the host with `sudo modprobe sctp`. Containers use the
host's kernel and need SCTP support there as well.

Add the package to an existing Go module:

```sh
go get github.com/skill215/sctp
```

See the [releases](https://github.com/skill215/sctp/releases) for changes and
[package reference](https://pkg.go.dev/github.com/skill215/sctp) for the full API.

## Quick start

The following server echoes the payload and its stream/PPID metadata. Create a
module for the example, then save the two programs in separate directories:

```sh
mkdir sctp-demo
cd sctp-demo
go mod init example.com/sctp-demo
go get github.com/skill215/sctp
mkdir server client
```

Save as `server/main.go`:

```go
package main

import (
	"errors"
	"io"
	"log"

	"github.com/skill215/sctp"
)

func main() {
	addr, err := sctp.ResolveSCTPAddr("sctp4", "127.0.0.1:9000")
	if err != nil {
		log.Fatal(err)
	}
	ln, err := sctp.ListenSCTP("sctp4", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	log.Printf("listening on %s", ln.Addr())
	for {
		conn, err := ln.AcceptSCTP()
		if err != nil {
			log.Print(err)
			return
		}
		go echo(conn)
	}
}

func echo(conn *sctp.SCTPConn) {
	defer conn.Close()
	if err := conn.SubscribeEvents(sctp.SCTP_EVENT_DATA_IO); err != nil {
		log.Print(err)
		return
	}
	buf := make([]byte, 64*1024)
	for {
		n, info, err := conn.SCTPRead(buf)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Print(err)
			}
			return
		}
		if _, err := conn.SCTPWrite(buf[:n], info); err != nil {
			log.Print(err)
			return
		}
	}
}
```

Save as `client/main.go`:

```go
package main

import (
	"fmt"
	"log"

	"github.com/skill215/sctp"
)

func main() {
	addr, err := sctp.ResolveSCTPAddr("sctp4", "127.0.0.1:9000")
	if err != nil {
		log.Fatal(err)
	}
	conn, err := sctp.DialSCTP("sctp4", nil, addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SubscribeEvents(sctp.SCTP_EVENT_DATA_IO); err != nil {
		log.Fatal(err)
	}
	if _, err := conn.SCTPWrite([]byte("hello SCTP"), &sctp.SndRcvInfo{Stream: 0, PPID: 42}); err != nil {
		log.Fatal(err)
	}
	buf := make([]byte, 1024)
	n, info, err := conn.SCTPRead(buf)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("payload=%q info=%+v\n", buf[:n], info)
}
```

Run `go run ./server` in one terminal and `go run ./client` in another. The
client should receive `hello SCTP` with stream `0` and PPID `42`.

For a configurable echo example, run the programs already in this repository:

```sh
# Terminal 1, from the repository root
go run ./example -server -ip 127.0.0.1 -port 9000

# Terminal 2, from the repository root
go run ./example -ip 127.0.0.1 -port 9000
```

[example/sctp.go](example/sctp.go) also accepts comma-separated IP addresses,
`-lport`, `-bufsize`, `-sndbuf`, and `-rcvbuf`.

## Addresses and multihoming

Use `sctp` for automatic address-family selection, `sctp4` for IPv4, or `sctp6`
for IPv6. An empty network name is equivalent to `sctp`.

`ResolveSCTPAddr` uses `/` to separate addresses in a multihomed endpoint, with
one port at the end:

| Endpoint | Address string |
| --- | --- |
| IPv4 | `127.0.0.1:9000` |
| IPv6 | `[::1]:9000` |
| Multiple IPv4 addresses | `192.0.2.10/192.0.2.11:9000` |
| Multiple IPv6 addresses | `[2001:db8::10]/[2001:db8::11]:9000` |
| Scoped IPv6 | `[fe80::1%eth0]:9000` or `[fe80::1%2]:9000` |
| Wildcard listener | `:9000` |

Local addresses must be assigned to the host. To select a local address or port
when dialing, pass a non-nil `laddr` to `DialSCTP`.

IPv6 zones first match an existing interface name, including a purely numeric
name, then fall back to a numeric interface index. `SCTPLocalAddr` and
`SCTPRemoteAddr` return address lists, including mixed IPv4/IPv6 lists;
`SCTPGetPrimaryPeerAddr` returns the primary peer address.

## Streams, metadata, and notifications

`SCTPConn` implements `net.Conn`, and `SCTPListener` implements `net.Listener`.
Use `Read` and `Write` for payload-only I/O, or `SCTPRead` and `SCTPWrite` for
per-message `SndRcvInfo`, including the stream number, PPID, and flags such as
`SCTP_UNORDERED`.

- Subscribe to `SCTP_EVENT_DATA_IO` to receive metadata; otherwise `SCTPRead`
  may return a nil `*SndRcvInfo`.
- `SndRcvInfo.PPID` uses **host byte order** in reads, writes, and
  `GetDefaultSentParam` / `SetDefaultSentParam`. Pass `42`, not a byte-swapped
  value.
- Stream numbers start at zero and must fit within the negotiated stream count.
  Use `ListenSCTPExt`, `DialSCTPExt`, or `SocketConfig.InitMsg` to set
  `NumOstreams` and `MaxInstreams` before association setup.
- Size receive buffers for your expected messages. `SCTPRead` does not expose
  `recvmsg` flags such as `MSG_EOR` to identify complete messages.

Set `SocketConfig.NotificationHandler` and subscribe to the desired events to
handle association, address, shutdown, or other notifications. The handler runs
synchronously during reads and receives raw notification bytes. Copy the bytes
if you need to retain them after it returns. A handler error ends the read;
without a handler, subscribed notifications are returned as ordinary read data.

`NewSCTPSndRcvInfoWrappedConn` adapts a connection for code that needs metadata
inside its read/write buffers. Each buffer starts with the 32-byte native-endian
encoding of `SndRcvInfo`, followed by the payload. The header is a local adapter
format; it is not added to the SCTP wire payload. Wrapped writes accept unaligned
buffers and return zero when a send fails without writing data, allowing the
whole frame to be retried. The wrapper subscribes to data I/O events when created.

## Socket configuration and timeouts

`SetDeadline`, `SetReadDeadline`, and `SetWriteDeadline` currently return
`syscall.EOPNOTSUPP`, including on wrapped connections. Use `SO_RCVTIMEO` and
`SO_SNDTIMEO` through `SocketConfig.Control` instead. This hook runs after socket
creation and before binding or connecting:

```go
import (
	"syscall"
	"time"

	"github.com/skill215/sctp"
)

func dialWithTimeout(addr *sctp.SCTPAddr) (*sctp.SCTPConn, error) {
	cfg := sctp.SocketConfig{
		InitMsg: sctp.InitMsg{NumOstreams: 8, MaxInstreams: 8},
		Control: func(_, _ string, raw syscall.RawConn) error {
			var optionErr error
			err := raw.Control(func(fd uintptr) {
				tv := syscall.NsecToTimeval((5 * time.Second).Nanoseconds())
				for _, option := range []int{syscall.SO_RCVTIMEO, syscall.SO_SNDTIMEO} {
					if optionErr = syscall.SetsockoptTimeval(int(fd), syscall.SOL_SOCKET, option, &tv); optionErr != nil {
						return
					}
				}
			})
			if err != nil {
				return err
			}
			return optionErr
		},
	}
	return cfg.Dial("sctp4", nil, addr)
}
```

The same control hook can configure a listener via `cfg.Listen`. For an existing
connection or listener, get `SyscallConn()` and set the options inside its
`Control` callback.

| Socket option | Operations | Timeout error |
| --- | --- | --- |
| `SO_RCVTIMEO` | Read and accept | `syscall.EAGAIN` |
| `SO_SNDTIMEO` | Write | `syscall.EAGAIN` |
| `SO_SNDTIMEO` | Connection attempt | `syscall.EINPROGRESS` |

A zero timeout allows unlimited waiting. Each operation snapshots its timeout;
time queued behind another operation counts toward that budget. A read keeps
the same budget while processing notifications, but cannot interrupt a running
notification handler. A timeout does not leave a deadline on subsequent
operations. Closing a connection or listener wakes blocked and queued I/O.

## File descriptors and raw I/O

`NewSCTPConn(fd, handler)` **takes ownership** of `fd`. Release it with the
connection's `Close`; do not close the descriptor independently.
`FileListener(file)` duplicates the supplied descriptor, so the caller remains
responsible for closing the original file. `PeelOff` returns a separately owned
connection with close-on-exec set.

The package uses non-blocking descriptors with Go's runtime poller. Raw callbacks
follow the `syscall.RawConn` contract: keep syscalls non-blocking and return
`false` from `Read` or `Write` callbacks on `EAGAIN` so the poller can wait for
readiness. A callback holds its descriptor alive; do not close the owner from
inside that callback.

For low-level SCTP `Getsockopt` calls, `optlen` points to a **uint32** Linux
`socklen_t`, including on 64-bit systems. Status constants follow the Linux
SCTP UAPI: `SCTP_ESTABLISHED` is `4` and `SCTP_ACTIVE` is `2`. The deprecated
`SCTP_BOUND` and `SCTP_LISTEN` constants are not returned by Linux `SCTP_STATUS`.

## Testing

Run on a Linux host with SCTP support:

```sh
go test -race ./...
go vet ./...
```

CI tests Go **1.26** and **1.27** on Linux/amd64. Release publication requires
ten full race-test runs per Go version and three runs of the numeric-interface
name/index conflict test in an isolated network namespace. That test requires
`unshare`, `iproute2`, and permission to create namespaces; it skips locally when
they are unavailable, while CI runs it with the required privileges and rejects
skips.

Architecture checks use Go 1.26 and QEMU for **ARM, ARM64, MIPS, MIPS64, PPC64,
and s390x**. These cover socket-option ABI, alignment, byte order, and address
encoding using codecs and non-SCTP sockets. They do not exercise full SCTP
associations under emulation; integration tests run natively on Linux/amd64.

## License

[Apache License 2.0](LICENSE). See [release notes](releases/0.7.1.md) for the
Go 1.26 migration and recent reliability fixes.
