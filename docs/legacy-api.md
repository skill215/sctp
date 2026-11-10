# Legacy SCTP APIs

These APIs remain available in v0.8.0 when `SocketConfig.Messages` is nil.
For new message-oriented applications, start with the [README](../README.md)
and [message example](../example/message/main.go). Legacy `SCTPRead` does not
expose EOR, so it cannot by itself establish complete application messages.

API availability does not imply identical behavior to 0.4. Since 0.5, owned
descriptors are nonblocking while public Accept waits through the Go poller.
Standard deadlines become active in v0.8.0 even in legacy mode. Review the
[version-by-version compatibility guide](compatibility-v0.4-v0.8.0.md) before
upgrading code that uses raw fds, duplicates, old constants, or timeout workarounds.

The original [echo example](../example/sctp.go) uses the 32-byte local wrapper
format on the server and `SCTPRead`/`SCTPWrite` on the client. It is retained for
compatibility; run it with `-ip`, `-port`, and optionally `-lport`, `-bufsize`,
`-sndbuf`, or `-rcvbuf`. It sends random payloads until interrupted.

For connection SO timeouts, v0.8.0 also offers `SetSocketTimeouts` without raw
syscalls. The `Control` example below is a legacy configuration mechanism and
cannot be combined with message mode or the bounded setup API.

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

Ports must be in 0..65535; local port 0 selects an ephemeral port. Dial requires
a non-nil remote address. Multihomed strings reject empty host segments;
`:9000` remains a valid standalone wildcard. Listen/Dial copy their address
inputs, so concurrent calls can share a read-only address.

Local addresses must be assigned to the host. To select a local address or port
when dialing, pass a non-nil `laddr` to `DialSCTP`.

IPv6 zones first match an existing interface name, including a purely numeric
name, then fall back to a numeric interface index. `SCTPLocalAddr` and
`SCTPRemoteAddr` return address lists, including mixed IPv4/IPv6 lists;
`SCTPGetPrimaryPeerAddr` returns the primary peer address. Complete address
queries grow from 4 KiB to a **1 MiB address-area budget** on `ENOMEM` (at most
nine attempts). Exceeding that budget returns an error wrapping `syscall.ENOMEM`,
not a partial list. `Addr`/`LocalAddr`/`RemoteAddr`, whose signatures cannot
return errors, return nil if the query fails.

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
whole frame to be retried. Construction adds DATA_IO while retaining existing
event subscriptions. Setup failures surface on subsequent Read/Write; Close
still releases the underlying connection. A returned frame always initializes
the complete header; absent metadata produces a zero header. That header alone
cannot distinguish notifications from stream=0/PPID=0 payloads.

On a live connection, empty Read/SCTPRead returns zero without consuming queued
data or notifications. Wrapped reads require **more than 32 bytes** of capacity;
shorter or header-only buffers return `0, syscall.EINVAL` without receiving data.
All empty writes make a genuine zero-payload kernel call. Ordinary empty SCTP
messages are rejected with the kernel error, and no byte is injected. Explicit
EOF/ABORT controls retain kernel applicability rules; `SCTP_EOF` is `0x200`.
Default send parameters and invalid control combinations are interpreted by the
kernel. Header-only wrapped controls count the 32-byte header only on success.

## Socket configuration and timeouts

On supported Linux targets, `SetDeadline`, `SetReadDeadline`, and
`SetWriteDeadline` affect current and future I/O, including wrapped connections.
Zero clears the public deadline. Expiry matches `os.ErrDeadlineExceeded`;
unsupported targets retain `syscall.EOPNOTSUPP`. See the
[waiting-layer scope and validation](diameterkit-u3-foundation.md).

`SO_RCVTIMEO` and `SO_SNDTIMEO` remain optional fixed per-operation budgets,
independent of the public deadlines. Set them through `SocketConfig.Control`.
This hook runs after socket creation and before binding or connecting:

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

A zero socket timeout adds no relative limit; a public deadline may still apply.
Each operation snapshots its timeout;
time queued behind another operation counts toward that budget. A read keeps
the same budget while processing notifications, but cannot interrupt a running
notification handler. A socket timeout does not leak its temporary deadline into subsequent
operations; an explicit public deadline remains until changed. Closing a connection or listener wakes blocked and queued I/O.

## File descriptors and raw I/O

`NewSCTPConn(fd, handler)` **takes ownership** of `fd`. Release it with the
connection's `Close`; do not close the descriptor independently. On unsupported
targets, construction cannot initialize I/O and immediately releases the supplied
socket with the platform's socket-close operation. Subsequent methods return
`ErrUnsupported`, including any saved native cleanup failure. Existing stub
builds expose `ErrUnsupported` and both `SyscallConn` methods for compatibility;
they do not provide supported SCTP operation on those targets.
`FileListener(file)` duplicates the supplied descriptor, so the caller remains
responsible for closing the original file. `PeelOff` returns a separately owned
connection with close-on-exec set.

On supported Linux targets, `PeelOff`, `SCTPLocalAddr` and `SCTPRemoteAddr`
reject association IDs outside the signed 32-bit kernel range with `EINVAL`.
`SetInitMsg` accepts values from 0 through 65535; zero preserves the kernel's
existing/default value. Out-of-range values return `EINVAL` without changing
socket options. Unsupported targets continue returning `ErrUnsupported`.

The package uses non-blocking descriptors with Go's runtime poller. Raw callbacks
follow the `syscall.RawConn` contract: keep syscalls non-blocking and return
`false` from `Read` or `Write` callbacks on `EAGAIN` so the poller can wait for
readiness. A callback holds its descriptor alive; do not close the owner from
inside that callback.

For low-level SCTP `Getsockopt` calls, `optlen` points to a **uint32** Linux
`socklen_t`, including on 64-bit systems. Status constants follow the Linux
SCTP UAPI: `SCTP_ESTABLISHED` is `4` and `SCTP_ACTIVE` is `2`. The deprecated
`SCTP_BOUND` and `SCTP_LISTEN` constants are not returned by Linux `SCTP_STATUS`.
